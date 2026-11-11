//go:build unix && !linux

package proc

import "syscall"

const termiosRequest = syscall.TIOCGETA
