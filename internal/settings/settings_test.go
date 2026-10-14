package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// Saving one preference must not lose the others.
func TestAllFieldsRoundTrip(t *testing.T) {
	t.Setenv(EnvPath, filepath.Join(t.TempDir(), "config.json"))
	when := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	want := Settings{Flavor: "obsidian", Lang: "en-GB", PDFA: true, NoUpdateCheck: true, UpdateCheckedAt: when, LatestVersion: "v1.2.0"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.Flavor != want.Flavor || got.Lang != "en-GB" || !got.PDFA || !got.NoUpdateCheck || !got.UpdateCheckedAt.Equal(when) || got.LatestVersion != "v1.2.0" {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// A language written into the file by hand is read in its usual form, and
// one that is not a language is not read at all.
func TestLoadNormalizesTheLanguage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)
	for written, want := range map[string]string{"hu_HU": "hu-HU", "EN-gb": "en-GB", "magyar": "", "": ""} {
		if err := os.WriteFile(path, []byte(`{"flavor": "markout", "lang": "`+written+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Load().Lang; got != want {
			t.Errorf("lang %q is read as %q, want %q", written, got, want)
		}
	}
}

// PDF/A is off unless it was saved on, and the file does not mention it
// when it is off.
func TestPDFASetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)

	if Load().PDFA {
		t.Error("PDF/A is on without a settings file")
	}
	if err := Save(Settings{Flavor: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "pdfa") {
		t.Errorf("the file mentions pdfa while it is off: %s", data)
	}
	if err := Save(Settings{Flavor: "gitlab", PDFA: true}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"pdfa": true`) {
		t.Errorf("the file does not say pdfa: %s", data)
	}
	if got := Load(); !got.PDFA || got.Flavor != "gitlab" {
		t.Errorf("Load() = %+v", got)
	}
}
