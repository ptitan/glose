//go:build unix

package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// detach makes the server independent of the terminal that launched it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// browserCommand returns the command that opens url in the browser of the
// user. Under WSL, it is the browser of Windows.
func browserCommand(url string) *exec.Cmd {
	name, _, err := BrowserOpener()
	if err != nil {
		name = "xdg-open"
	}
	switch name {
	case "cmd.exe":
		cmd := exec.Command("cmd.exe", "/c", "start", "", url)
		// cmd.exe refuses a Linux working directory: it runs in its own
		// (System32, wherever Windows is mounted).
		if p, err := exec.LookPath("cmd.exe"); err == nil {
			cmd.Dir = filepath.Dir(p)
		}
		return cmd
	default:
		return exec.Command(name, url)
	}
}

// browserOpeners lists the programs that can open the browser, in order of
// preference. Under WSL, cmd.exe is preferred: wslview is not always
// installed, and explorer.exe returns an error code even when it succeeds.
func browserOpeners() []string {
	switch {
	case isWSL():
		return []string{"cmd.exe", "wslview", "explorer.exe"}
	case isDarwin:
		return []string{"open"}
	default:
		return []string{"xdg-open"}
	}
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}
