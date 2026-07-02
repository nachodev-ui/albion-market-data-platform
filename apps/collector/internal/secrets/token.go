package secrets

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"unicode"
)

const maxTokenBytes = 16 << 10

type Token struct {
	value  string
	source string
}

type ResolveOptions struct {
	Value         string
	FilePath      string
	MinimumLength int
	Production    bool
}

func ResolveToken(options ResolveOptions) (Token, error) {
	value := strings.TrimSpace(options.Value)
	filePath := strings.TrimSpace(options.FilePath)
	if value != "" && filePath != "" {
		return Token{}, fmt.Errorf("token value and token file are mutually exclusive")
	}

	source := "environment"
	if filePath != "" {
		info, err := os.Stat(filePath)
		if err != nil {
			return Token{}, fmt.Errorf("read token file: %w", err)
		}
		if info.IsDir() {
			return Token{}, fmt.Errorf("token file path references a directory")
		}
		if info.Size() > maxTokenBytes {
			return Token{}, fmt.Errorf("token file exceeds %d bytes", maxTokenBytes)
		}
		if options.Production && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return Token{}, fmt.Errorf("token file permissions are too broad; use 0600 or stricter")
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return Token{}, fmt.Errorf("read token file: %w", err)
		}
		value = strings.TrimSpace(string(content))
		source = "file"
	}

	if value == "" {
		return Token{}, fmt.Errorf("token is required")
	}
	minimumLength := options.MinimumLength
	if minimumLength <= 0 {
		minimumLength = 32
	}
	if minimumLength < 16 || minimumLength > maxTokenBytes {
		return Token{}, fmt.Errorf("minimum token length must be between 16 and %d", maxTokenBytes)
	}
	if options.Production && minimumLength < 32 {
		return Token{}, fmt.Errorf("minimum token length must be at least 32 in production")
	}
	if len(value) < minimumLength {
		return Token{}, fmt.Errorf("token must contain at least %d characters", minimumLength)
	}
	if len(value) > maxTokenBytes {
		return Token{}, fmt.Errorf("token exceeds %d characters", maxTokenBytes)
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return Token{}, fmt.Errorf("token must not contain whitespace or control characters")
		}
	}
	upper := strings.ToUpper(value)
	if strings.Contains(upper, "CHANGE_ME") || strings.Contains(upper, "REPLACE_ME") {
		return Token{}, fmt.Errorf("token still contains a placeholder")
	}
	switch strings.ToLower(value) {
	case "secret", "token", "password", "changeme":
		return Token{}, fmt.Errorf("token is a known insecure placeholder")
	}

	return Token{value: value, source: source}, nil
}

func (t Token) Value() string {
	return t.value
}

func (t Token) Source() string {
	return t.source
}
