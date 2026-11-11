package proc

import (
	"os"
	"testing"
)

// Under go test, the standard input is not a terminal (D-64: no approval
// without a person at the keyboard). GLOSE_TTY_PROBE prints the answer, for a
// check by hand under a terminal (script -qc).
func TestInteractiveWithoutATerminal(t *testing.T) {
	if os.Getenv("GLOSE_TTY_PROBE") != "" {
		t.Logf("interactif : %v", Interactive())
		return
	}
	if f, err := os.Open(os.DevNull); err == nil {
		prev := os.Stdin
		os.Stdin = f
		defer func() { os.Stdin = prev; f.Close() }()
	}
	if Interactive() {
		t.Fatal("/dev/null pris pour un terminal")
	}
}
