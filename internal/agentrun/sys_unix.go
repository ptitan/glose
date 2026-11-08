//go:build unix

package agentrun

import (
	"errors"
	"os/exec"
	"syscall"
)

// group puts the agent in a process group of its own, so that stopping it
// stops what it launched (MCP servers, commands).
func group(cmd *exec.Cmd) {
	ownGroup(cmd)
	cmd.Cancel = func() error { return stopGroup(cmd) }
}

// ownGroup gives cmd a process group of its own.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// tree is the processes of a started agent: its group.
type tree struct{ pgid int }

// newTree follows the processes of cmd, once started.
func newTree(cmd *exec.Cmd) *tree { return &tree{pgid: cmd.Process.Pid} }

// stop stops every process of the group. A group with no process left is
// not an error. A process group number is not reused while the group has
// members.
func (t *tree) stop() error {
	if err := syscall.Kill(-t.pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (t *tree) close() {}

// stopGroup stops the process group of cmd.
func stopGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return (&tree{pgid: cmd.Process.Pid}).stop()
}

// stopPID stops the process group of an orphan agent: its group bears its
// number (see group). Never the process alone: a number reused by another
// process is not a group of Glose.
func stopPID(pid int) error {
	return (&tree{pgid: pid}).stop()
}
