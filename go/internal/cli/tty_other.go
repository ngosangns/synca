//go:build !darwin && !linux

package cli

import "os"

// isTTY is false on platforms without a termios ioctl in package syscall.
func isTTY(_ *os.File) bool { return false }
