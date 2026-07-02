package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTokenFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream.token")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 48)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	token, err := ResolveToken(ResolveOptions{
		FilePath:      path,
		MinimumLength: 32,
		Production:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.Value() != strings.Repeat("a", 48) || token.Source() != "file" {
		t.Fatalf("token length=%d source=%q", len(token.Value()), token.Source())
	}
}

func TestResolveTokenRejectsAmbiguousAndWeakSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream.token")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 48)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveToken(ResolveOptions{Value: strings.Repeat("b", 48), FilePath: path}); err == nil {
		t.Fatal("expected ambiguous source error")
	}
	if _, err := ResolveToken(ResolveOptions{Value: "CHANGE_ME_STRONG_RANDOM_TOKEN", MinimumLength: 16}); err == nil {
		t.Fatal("expected placeholder token error")
	}
	if _, err := ResolveToken(ResolveOptions{Value: "short", MinimumLength: 32}); err == nil {
		t.Fatal("expected short token error")
	}
}
