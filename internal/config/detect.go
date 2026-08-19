package config

import (
	"os"
	"path/filepath"
)

// Detect guesses a repo's language from its build manifest and returns the
// build and test commands that should seed [verify], plus the language name.
// It returns empty strings when nothing is recognized; the caller refuses to
// write a romp.toml without at least a test command.
func Detect(root string) (build, test, lang string) {
	switch {
	case exists(filepath.Join(root, "go.mod")):
		return "go build ./...", "go test ./... -count=1", "go"
	case exists(filepath.Join(root, "Cargo.toml")):
		return "cargo build", "cargo test", "rust"
	case exists(filepath.Join(root, "pyproject.toml")):
		return "", "pytest", "python"
	case exists(filepath.Join(root, "Makefile")):
		return "", "make test", "make"
	case exists(filepath.Join(root, "package.json")):
		return "", "npm test", "node"
	default:
		return "", "", ""
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
