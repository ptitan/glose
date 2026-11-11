//go:build unix

package proc

import (
	"os"
	"syscall"
	"unsafe"
)

// Interactive reports whether the standard input of this process is a
// terminal (not a pipe, a file or /dev/null): a person typed the command.
// An agent launched by Glose has none (D-64).
func Interactive() bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), uintptr(termiosRequest), uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
