package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"albion-market-data/collector/internal/instancelock"
	"albion-market-data/collector/internal/storage/backup"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: storagectl <backup|verify|restore>")
	}
	switch args[0] {
	case "backup":
		flags := flag.NewFlagSet("backup", flag.ContinueOnError)
		data := flags.String("data", "./data", "")
		output := flags.String("output", "./backups", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		lock, err := instancelock.Acquire(filepath.Join(*data, ".receiver.lock"))
		if err != nil {
			return fmt.Errorf("stop the receiver before backup: %w", err)
		}
		defer lock.Close()
		path, manifest, err := backup.Create(*data, *output, time.Now())
		if err != nil {
			return err
		}
		fmt.Printf("Backup=%s\nFiles=%d\n", path, len(manifest.Files))
		return nil
	case "verify":
		flags := flag.NewFlagSet("verify", flag.ContinueOnError)
		path := flags.String("backup", "", "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		manifest, err := backup.Verify(*path)
		if err != nil {
			return err
		}
		fmt.Printf("Verified=%s\nFiles=%d\n", *path, len(manifest.Files))
		return nil
	case "restore":
		flags := flag.NewFlagSet("restore", flag.ContinueOnError)
		path := flags.String("backup", "", "")
		target := flags.String("target", "./data", "")
		force := flags.Bool("force", false, "")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		lock, err := instancelock.Acquire(filepath.Join(*target, ".receiver.lock"))
		if err != nil {
			return fmt.Errorf("stop the receiver before restore: %w", err)
		}
		if err := lock.Close(); err != nil {
			return err
		}
		report, err := backup.Restore(*path, *target, *force)
		if err != nil {
			return err
		}
		fmt.Printf("Restored=%s\nFiles=%d\nBytes=%d\n", report.Target, report.Files, report.Bytes)
		return nil
	default:
		return fmt.Errorf("unknown storagectl command %q", args[0])
	}
}
