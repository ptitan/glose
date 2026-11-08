//go:build unix

package proc

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Alive reports whether the process pid runs.
func Alive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// Identity returns the start time of the process pid, where the system
// gives it (Linux: /proc/PID/stat, field 22); "" elsewhere.
func Identity(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')') // the name may hold spaces
	if i < 0 {
		return ""
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return ""
	}
	return f[19] // starttime: field 22 of the line, the 20th after the name
}
