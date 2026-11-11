//go:build unix

package agentrun

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
)

// ownGroup gives cmd a process group of its own.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// ownTree prepares an agent for newTree: its own group.
func ownTree(cmd *exec.Cmd) { ownGroup(cmd) }

// tree is the processes of a started agent: its group. Its methods are safe
// for concurrent use (audit A10).
type tree struct {
	mu     sync.Mutex
	pgid   int
	closed bool
}

// newTree follows the processes of cmd, once started.
func newTree(cmd *exec.Cmd) (*tree, error) { return &tree{pgid: cmd.Process.Pid}, nil }

// stop stops every process of the group. A group with no process left is
// not an error. A process group number is not reused while the group has
// members; once the tree is closed (the agent ended, what it left stopped),
// it may be: nothing is sent then.
func (t *tree) stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	return killGroup(t.pgid)
}

// close marks the end of the agent and of its group; stop does nothing after.
func (t *tree) close() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
}

func killGroup(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// stopGroup stops the process group of cmd.
func stopGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return killGroup(cmd.Process.Pid)
}

// stopPID stops the process group of an orphan agent: its group bears its
// number (see group). Never the process alone: a number reused by another
// process is not a group of Glose.
func stopPID(pid int) error {
	return killGroup(pid)
}
