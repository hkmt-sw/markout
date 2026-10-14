package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hkmt-sw/markout/internal/flavor"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv(EnvPath, path)

	if got := mustLoad(t); got.Flavor != flavor.DefaultID {
		t.Fatalf("Load() without a file = %q, want the default", got.Flavor)
	}
	if err := Save(Settings{Flavor: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	got := mustLoad(t)
	if got.Flavor != "gitlab" || got.MarkdownFlavor().ID != "gitlab" {
		t.Fatalf("Load() after Save = %+v", got)
	}
}

// mustLoad loads the settings and fails the test if that is an error.
func mustLoad(t *testing.T) Settings {
	t.Helper()
	s, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	return s
}

// A file that cannot be parsed gives the defaults and an error, so that the
// caller knows not to write the defaults over it.
func TestLoadReportsBrokenFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)

	for _, content := range []string{"not json", "", `{"flavor": "gitlab",}`, `{"flavor": 3}`} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Load()
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Load() with %q gave the error %v, want one that names the file", content, err)
		}
		if got.Flavor != flavor.DefaultID {
			t.Errorf("Load() with %q = %q, want the default", content, got.Flavor)
		}
	}
}

// A file that cannot be read is reported too; a directory is one.
func TestLoadReportsUnreadableFiles(t *testing.T) {
	t.Setenv(EnvPath, t.TempDir())
	if _, err := Load(); err == nil {
		t.Error("Load() of a directory gave no error")
	}
}

// A flavor the program does not know is replaced by the default without an
// error, as it may come from another version.
func TestLoadReplacesAnUnknownFlavor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)
	if err := os.WriteFile(path, []byte(`{"flavor": "no-such-flavor"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t); got.Flavor != flavor.DefaultID {
		t.Errorf("Load() = %q, want the default", got.Flavor)
	}
}

func TestSaveIsPrivateAndLeavesNoTemporaryFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "markout")
	t.Setenv(EnvPath, filepath.Join(dir, "config.json"))
	if err := Save(Settings{Flavor: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("the directory holds %v (%v), want only config.json", entries, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "config.json"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		// The umask can only take permissions away, and these are the least
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %o, want %o", path, got, want)
		}
	}
}

// A save that fails must leave the file as it was.
func TestFailedSaveKeepsTheFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions work differently on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(EnvPath, path)
	if err := Save(Settings{Flavor: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only directory")
	}

	if err := Save(Settings{Flavor: "obsidian"}); err == nil {
		t.Fatal("Save() into a read-only directory gave no error")
	}
	if got := mustLoad(t); got.Flavor != "gitlab" {
		t.Errorf("the file holds %q, want the old settings", got.Flavor)
	}
}

func TestSetAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(EnvPath, path)
	if err := os.WriteFile(path, []byte("my typo"), 0o644); err != nil {
		t.Fatal(err)
	}

	backup, err := SetAside()
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".bak" {
		t.Errorf("SetAside() = %q, want %q", backup, path+".bak")
	}
	if got, _ := os.ReadFile(backup); string(got) != "my typo" {
		t.Errorf("the backup holds %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file is still in place: %v", err)
	}
	// The settings can be saved now, and the backup stays
	if err := Save(Settings{Flavor: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(backup); string(got) != "my typo" {
		t.Errorf("the backup holds %q after a save", got)
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
	got := mustLoad(t)
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
		if got := mustLoad(t).Lang; got != want {
			t.Errorf("lang %q is read as %q, want %q", written, got, want)
		}
	}
}

// PDF/A is off unless it was saved on, and the file does not mention it
// when it is off.
func TestPDFASetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvPath, path)

	if mustLoad(t).PDFA {
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
	if got := mustLoad(t); !got.PDFA || got.Flavor != "gitlab" {
		t.Errorf("Load() = %+v", got)
	}
}
