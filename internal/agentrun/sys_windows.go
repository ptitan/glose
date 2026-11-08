//go:build windows

package agentrun

import (
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

// group puts the agent in a process group of its own; once started, it is
// put in a job (newTree), which stops what it launched with it.
func group(cmd *exec.Cmd) {
	ownGroup(cmd)
	cmd.Cancel = func() error { return stopGroup(cmd) }
}

// ownGroup gives cmd a process group of its own.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject  = kernel32.NewProc("CreateJobObjectW")
	procAssignProcess    = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJob     = kernel32.NewProc("TerminateJobObject")
	processSetQuota      = 0x0100
	processTerminate     = 0x0001
	jobTerminateExitCode = uintptr(1)
)

// tree is the processes of a started agent: a job object that holds the
// agent and what it launches after it was assigned (a child started in
// between escapes it).
type tree struct {
	job syscall.Handle
	pid int
}

// newTree puts the started cmd in a new job; without one, only the agent
// is stopped.
func newTree(cmd *exec.Cmd) *tree {
	t := &tree{pid: cmd.Process.Pid}
	h, _, _ := procCreateJobObject.Call(0, 0)
	if h == 0 {
		return t
	}
	p, err := syscall.OpenProcess(uint32(processSetQuota|processTerminate), false, uint32(t.pid))
	if err != nil {
		syscall.CloseHandle(syscall.Handle(h))
		return t
	}
	defer syscall.CloseHandle(p)
	if r, _, _ := procAssignProcess.Call(h, uintptr(p)); r == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return t
	}
	t.job = syscall.Handle(h)
	return t
}

// stop stops the job, or the agent alone.
func (t *tree) stop() error {
	if t.job != 0 {
		if r, _, err := procTerminateJob.Call(uintptr(t.job), jobTerminateExitCode); r == 0 {
			return err
		}
		return nil
	}
	return stopPID(t.pid)
}

func (t *tree) close() {
	if t.job != 0 {
		syscall.CloseHandle(t.job)
		t.job = 0
	}
}

// stopGroup stops the agent (the job is stopped by the runner).
func stopGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// stopPID stops the process pid (an orphan: the job of the server that
// launched it is gone with it; what the agent launched may survive).
func stopPID(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
