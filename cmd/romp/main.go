// Command romp labels an issue and gets a pull request.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "romp:", err)
		os.Exit(1)
	}
}
