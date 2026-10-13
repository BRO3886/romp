//go:build !darwin

package daemon

import (
	"fmt"
	"net"
)

func peerAllowed(*net.UnixConn) error { return fmt.Errorf("rompd requires macOS peer credentials") }
func memoryPressure() (string, error) {
	return "unknown", fmt.Errorf("rompd admission requires macOS memory pressure")
}
