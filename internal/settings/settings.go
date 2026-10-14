// Package settings persists the user's markout preferences between runs.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/lang"
	"github.com/hkmt-sw/markout/internal/theme"
)

// EnvPath names an environment variable that overrides the settings file
// location.
const EnvPath = "MARKOUT_CONFIG"

// Settings are the preferences stored in the settings file.
type Settings struct {
	// Flavor is the ID of the Markdown flavor used for conversions.
	Flavor string `json:"flavor"`

	// Theme is the name or file path of the theme that styles the output.
	Theme string `json:"theme,omitempty"`

	// Lang is the language of documents that do not name one themselves, as
	// a language tag ("hu", "en-GB"). Empty means none.
	Lang string `json:"lang,omitempty"`

	// PDFA makes PDF output PDF/A-2u, for archiving.
	PDFA bool `json:"pdfa,omitempty"`

	// NoUpdateCheck turns off the automatic check for a newer release.
	NoUpdateCheck bool `json:"no_update_check,omitempty"`
	// UpdateCheckedAt and LatestVersion remember the last check, so it runs
	// at most once a day.
	UpdateCheckedAt time.Time `json:"update_checked_at,omitzero"`
	LatestVersion   string    `json:"latest_version,omitempty"`
}

// Path returns the location of the settings file: $MARKOUT_CONFIG if set,
// otherwise markout/config.json in the user's configuration directory.
func Path() (string, error) {
	if p := os.Getenv(EnvPath); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "markout", "config.json"), nil
}

// Load reads the settings file. A missing or unreadable file yields the
// defaults.
func Load() Settings {
	s := Settings{Flavor: flavor.DefaultID}
	path, err := Path()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var stored Settings
	if json.Unmarshal(data, &stored) != nil {
		return s
	}
	if _, ok := flavor.ByID(stored.Flavor); !ok {
		stored.Flavor = flavor.DefaultID
	}
	// A language written by hand may be in another form, or not one at all
	stored.Lang, _ = lang.Normalize(stored.Lang)
	return stored
}

// Save writes the settings file, creating its directory if needed.
func Save(s Settings) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ThemesDir is where the user's own themes are kept, next to the settings
// file: one NAME.toml per theme.
func ThemesDir() string {
	path, err := Path()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "themes")
}

// Themes finds themes by name, including the user's own.
func Themes() theme.Loader {
	return theme.Loader{UserDir: ThemesDir()}
}

// MarkdownFlavor resolves the configured flavor, falling back to the default.
func (s Settings) MarkdownFlavor() flavor.Flavor {
	if f, ok := flavor.ByID(s.Flavor); ok {
		return f
	}
	return flavor.Default()
}
