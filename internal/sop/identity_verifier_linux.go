//go:build linux

package sop

import (
	"fmt"
	"syscall"
	"unsafe"
)

// getPIDFromFD resolves the peer PID from a Unix-domain or local TCP socket
// using the Linux-specific SO_PEERCRED option.
func getPIDFromFD(fd uintptr) (uint32, error) {
	ucred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return 0, fmt.Errorf("SO_PEERCRED failed: %w", err)
	}
	_ = unsafe.Sizeof(ucred) // ensure import is used
	return uint32(ucred.Pid), nil
}
