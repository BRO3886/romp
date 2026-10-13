package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/BRO3886/romp/internal/daemon"
	"github.com/spf13/cobra"
)

// doctorCheck is one item romp doctor verifies. run returns a short
// human-readable detail on success, or an error with an actionable message.
type doctorCheck struct {
	name string
	run  func(ctx context.Context) (detail string, err error)
}

func newDoctorCmd(checks ...doctorCheck) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check gh auth, harness, git version, and config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(checks) != 0 {
				return runDoctor(cmd.OutOrStdout(), cmd.Context(), checks)
			}
			root, err := repoRoot(cmd.Context())
			if err != nil {
				return err
			}
			var results []daemon.Check
			if err := daemonCall(cmd.Context(), "doctor", daemon.Params{Path: root}, &results); err != nil {
				return err
			}
			failed := 0
			for _, r := range results {
				status := "ok"
				if !r.OK {
					status = "FAIL"
					failed++
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", r.Name, r.Detail, status)
			}
			if failed > 0 {
				return fmt.Errorf("%d daemon checks failed", failed)
			}
			return nil
		},
	}
}

func runDoctor(out io.Writer, ctx context.Context, checks []doctorCheck) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	var failed int
	for _, c := range checks {
		detail, err := c.run(ctx)
		status := "ok"
		if err != nil {
			failed++
			status = "FAIL"
			detail = err.Error()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.name, detail, status)
	}
	w.Flush()
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}
	return nil
}
