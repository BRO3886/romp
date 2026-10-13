package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/job"
	"github.com/BRO3886/romp/internal/protocol"
	"golang.org/x/mod/semver"
	"golang.org/x/sys/unix"
)

const AgentLabel = "com.romp.daemon"

// Environment preserves the installer-resolved execution paths for launchd.
type Environment struct {
	Path     string            `json:"path"`
	Programs map[string]string `json:"programs"`
}

// InstallPaths contains the stable user-agent installation locations.
type InstallPaths struct{ Binary, Environment, Plist string }

// Paths returns canonical locations independent of either client distribution.
func Paths() (InstallPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return InstallPaths{}, err
	}
	support := filepath.Join(home, "Library", "Application Support", "romp")
	return InstallPaths{filepath.Join(support, "bin", "rompd"), filepath.Join(support, "environment.json"), filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist")}, nil
}

// Preflight resolves installation inputs without writing files or starting jobs.
func Preflight(ctx context.Context, root, binary string) (Environment, error) {
	if runtime.GOOS != "darwin" {
		return Environment{}, fmt.Errorf("daemon installation requires macOS")
	}
	if !filepath.IsAbs(binary) {
		return Environment{}, fmt.Errorf("daemon binary must be absolute")
	}
	if _, err := binaryVersion(ctx, binary); err != nil {
		return Environment{}, err
	}
	protocolOutput, err := exec.CommandContext(ctx, binary, "--protocol-version").Output()
	if err != nil || strings.TrimSpace(string(protocolOutput)) != fmt.Sprint(protocol.Version) {
		return Environment{}, fmt.Errorf("daemon protocol is incompatible with this installer")
	}
	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		return Environment{}, err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	output, err := exec.CommandContext(ctx, shell, "-lc", "printf '%s' \"$PATH\"").Output()
	if err != nil {
		return Environment{}, fmt.Errorf("resolve login PATH: %w", err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return Environment{}, fmt.Errorf("login environment has empty PATH")
	}
	env := Environment{Path: path, Programs: map[string]string{}}
	names := []string{"git", "gh", cfg.Harness.Default}
	if cfg.Review.Enabled {
		names = append(names, cfg.ReviewHarness())
	}
	for _, name := range []string{"claude", "codex", "opencode"} {
		if _, err := resolveProgram(path, name); err == nil {
			names = append(names, name)
		}
	}
	for _, name := range names {
		program, err := resolveProgram(path, name)
		if err != nil {
			return Environment{}, err
		}
		env.Programs[name] = program
	}
	return env, nil
}

func resolveProgram(path, name string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s is not executable in daemon login PATH", name)
}

// LoadEnvironment restores the installer's PATH and verifies pinned programs.
func LoadEnvironment(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var env Environment
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.Path == "" || len(env.Programs) == 0 {
		return fmt.Errorf("invalid daemon environment")
	}
	if err := os.Setenv("PATH", env.Path); err != nil {
		return err
	}
	return os.Setenv("ROMP_DAEMON_ENVIRONMENT", path)
}

func validateEnvironment(env Environment) error {
	for name, path := range env.Programs {
		actual, err := resolveProgram(env.Path, name)
		if err != nil {
			return fmt.Errorf("stale daemon path %s: %w; rerun daemon install", path, err)
		}
		if actual != path {
			return fmt.Errorf("stale daemon path for %s: %s now resolves to %s; rerun daemon install", name, path, actual)
		}
	}
	return nil
}

// Install stages the canonical binary and the sole launchd plist atomically.
func Install(ctx context.Context, root, source string) (InstallPaths, error) {
	env, err := Preflight(ctx, root, source)
	if err != nil {
		return InstallPaths{}, err
	}
	paths, err := Paths()
	if err != nil {
		return paths, err
	}
	if err := os.MkdirAll(filepath.Dir(paths.Environment), 0700); err != nil {
		return paths, err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(paths.Environment), "install.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return paths, err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return paths, err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	newVersion, err := binaryVersion(ctx, source)
	if err != nil {
		return paths, err
	}
	if _, err := os.Stat(paths.Binary); err == nil {
		oldVersion, err := binaryVersion(ctx, paths.Binary)
		if err != nil {
			return paths, err
		}
		if err := allowUpgrade(oldVersion, newVersion); err != nil {
			return paths, err
		}
	} else if !os.IsNotExist(err) {
		return paths, err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return paths, err
	}
	if err := atomicWrite(paths.Binary, data, 0755); err != nil {
		return paths, err
	}
	data, err = json.MarshalIndent(env, "", "  ")
	if err != nil {
		return paths, err
	}
	if err := atomicWrite(paths.Environment, append(data, '\n'), 0600); err != nil {
		return paths, err
	}
	if err := atomicWrite(paths.Plist, []byte(agentPlist(paths, env)), 0600); err != nil {
		return paths, err
	}
	return paths, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".romp-install-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func binaryVersion(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("read daemon version: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != "rompd" {
		return "", fmt.Errorf("invalid rompd version output")
	}
	return fields[1], nil
}

func allowUpgrade(old, new string) error {
	if old == new {
		return nil
	}
	a := "v" + strings.TrimPrefix(old, "v")
	b := "v" + strings.TrimPrefix(new, "v")
	if !semver.IsValid(a) || !semver.IsValid(b) {
		return fmt.Errorf("cannot prove upgrade from %s to %s; refuse replacement", old, new)
	}
	if semver.Compare(b, a) < 0 {
		return fmt.Errorf("refuse daemon downgrade from %s to %s", old, new)
	}
	return nil
}

func agentPlist(paths InstallPaths, env Environment) string {
	escape := func(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() }
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>Program</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--environment</string><string>%s</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>%s</string><key>XDG_STATE_HOME</key><string>%s</string><key>XDG_CONFIG_HOME</key><string>%s</string></dict>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, AgentLabel, escape(paths.Binary), escape(paths.Binary), escape(paths.Environment), escape(env.Path), escape(filepath.Dir(job.StateDir())), escape(configDir()), escape(filepath.Join(filepath.Dir(paths.Environment), "daemon.log")), escape(filepath.Join(filepath.Dir(paths.Environment), "daemon.log")))
}

func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// Launch invokes launchctl in the caller's graphical login domain.
func Launch(ctx context.Context, action string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("launchd requires macOS")
	}
	paths, err := Paths()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	args := []string{action, domain, paths.Plist}
	if action == "print" {
		args = []string{action, domain + "/" + AgentLabel}
	}
	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}
