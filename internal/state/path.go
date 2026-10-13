// Package state resolves machine-local Romp paths without accessing storage.
package state

import (
	"os"
	"path/filepath"
)

// Dir returns the XDG state directory with the standard home fallback.
func Dir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "romp")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "romp")
}
