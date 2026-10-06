// Package settings persists the user's markout preferences between runs.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hkmt-sw/markout/internal/flavor"
)

// EnvPath names an environment variable that overrides the settings file
// location.
const EnvPath = "MARKOUT_CONFIG"

// Settings are the preferences stored in the settings file.
type Settings struct {
	// Flavor is the ID of the Markdown flavor used for conversions.
	Flavor string `json:"flavor"`
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
	if json.Unmarshal(data, &stored) == nil {
		if _, ok := flavor.ByID(stored.Flavor); ok {
			s.Flavor = stored.Flavor
		}
	}
	return s
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

// MarkdownFlavor resolves the configured flavor, falling back to the default.
func (s Settings) MarkdownFlavor() flavor.Flavor {
	if f, ok := flavor.ByID(s.Flavor); ok {
		return f
	}
	return flavor.Default()
}
