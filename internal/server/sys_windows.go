//go:build windows

package server

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach makes the server independent of the console that launched it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess, HideWindow: true}
}

// browserCommand returns the command that opens url in the default browser.
func browserCommand(url string) *exec.Cmd {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
}

// browserOpeners lists the programs that can open the browser.
func browserOpeners() []string { return []string{"rundll32"} }

// IsWSL is false under Windows.
func isWSL() bool { return false }
