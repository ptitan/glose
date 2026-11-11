//go:build windows

package agentrun

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createSuspended       = 0x00000004
	processSuspendResume  = 0x0800
)

// ownGroup gives cmd a process group of its own.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// ownTree prepares an agent for newTree: started suspended, it is put in its
// job before it runs, so that no child escapes it (audit A10); newTree lets
// it run.
func ownTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | createSuspended}
}

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject  = kernel32.NewProc("CreateJobObjectW")
	procAssignProcess    = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJob     = kernel32.NewProc("TerminateJobObject")
	ntdll                = syscall.NewLazyDLL("ntdll.dll")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
	processSetQuota      = 0x0100
	processTerminate     = 0x0001
	jobTerminateExitCode = uintptr(1)
)

// tree is the processes of a started agent: a job object that holds the
// agent and what it launches after it was assigned (a child started in
// between escapes it).
// Its methods are safe for concurrent use: the author may stop the agent
// while it ends (audit A10).
type tree struct {
	mu     sync.Mutex
	job    syscall.Handle
	pid    int
	proc   *os.Process
	closed bool
}

// newTree puts cmd, started suspended (ownTree), in a new job, then lets it
// run; without a job, only the agent is stopped. An agent that cannot be let
// run is stopped, and the error returned.
func newTree(cmd *exec.Cmd) (*tree, error) {
	t := &tree{pid: cmd.Process.Pid, proc: cmd.Process}
	p, err := syscall.OpenProcess(uint32(processSetQuota|processTerminate|processSuspendResume), false, uint32(t.pid))
	if err != nil {
		cmd.Process.Kill()
		return t, err
	}
	defer syscall.CloseHandle(p)
	if h, _, _ := procCreateJobObject.Call(0, 0); h != 0 {
		if r, _, _ := procAssignProcess.Call(h, uintptr(p)); r != 0 {
			t.job = syscall.Handle(h)
		} else {
			syscall.CloseHandle(syscall.Handle(h))
		}
	}
	if st, _, _ := procNtResumeProcess.Call(uintptr(p)); st != 0 {
		t.stop()
		return t, fmt.Errorf("reprise du processus de l'agent impossible (NTSTATUS 0x%x)", st)
	}
	return t, nil
}

// stop stops the job, or the agent alone, through its handle (never its
// number, which may be reused once it has ended). Once closed, nothing.
func (t *tree) stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	if t.job != 0 {
		if r, _, err := procTerminateJob.Call(uintptr(t.job), jobTerminateExitCode); r == 0 {
			return err
		}
		return nil
	}
	if err := t.proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// close releases the job; stop does nothing after it.
func (t *tree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		syscall.CloseHandle(t.job)
		t.job = 0
	}
	t.closed = true
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
