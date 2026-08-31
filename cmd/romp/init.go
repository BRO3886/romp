package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/BRO3886/romp/internal/config"
	"github.com/BRO3886/romp/internal/gh"
	"github.com/BRO3886/romp/internal/git"
)

const gitignoreEntry = ".romp/local.toml"

func newInitCmd() *cobra.Command {
	var verify []string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Choose verification commands, write romp.toml, and create labels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, verify)
		},
	}
	cmd.Flags().StringArrayVar(&verify, "verify", nil, "verification command (repeatable)")
	return cmd
}

func runInit(cmd *cobra.Command, verifyFlags []string) error {
	ctx := cmd.Context()
	root, err := repoRoot(ctx)
	if err != nil {
		return err
	}

	path := filepath.Join(root, "romp.toml")
	if _, err := os.Stat(path); err == nil {
		cmd.Printf("romp.toml already exists; leaving it untouched\n")
	} else if os.IsNotExist(err) {
		commands := verifyFlags
		reviewEnabled := true
		interactive := isInteractive(cmd)
		if interactive {
			var err error
			commands, err = chooseVerifyCommands(cmd.InOrStdin(), cmd.OutOrStdout(), config.Discover(root), verifyFlags)
			if err != nil {
				return err
			}
			reviewEnabled, err = chooseReviewEnabled(cmd.InOrStdin(), cmd.OutOrStdout())
			if err != nil {
				return err
			}
		} else if len(commands) == 0 {
			return fmt.Errorf("non-interactive init requires at least one --verify command")
		}
		if len(commands) == 0 {
			return fmt.Errorf("at least one verification command is required")
		}
		commands, err = normalizeVerifyCommands(commands)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(seedConfig(commands, reviewEnabled)), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		} else {
			cmd.Printf("wrote %s\n", path)
		}
	} else {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	cfg, err := loadConfig(root, config.Overrides{}, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	owner, name, err := (&git.Repo{Root: root}).Origin(ctx)
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}
	ownerName := owner + "/" + name

	client := &gh.Client{}
	for _, l := range initLabels(cfg) {
		if err := client.CreateLabel(ctx, ownerName, l.name, l.desc); err != nil {
			return fmt.Errorf("creating label %q: %w", l.name, err)
		}
		cmd.Printf("label %q ready on %s\n", l.name, ownerName)
	}

	if err := ensureGitignore(root); err != nil {
		return err
	}
	cmd.Printf("updated .gitignore\n")
	return nil
}

func isInteractive(cmd *cobra.Command) bool {
	in := cmd.InOrStdin()
	out := cmd.OutOrStdout()
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	return inOK && outOK &&
		(isatty.IsTerminal(inFile.Fd()) || isatty.IsCygwinTerminal(inFile.Fd())) &&
		(isatty.IsTerminal(outFile.Fd()) || isatty.IsCygwinTerminal(outFile.Fd()))
}

func seedConfig(commands []string, reviewEnabled bool) string {
	var b strings.Builder
	b.WriteString("label          = \"romp\"\n")
	b.WriteString("claimed_label  = \"romp:claimed\"\n")
	b.WriteString("blocked_label  = \"romp:blocked\"\n")
	b.WriteString("width          = 3\n\n")
	b.WriteString("[verify]\n")
	b.WriteString("commands = [\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "  %q,\n", command)
	}
	b.WriteString("]\n\n")
	b.WriteString("[harness]\ndefault = \"codex\"\n")
	fmt.Fprintf(&b, "\n[review]\nenabled = %t\n", reviewEnabled)
	return b.String()
}

func chooseReviewEnabled(in io.Reader, out io.Writer) (bool, error) {
	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "Enable review gate? [Y/n] ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return false, fmt.Errorf("reading review choice: %w", err)
			}
			return true, nil
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Enter yes or no.")
		}
	}
}

type repoLabel struct {
	name string
	desc string
}

func initLabels(cfg *config.Config) []repoLabel {
	return []repoLabel{
		{cfg.Label, "romp will pick this up and open a pull request"},
		{cfg.ClaimedLabel, "a romp job is working this issue"},
		{cfg.BlockedLabel, "romp stopped; the issue is under-scoped"},
	}
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
