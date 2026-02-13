//go:build darwin

package sop

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	// LOCAL_PEERPID is the macOS-specific socket option to retrieve the
	// peer process ID. Defined in <sys/un.h> as 0x002.
	localPeerPID = 0x002
)

// getPIDFromFD resolves the peer PID from a Unix-domain or local TCP socket
// using the macOS-specific LOCAL_PEERPID option.
func getPIDFromFD(fd uintptr) (uint32, error) {
	pid := int32(0)
	pidLen := uint32(unsafe.Sizeof(pid))

	_, _, errno := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		fd,
		0, // SOL_LOCAL (0 on macOS for local domain)
		uintptr(localPeerPID),
		uintptr(unsafe.Pointer(&pid)),
		uintptr(unsafe.Pointer(&pidLen)),
		0,
	)

	if errno != 0 {
		return 0, fmt.Errorf("LOCAL_PEERPID failed: %w", errno)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("LOCAL_PEERPID returned invalid PID: %d", pid)
	}

	return uint32(pid), nil
}
