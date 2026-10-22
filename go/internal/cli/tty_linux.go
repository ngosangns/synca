//go:build linux

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// isTTY reports whether f is a terminal. TCGETS is the Linux counterpart of
// TIOCGETA and fails for /dev/null, which is a character device but not a tty.
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
