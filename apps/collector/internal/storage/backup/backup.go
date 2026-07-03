package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const manifestName = "manifest.json"

type FileRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion int          `json:"schema_version"`
	CreatedAt     time.Time    `json:"created_at"`
	Files         []FileRecord `json:"files"`
}

type RestoreReport struct {
	Files  int
	Bytes  int64
	Target string
}

func Create(dataRoot, outputDirectory string, now time.Time) (string, Manifest, error) {
	dataRoot, _ = filepath.Abs(dataRoot)
	outputDirectory, _ = filepath.Abs(outputDirectory)
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return "", Manifest{}, err
	}
	records, err := collect(dataRoot)
	if err != nil {
		return "", Manifest{}, err
	}
	manifest := Manifest{SchemaVersion: 1, CreatedAt: now.UTC(), Files: records}
	finalPath := filepath.Join(outputDirectory, "albion-market-data-"+now.UTC().Format("20060102-150405")+".zip")
	temporary, err := os.CreateTemp(outputDirectory, ".backup-*.zip")
	if err != nil {
		return "", Manifest{}, err
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	archive := zip.NewWriter(temporary)
	for _, record := range records {
		source := filepath.Join(dataRoot, filepath.FromSlash(record.Path))
		info, err := os.Stat(source)
		if err != nil {
			archive.Close()
			temporary.Close()
			return "", Manifest{}, err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			archive.Close()
			temporary.Close()
			return "", Manifest{}, err
		}
		header.Name = record.Path
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			archive.Close()
			temporary.Close()
			return "", Manifest{}, err
		}
		file, err := os.Open(source)
		if err != nil {
			archive.Close()
			temporary.Close()
			return "", Manifest{}, err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			archive.Close()
			temporary.Close()
			return "", Manifest{}, copyErr
		}
		if closeErr != nil {
			archive.Close()
			temporary.Close()
			return "", Manifest{}, closeErr
		}
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		archive.Close()
		temporary.Close()
		return "", Manifest{}, err
	}
	writer, err := archive.Create(manifestName)
	if err != nil {
		archive.Close()
		temporary.Close()
		return "", Manifest{}, err
	}
	if _, err := writer.Write(payload); err != nil {
		archive.Close()
		temporary.Close()
		return "", Manifest{}, err
	}
	if err := archive.Close(); err != nil {
		temporary.Close()
		return "", Manifest{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", Manifest{}, err
	}
	if err := temporary.Close(); err != nil {
		return "", Manifest{}, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", Manifest{}, err
	}
	if _, err := Verify(finalPath); err != nil {
		os.Remove(finalPath)
		return "", Manifest{}, err
	}
	digest, err := fileDigest(finalPath)
	if err != nil {
		return "", Manifest{}, err
	}
	if err := os.WriteFile(finalPath+".sha256", []byte(digest+"  "+filepath.Base(finalPath)+"\n"), 0o600); err != nil {
		return "", Manifest{}, err
	}
	return finalPath, manifest, nil
}

func Verify(path string) (Manifest, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, err
	}
	defer archive.Close()
	entries := make(map[string]*zip.File)
	for _, entry := range archive.File {
		entries[entry.Name] = entry
	}
	manifestEntry := entries[manifestName]
	if manifestEntry == nil {
		return Manifest{}, fmt.Errorf("backup manifest is missing")
	}
	reader, err := manifestEntry.Open()
	if err != nil {
		return Manifest{}, err
	}
	payload, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.SchemaVersion != 1 {
		return Manifest{}, fmt.Errorf("unsupported backup schema version %d", manifest.SchemaVersion)
	}
	if len(entries) != len(manifest.Files)+1 {
		return Manifest{}, fmt.Errorf("backup entry count does not match manifest")
	}
	seen := make(map[string]struct{})
	for _, record := range manifest.Files {
		if !safeRelative(record.Path) {
			return Manifest{}, fmt.Errorf("unsafe backup path %q", record.Path)
		}
		if _, duplicate := seen[record.Path]; duplicate {
			return Manifest{}, fmt.Errorf("duplicate backup path %q", record.Path)
		}
		seen[record.Path] = struct{}{}
		entry := entries[record.Path]
		if entry == nil {
			return Manifest{}, fmt.Errorf("backup file %s is missing", record.Path)
		}
		opened, err := entry.Open()
		if err != nil {
			return Manifest{}, err
		}
		hash := sha256.New()
		count, err := io.Copy(hash, opened)
		opened.Close()
		if err != nil {
			return Manifest{}, err
		}
		if count != record.Size || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
			return Manifest{}, fmt.Errorf("backup file %s failed checksum verification", record.Path)
		}
	}
	if content, err := os.ReadFile(path + ".sha256"); err == nil {
		fields := strings.Fields(string(content))
		digest, digestErr := fileDigest(path)
		if len(fields) == 0 || digestErr != nil || fields[0] != digest {
			return Manifest{}, fmt.Errorf("backup archive checksum verification failed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	}
	return manifest, nil
}

func Restore(path, target string, force bool) (RestoreReport, error) {
	manifest, err := Verify(path)
	if err != nil {
		return RestoreReport{}, err
	}
	target, _ = filepath.Abs(target)
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return RestoreReport{}, err
	}
	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 && !force {
		return RestoreReport{}, fmt.Errorf("restore target is not empty: %s", target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RestoreReport{}, err
	}
	staging, err := os.MkdirTemp(parent, ".restore-*")
	if err != nil {
		return RestoreReport{}, err
	}
	defer os.RemoveAll(staging)
	archive, err := zip.OpenReader(path)
	if err != nil {
		return RestoreReport{}, err
	}
	entries := make(map[string]*zip.File)
	for _, entry := range archive.File {
		entries[entry.Name] = entry
	}
	report := RestoreReport{Files: len(manifest.Files), Target: target}
	for _, record := range manifest.Files {
		destination := filepath.Join(staging, filepath.FromSlash(record.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			archive.Close()
			return RestoreReport{}, err
		}
		source, err := entries[record.Path].Open()
		if err != nil {
			archive.Close()
			return RestoreReport{}, err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			source.Close()
			archive.Close()
			return RestoreReport{}, err
		}
		count, copyErr := io.Copy(output, source)
		syncErr := output.Sync()
		closeErr := output.Close()
		source.Close()
		if copyErr != nil {
			archive.Close()
			return RestoreReport{}, copyErr
		}
		if syncErr != nil {
			archive.Close()
			return RestoreReport{}, syncErr
		}
		if closeErr != nil {
			archive.Close()
			return RestoreReport{}, closeErr
		}
		report.Bytes += count
	}
	archive.Close()
	old := ""
	if _, err := os.Stat(target); err == nil {
		old = target + ".pre-restore-" + time.Now().UTC().Format("20060102T150405")
		if err := os.Rename(target, old); err != nil {
			return RestoreReport{}, err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if old != "" {
			_ = os.Rename(old, target)
		}
		return RestoreReport{}, err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return report, nil
}

func collect(root string) ([]FileRecord, error) {
	records := make([]FileRecord, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ".receiver.lock" || strings.Contains(relative, ".tmp-") || strings.Contains(relative, ".corrupt-") || strings.Contains(relative, ".truncated-") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		digest, err := fileDigest(path)
		if err != nil {
			return err
		}
		records = append(records, FileRecord{Path: relative, Size: info.Size(), SHA256: digest})
		return nil
	})
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records, err
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func safeRelative(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
