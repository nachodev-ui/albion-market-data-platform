package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileLoadsValuesWithoutOverwritingEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := []byte("# comment\nOBS_TEST_LOADED=plain\nOBS_TEST_QUOTED=\"quoted value\"\nOBS_TEST_EXISTING=from-file\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"OBS_TEST_LOADED", "OBS_TEST_QUOTED", "OBS_TEST_EXISTING"} {
		original, existed := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		key := key
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, original)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
	if err := os.Setenv("OBS_TEST_EXISTING", "from-environment"); err != nil {
		t.Fatal(err)
	}

	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv("OBS_TEST_LOADED"); value != "plain" {
		t.Fatalf("loaded = %q", value)
	}
	if value := os.Getenv("OBS_TEST_QUOTED"); value != "quoted value" {
		t.Fatalf("quoted = %q", value)
	}
	if value := os.Getenv("OBS_TEST_EXISTING"); value != "from-environment" {
		t.Fatalf("existing = %q", value)
	}
}
