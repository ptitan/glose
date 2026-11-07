package server

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

var isDarwin = runtime.GOOS == "darwin"

// OpenBrowser tries to open url in the browser of the user. Failure is not
// fatal: the address is always printed.
func OpenBrowser(url string) error {
	cmd := browserCommand(url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// BrowserOpener returns the program that « glose open » uses to open the
// browser, and where it was found, without running it (glose doctor). The
// error lists the programs looked for.
func BrowserOpener() (name, path string, err error) {
	names := browserOpeners()
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return n, p, nil
		}
	}
	return "", "", fmt.Errorf("aucun programme pour ouvrir le navigateur (cherché : %s)", strings.Join(names, ", "))
}

// UnderWSL tells whether Glose runs under the Windows Subsystem for Linux.
func UnderWSL() bool { return isWSL() }
