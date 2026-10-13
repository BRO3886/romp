// Command romp labels an issue and gets a pull request.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/BRO3886/romp/internal/runner"
)

// Set by ldflags at build time.
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		if errors.Is(err, runner.ErrBlocked) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "romp:", err)
		os.Exit(1)
	}
}
