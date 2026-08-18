// Command romp labels an issue and gets a pull request.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/BRO3886/romp/internal/runner"
)

// Set by ldflags at build time.
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		if errors.Is(err, runner.ErrBlocked) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "romp:", err)
		os.Exit(1)
	}
}
