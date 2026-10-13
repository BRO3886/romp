// Command rompd is the machine-wide Romp server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/BRO3886/romp/internal/daemon"
	"github.com/BRO3886/romp/internal/protocol"
)

var version, commit, buildTime = "dev", "none", "unknown"

func main() {
	showVersion := flag.Bool("version", false, "print daemon version")
	showProtocol := flag.Bool("protocol-version", false, "print protocol version")
	environment := flag.String("environment", "", "installer-resolved runtime environment")
	flag.Parse()
	if *showProtocol {
		fmt.Println(protocol.Version)
		return
	}
	if *showVersion {
		fmt.Printf("rompd %s (commit %s, built %s)\n", version, commit, buildTime)
		return
	}
	if *environment != "" {
		if err := daemon.LoadEnvironment(*environment); err != nil {
			fmt.Fprintln(os.Stderr, "rompd:", err)
			os.Exit(1)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, version); err != nil {
		fmt.Fprintln(os.Stderr, "rompd:", err)
		os.Exit(1)
	}
}
