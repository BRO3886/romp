package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	skillSourceRoot = "skills/rompify"
	skillDirName    = "rompify"
	versionFileName = ".romp-version"
)

// VersionFileName is the file used to track the installed skill version.
const VersionFileName = versionFileName

// AgentTarget identifies a supported agent skill directory.
type AgentTarget struct {
	Name    string
	Key     string
	BaseDir string
}

// DefaultTargets returns the supported agent skill directories below homeDir.
func DefaultTargets(homeDir string) []AgentTarget {
	return []AgentTarget{
		{Name: "Claude Code", Key: "claude", BaseDir: filepath.Join(homeDir, ".claude", "skills")},
		{Name: "Codex CLI", Key: "codex", BaseDir: filepath.Join(homeDir, ".agents", "skills")},
		{Name: "OpenClaw", Key: "openclaw", BaseDir: filepath.Join(homeDir, ".openclaw", "skills")},
	}
}

// SkillDir returns the installation directory for target.
func SkillDir(target AgentTarget) string {
	return filepath.Join(target.BaseDir, skillDirName)
}

// Files lists the embedded files installed for the skill.
func Files(embeddedFS fs.FS) ([]string, error) {
	var files []string
	hasEntrypoint := false
	err := fs.WalkDir(embeddedFS, skillSourceRoot, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relPath := strings.TrimPrefix(filePath, skillSourceRoot+"/")
		if relPath == filePath {
			return fmt.Errorf("embedded skill file %q is outside %q", filePath, skillSourceRoot)
		}
		if relPath == "SKILL.md" {
			hasEntrypoint = true
		}
		files = append(files, relPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list embedded skill files: %w", err)
	}
	if !hasEntrypoint {
		return nil, fmt.Errorf("embedded skill is missing SKILL.md")
	}
	return files, nil
}

// Install replaces target's installed skill with the embedded version.
func Install(embeddedFS fs.FS, target AgentTarget, version string) ([]string, error) {
	files, err := Files(embeddedFS)
	if err != nil {
		return nil, err
	}
	type bundledFile struct {
		relPath string
		data    []byte
	}
	bundle := make([]bundledFile, 0, len(files))
	for _, relPath := range files {
		data, err := fs.ReadFile(embeddedFS, path.Join(skillSourceRoot, filepath.ToSlash(relPath)))
		if err != nil {
			return nil, fmt.Errorf("read embedded skill file %q: %w", relPath, err)
		}
		bundle = append(bundle, bundledFile{relPath: relPath, data: data})
	}

	destination := SkillDir(target)
	if err := os.RemoveAll(destination); err != nil {
		return nil, fmt.Errorf("remove existing skill directory: %w", err)
	}
	for _, file := range bundle {
		relPath := file.relPath
		destinationPath := filepath.Join(destination, relPath)
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return nil, fmt.Errorf("create skill directory for %q: %w", relPath, err)
		}
		if err := os.WriteFile(destinationPath, file.data, 0o644); err != nil {
			return nil, fmt.Errorf("write skill file %q: %w", relPath, err)
		}
	}

	versionPath := filepath.Join(destination, versionFileName)
	if err := os.WriteFile(versionPath, []byte(version+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write skill version: %w", err)
	}
	return files, nil
}

// Uninstall removes target's skill directory.
func Uninstall(target AgentTarget) (bool, error) {
	destination := SkillDir(target)
	if _, err := os.Lstat(destination); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect skill directory: %w", err)
	}
	if err := os.RemoveAll(destination); err != nil {
		return false, fmt.Errorf("remove skill directory: %w", err)
	}
	return true, nil
}

// InstalledVersion reads target's installed skill version.
func InstalledVersion(target AgentTarget) string {
	data, err := os.ReadFile(filepath.Join(SkillDir(target), versionFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// IsInstalled reports whether target has a SKILL.md for rompify.
func IsInstalled(target AgentTarget) bool {
	_, err := os.Stat(filepath.Join(SkillDir(target), "SKILL.md"))
	return err == nil
}

// DetectAgents returns targets whose agent configuration directory exists.
func DetectAgents(targets []AgentTarget) []AgentTarget {
	var detected []AgentTarget
	for _, target := range targets {
		if _, err := os.Stat(filepath.Dir(target.BaseDir)); err == nil {
			detected = append(detected, target)
		}
	}
	return detected
}

// InstalledTargets returns targets that currently contain the skill.
func InstalledTargets(targets []AgentTarget) []AgentTarget {
	var installed []AgentTarget
	for _, target := range targets {
		if IsInstalled(target) {
			installed = append(installed, target)
		}
	}
	return installed
}

// DisplayPath replaces a path below homeDir with a leading tilde.
func DisplayPath(path, homeDir string) string {
	relPath, err := filepath.Rel(homeDir, path)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return path
	}
	if relPath == "." {
		return "~"
	}
	return filepath.Join("~", relPath)
}
