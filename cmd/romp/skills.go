package main

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	romp "github.com/BRO3886/romp"
	"github.com/BRO3886/romp/internal/skills"
	"github.com/spf13/cobra"
)

type homeDirFunc func() (string, error)

func newSkillsCmd(embeddedFS fs.FS, binaryVersion string, homeDir homeDirFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage the bundled rompify agent skill",
		Long:  "Install, uninstall, and inspect the bundled skill that converts specifications and GitHub issues into Romp-ready execution contracts.",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSkillsInstallCmd(embeddedFS, binaryVersion, homeDir))
	cmd.AddCommand(newSkillsUninstallCmd(homeDir))
	cmd.AddCommand(newSkillsStatusCmd(binaryVersion, homeDir))
	return cmd
}

func newSkillsInstallCmd(embeddedFS fs.FS, binaryVersion string, homeDir homeDirFunc) *cobra.Command {
	var agent string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the rompify skill for AI agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := homeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			targets, err := resolveSkillTargets(skills.DefaultTargets(home), agent, "install")
			if err != nil {
				return err
			}
			if dryRun {
				return printSkillDryRun(cmd, embeddedFS, targets, home)
			}
			for _, target := range targets {
				written, err := skills.Install(embeddedFS, target, binaryVersion)
				if err != nil {
					return fmt.Errorf("install skill for %s: %w", target.Name, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Installed rompify skill to %s\n", skills.DisplayPath(skills.SkillDir(target), home))
				fmt.Fprintf(cmd.OutOrStdout(), "  Files: %s\n", strings.Join(written, ", "))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "The skill will be available in the next agent session.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&agent, "agent", "a", "", "agent target: claude, codex, openclaw, or all")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview files without writing them")
	return cmd
}

func newSkillsUninstallCmd(homeDir homeDirFunc) *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the rompify skill from AI agents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := homeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			targets, err := resolveSkillTargets(skills.DefaultTargets(home), agent, "uninstall")
			if err != nil {
				return err
			}
			for _, target := range targets {
				removed, err := skills.Uninstall(target)
				if err != nil {
					return fmt.Errorf("uninstall skill from %s: %w", target.Name, err)
				}
				if removed {
					fmt.Fprintf(cmd.OutOrStdout(), "Removed rompify skill from %s\n", skills.DisplayPath(skills.SkillDir(target), home))
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Not installed at %s\n", skills.DisplayPath(skills.SkillDir(target), home))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&agent, "agent", "a", "", "agent target: claude, codex, openclaw, or all")
	return cmd
}

func newSkillsStatusCmd(binaryVersion string, homeDir homeDirFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show rompify skill installation status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := homeDir()
			if err != nil {
				return fmt.Errorf("resolve home directory: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rompify skill (binary %s):\n", binaryVersion)
			for _, target := range skills.DefaultTargets(home) {
				path := skills.DisplayPath(skills.SkillDir(target), home)
				if !skills.IsInstalled(target) {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s (not installed)\n", target.Name, path)
					continue
				}
				installedVersion := skills.InstalledVersion(target)
				switch {
				case installedVersion == "":
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s (installed, unknown version)\n", target.Name, path)
				case installedVersion != binaryVersion:
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s (installed %s, outdated)\n", target.Name, path, installedVersion)
				default:
					fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %s (installed %s)\n", target.Name, path, installedVersion)
				}
			}
			return nil
		},
	}
}

func resolveSkillTargets(targets []skills.AgentTarget, agent, action string) ([]skills.AgentTarget, error) {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "all" {
		return targets, nil
	}
	if agent != "" {
		for _, target := range targets {
			if target.Key == agent {
				return []skills.AgentTarget{target}, nil
			}
		}
		return nil, fmt.Errorf("unknown agent %q (valid: claude, codex, openclaw, all)", agent)
	}
	if action == "uninstall" {
		installed := skills.InstalledTargets(targets)
		if len(installed) == 0 {
			return nil, fmt.Errorf("rompify is not installed for a supported agent; use --agent to select a target")
		}
		return installed, nil
	}
	detected := skills.DetectAgents(targets)
	if len(detected) == 0 {
		return nil, fmt.Errorf("no supported agent directories detected; use --agent to select claude, codex, openclaw, or all")
	}
	return detected, nil
}

func printSkillDryRun(cmd *cobra.Command, embeddedFS fs.FS, targets []skills.AgentTarget, homeDir string) error {
	files, err := skills.Files(embeddedFS)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Dry run: no files will be written.")
	for _, target := range targets {
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s):\n", target.Name, skills.DisplayPath(skills.SkillDir(target), homeDir))
		for _, file := range files {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", file)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", skills.VersionFileName)
	}
	return nil
}

func defaultSkillsCmd() *cobra.Command {
	return newSkillsCmd(romp.EmbeddedSkills, version, os.UserHomeDir)
}
