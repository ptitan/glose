//go:build linux

package proc

import "syscall"

const termiosRequest = syscall.TCGETS
