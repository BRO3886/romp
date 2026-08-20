package config

import (
	"bufio"
	"encoding/json"
	"fmt"
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

// Discover returns commands found in common project entry-point files and
// documentation. It reads files only and never executes a discovered command.
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

	collectCI(root, add)
	collectMarkdown(root, add)
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

func collectCI(root string, add func(string, string)) {
	dir := filepath.Join(root, ".github", "workflows")
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		collectRunLines(path, add)
		return nil
	})
}

func collectRunLines(path string, add func(string, string)) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	blockIndent := -1
	for line := 1; scanner.Scan(); line++ {
		raw := scanner.Text()
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		text := strings.TrimSpace(raw)
		if blockIndent >= 0 {
			if text == "" {
				continue
			}
			if indent > blockIndent {
				add(text, fmt.Sprintf("%s:%d", filepath.ToSlash(filepath.Join(".github", "workflows", filepath.Base(path))), line))
				continue
			}
			blockIndent = -1
		}
		runPrefix := ""
		switch {
		case strings.HasPrefix(text, "run:"):
			runPrefix = "run:"
		case strings.HasPrefix(text, "- run:"):
			runPrefix = "- run:"
		}
		if runPrefix != "" {
			command := strings.TrimSpace(strings.TrimPrefix(text, runPrefix))
			if command == "|" || command == ">-" || command == ">" {
				blockIndent = indent
			} else {
				add(command, fmt.Sprintf("%s:%d", filepath.ToSlash(filepath.Join(".github", "workflows", filepath.Base(path))), line))
			}
		}
	}
}

func collectMarkdown(root string, add func(string, string)) {
	paths := []string{filepath.Join(root, "README.md"), filepath.Join(root, "CONTRIBUTING.md")}
	docs := filepath.Join(root, "docs")
	_ = filepath.WalkDir(docs, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	for _, path := range paths {
		collectMarkdownFile(root, path, add)
	}
}

func collectMarkdownFile(root, path string, add func(string, string)) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	inFence := false
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(text, "```") {
			inFence = !inFence
			continue
		}
		if !inFence || text == "" {
			continue
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, "$"))
		if strings.HasPrefix(text, "#") {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		add(text, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), line))
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
