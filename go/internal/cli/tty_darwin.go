//go:build darwin

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// isTTY reports whether f is a terminal. A character device such as /dev/null
// passes ModeCharDevice, so the check is TIOCGETA, which fails with ENOTTY there.
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
