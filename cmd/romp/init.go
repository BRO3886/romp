package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
)

const gitignoreEntry = ".romp/local.toml"

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Write romp.toml, create the label, and update .gitignore",
		Args:  cobra.NoArgs,
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	root, err := repoRoot(ctx)
	if err != nil {
		return err
	}

	build, test, lang := config.Detect(root)

	path := filepath.Join(root, "romp.toml")
	if _, err := os.Stat(path); err == nil {
		cmd.Printf("romp.toml already exists; leaving it untouched\n")
	} else if os.IsNotExist(err) {
		if test == "" {
			cmd.Printf("could not detect the language; add a [verify] test command to %s manually\n", path)
		} else if err := os.WriteFile(path, []byte(seedConfig(build, test, lang)), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		} else {
			cmd.Printf("wrote %s\n", path)
		}
	} else {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	cfg, err := config.Load(root, config.Overrides{})
	if err != nil {
		return err
	}

	owner, name, err := (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}
	ownerName := owner + "/" + name

	if err := (&gh.Client{}).CreateLabel(ctx, ownerName, cfg.Label); err != nil {
		return fmt.Errorf("creating label %q: %w", cfg.Label, err)
	}
	cmd.Printf("label %q ready on %s\n", cfg.Label, ownerName)

	if err := ensureGitignore(root); err != nil {
		return err
	}
	cmd.Printf("updated .gitignore\n")
	return nil
}

func seedConfig(build, test, lang string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s project\n", lang)
	b.WriteString("label   = \"romp\"\n")
	b.WriteString("width   = 3\n")
	b.WriteString("timeout = \"25m\"\n\n")
	b.WriteString("[verify]\n")
	if build != "" {
		fmt.Fprintf(&b, "build = %q\n", build)
	}
	fmt.Fprintf(&b, "test  = %q\n\n", test)
	b.WriteString("[harness]\ndefault = \"claude\"\n")
	return b.String()
}

// ensureGitignore appends .romp/local.toml to the repo's .gitignore when it
// is not already ignored.
func ensureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading .gitignore: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == gitignoreEntry {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening .gitignore: %w", err)
	}
	defer f.Close()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString(gitignoreEntry + "\n"); err != nil {
		return err
	}
	return nil
}
