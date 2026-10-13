package daemon

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func peerAllowed(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		credErr = err
		if err == nil {
			uid = cred.Uid
		}
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if uid != uint32(os.Geteuid()) {
		return fmt.Errorf("peer user differs from daemon user")
	}
	return nil
}

func memoryPressure() (string, error) {
	level, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	if err != nil {
		return "unknown", err
	}
	switch level {
	case 1:
		return "normal", nil
	case 2:
		return "warning", nil
	case 4:
		return "critical", nil
	default:
		return "unknown", fmt.Errorf("unknown memory pressure level %d", level)
	}
}
