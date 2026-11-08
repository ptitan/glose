//go:build windows

package proc

import (
	"strconv"
	"syscall"
)

const (
	processQueryLimited = 0x1000
	stillActive         = uint32(259)
)

// Alive reports whether the process pid runs.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// Identity returns the creation time of the process pid.
func Identity(pid int) string {
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	var c, e, k, u syscall.Filetime
	if err := syscall.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return ""
	}
	return strconv.FormatInt(c.Nanoseconds(), 10)
}
