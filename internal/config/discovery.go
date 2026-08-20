package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Candidate is a verification command found in repository files. Discovery
// provides context only; it does not assess whether the command is correct.
type Candidate struct {
	Command string
	Sources []string
}

var makeTargetPattern = regexp.MustCompile(`^([A-Za-z0-9_./% -]+):`)

// Discover returns commands found in project configuration and entry-point
// files. It reads files only and never executes a discovered command.
func Discover(root string) []Candidate {
	var found []Candidate
	add := func(command, source string) {
		command = strings.TrimSpace(command)
		if command == "" {
			return
		}
		for i := range found {
			if found[i].Command == command {
				if !containsSource(found[i].Sources, source) {
					found[i].Sources = append(found[i].Sources, source)
				}
				return
			}
		}
		found = append(found, Candidate{Command: command, Sources: []string{source}})
	}

	if data, err := os.ReadFile(filepath.Join(root, "Makefile")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			match := makeTargetPattern.FindStringSubmatch(line)
			if len(match) != 2 || strings.HasPrefix(strings.TrimSpace(match[1]), ".") {
				continue
			}
			for _, target := range strings.Fields(match[1]) {
				add("make "+target, `Makefile target "`+target+`"`)
			}
		}
	}

	collectPackageScripts(root, add)
	if fileExists(filepath.Join(root, "go.mod")) {
		add("go test ./...", "go.mod")
	}
	if fileExists(filepath.Join(root, "Cargo.toml")) {
		add("cargo test", "Cargo.toml")
	}
	if fileExists(filepath.Join(root, "pyproject.toml")) {
		add("pytest", "pyproject.toml")
	}

	return found
}

func collectPackageScripts(root string, add func(string, string)) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "package.json" {
			return nil
		}
		collectPackageJSON(root, path, add)
		return nil
	})
}

func collectPackageJSON(root, path string, add func(string, string)) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var pkg struct {
		Scripts        map[string]string `json:"scripts"`
		PackageManager string            `json:"packageManager"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return
	}
	dir, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || dir == "." {
		dir = ""
	}
	manager := "npm"
	managerDir := filepath.Dir(path)
	switch {
	case fileExists(filepath.Join(managerDir, "pnpm-lock.yaml")) || fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		manager = "pnpm"
	case fileExists(filepath.Join(managerDir, "yarn.lock")) || fileExists(filepath.Join(root, "yarn.lock")):
		manager = "yarn"
	case fileExists(filepath.Join(managerDir, "bun.lockb")) || fileExists(filepath.Join(managerDir, "bun.lock")) || fileExists(filepath.Join(root, "bun.lockb")) || fileExists(filepath.Join(root, "bun.lock")):
		manager = "bun"
	case strings.HasPrefix(pkg.PackageManager, "pnpm@"):
		manager = "pnpm"
	case strings.HasPrefix(pkg.PackageManager, "yarn@"):
		manager = "yarn"
	case strings.HasPrefix(pkg.PackageManager, "bun@"):
		manager = "bun"
	}
	keys := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		command := ""
		switch manager {
		case "npm":
			command = "npm"
			if dir != "" {
				command += " --prefix " + filepath.ToSlash(dir)
			}
			command += " run " + name
		case "pnpm":
			command = "pnpm"
			if dir != "" {
				command += " --dir " + filepath.ToSlash(dir)
			}
			command += " run " + name
		case "yarn":
			command = "yarn"
			if dir != "" {
				command += " --cwd " + filepath.ToSlash(dir)
			}
			command += " " + name
		case "bun":
			command = "bun"
			if dir != "" {
				command += " --cwd " + filepath.ToSlash(dir)
			}
			command += " run " + name
		}
		source := filepath.ToSlash(filepath.Join(dir, "package.json")) + ` script "` + name + `"`
		add(command, source)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func containsSource(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
