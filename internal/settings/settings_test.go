package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv(EnvPath, path)

	if got := Load(); got.Flavor != flavor.DefaultID {
		t.Fatalf("Load() without a file = %q, want the default", got.Flavor)
	}
	if err := Save(Settings{Flavor: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.Flavor != "gitlab" || got.MarkdownFlavor().ID != "gitlab" {
		t.Fatalf("Load() after Save = %+v", got)
	}
}

func TestLoadIgnoresBadFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)

	for _, content := range []string{"not json", `{"flavor": "no-such-flavor"}`} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Load(); got.Flavor != flavor.DefaultID {
			t.Errorf("Load() with %q = %q, want the default", content, got.Flavor)
		}
	}
}
