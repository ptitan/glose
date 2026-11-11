package agentrun

import (
	"context"
	"errors"
	"fmt"
	"github.com/ptitan/glose/internal/proc"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/store"
)

// A Runner launches the agent at the handover (P10.5, D-46): one at a time,
// for a maximum duration, its output in a log file of the project; it can
// be stopped. It lives in the server; an agent launched by a server that has
// stopped since is found again by its process number (an orphan).
type Runner struct {
	mu   sync.Mutex
	cur  *run // running, or nil
	last *run // the last one, ended
}

type run struct {
	Status
	cmd    *exec.Cmd
	tree   *tree
	cancel context.CancelFunc
	done   chan struct{}
}

// Status is what the interface shows of the run.
type Status struct {
	Running bool   `json:"running"`
	Orphan  bool   `json:"orphan,omitempty"` // launched by a server stopped since
	Alias   string `json:"alias"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"`
	PID     int    `json:"pid"`
	Turn    int    `json:"turn"`
	Log     string `json:"log"` // path of the log, relative to the state directory
	Started string `json:"started"`
	Ended   string `json:"ended,omitempty"`
	// ExitCode of the agent once ended; TimedOut: stopped at the maximum
	// duration; Stopped: by the author.
	ExitCode int    `json:"exitCode"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Stopped  bool   `json:"stopped,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ErrBusy means that an agent already runs.
var ErrBusy = errors.New("un agent lancé par Glose travaille déjà")

// Launch describes a run.
type Launch struct {
	Spec
	State string        // state directory of the project (.glose)
	Turn  int           // part of the journal handed to the agent
	Max   time.Duration // maximum duration
	// Ended is called when the agent stops, whatever the reason.
	Ended func(Status)
}

// LogName is the log of the agent for the round of turn, relative to the
// state directory.
func LogName(turn int) string {
	return fmt.Sprintf("agent/tour-%d.log", navette.Round(turn))
}

// Start launches the agent. The log gets a header, then everything the
// agent writes.
func (r *Runner) Start(l Launch) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur != nil {
		return r.cur.Status, ErrBusy
	}
	if p := orphan(l.State); p != nil {
		return Status{Running: true, Orphan: true, PID: p.PID, Alias: p.Name}, ErrBusy
	}
	if l.Max <= 0 {
		l.Max = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), l.Max)
	cmd, err := Command(ctx, l.Spec)
	if err != nil {
		cancel()
		return Status{}, err
	}
	// The whole tree of processes is stopped: at the deadline, by the
	// author, and once the agent has ended (what it left running).
	ownTree(cmd)
	var trp atomic.Pointer[tree] // set once started, read by the cancellation
	cmd.Cancel = func() error {
		if tr := trp.Load(); tr != nil {
			return tr.stop()
		}
		return stopGroup(cmd)
	}
	cmd.WaitDelay = 5 * time.Second
	logName := LogName(l.Turn)
	logf, err := openLog(l.State, logName)
	if err != nil {
		cancel()
		return Status{}, err
	}
	now := time.Now()
	fmt.Fprintf(logf, "\n— Glose : %s (%s%s) lancé le %s, tour %d, %s au plus —\n", l.Name, l.Agent, modelSuffix(l.Model),
		now.Format("02/01/2006 à 15:04:05"), navette.Round(l.Turn), l.Max)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		cancel()
		return Status{}, err
	}
	tr, err := newTree(cmd)
	trp.Store(tr)
	if err != nil {
		cancel()
		cmd.Wait()
		tr.close()
		fmt.Fprintf(logf, "\n— Glose : arrêté aussitôt (%v) —\n", err)
		logf.Close()
		return Status{}, err
	}
	pid := cmd.Process.Pid
	// Without its presence (process, identity, deadline), a server started
	// again would not find this agent, and could launch another one: the
	// launch fails, and the agent is stopped (audit A3).
	if err := touchLaunched(l.State, l.Name, l.Turn, pid, proc.Identity(pid), now.Add(l.Max), l.Model); err != nil {
		tr.stop()
		cancel()
		cmd.Wait()
		tr.close()
		fmt.Fprintf(logf, "\n— Glose : arrêté aussitôt, présence non enregistrée (%v) —\n", err)
		logf.Close()
		return Status{}, fmt.Errorf("lancement annulé, présence de l'agent non enregistrée : %w", err)
	}
	ru := &run{cmd: cmd, tree: tr, cancel: cancel, done: make(chan struct{}), Status: Status{Running: true, Alias: l.Name, Agent: l.Agent, Model: l.Model,
		PID: pid, Turn: l.Turn, Log: logName, Started: now.UTC().Format(time.RFC3339Nano)}}
	r.cur = ru
	go func() {
		err := cmd.Wait()
		tr.stop() // what the agent left running
		tr.close()
		end := time.Now()
		r.mu.Lock()
		ru.Running = false
		ru.Ended = end.UTC().Format(time.RFC3339Nano)
		var ee *exec.ExitError
		switch {
		case ctx.Err() == context.DeadlineExceeded && !ru.Stopped:
			ru.TimedOut, ru.ExitCode = true, -1
			ru.Error = fmt.Sprintf("arrêté au bout de %s", l.Max)
		case ru.Stopped:
			ru.ExitCode = -1
		case errors.As(err, &ee):
			ru.ExitCode = ee.ExitCode()
		case err != nil:
			ru.ExitCode, ru.Error = -1, err.Error()
		}
		st := ru.Status
		r.mu.Unlock()
		what := fmt.Sprintf("code %d", st.ExitCode)
		switch {
		case st.TimedOut:
			what = st.Error
		case st.Stopped:
			what = "arrêté par l'auteur"
		}
		fmt.Fprintf(logf, "\n— Glose : fin le %s (%s), après %s —\n", end.Format("02/01/2006 à 15:04:05"), what, end.Sub(now).Round(time.Second))
		logf.Close()
		cancel()
		navette.ClearLaunched(l.State, pid)
		// Released once everything is written: Wait and a new Start see a
		// finished run; Ended may start the next one (P10.6).
		r.mu.Lock()
		r.last, r.cur = ru, nil
		r.mu.Unlock()
		close(ru.done)
		if l.Ended != nil {
			l.Ended(st)
		}
	}()
	return ru.Status, nil
}

func modelSuffix(m string) string {
	if m == "" {
		return ""
	}
	return ", " + m
}

// Status returns the run in progress, else the last one, else an orphan
// found by the presence of the agent; ok is false if there is none.
func (r *Runner) Status(state string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur != nil {
		return r.cur.Status, true
	}
	if p := orphan(state); p != nil {
		return Status{Running: true, Orphan: true, PID: p.PID, Alias: p.Name, Turn: p.Turn, Log: LogName(p.Turn)}, true
	}
	if r.last != nil {
		return r.last.Status, true
	}
	return Status{}, false
}

// Stop stops the agent in progress (its process group), or an orphan.
func (r *Runner) Stop(state string) error {
	r.mu.Lock()
	ru := r.cur
	if ru != nil {
		ru.Stopped = true
	}
	r.mu.Unlock()
	if ru != nil {
		ru.tree.stop()
		ru.cancel()
		select {
		case <-ru.done:
		case <-time.After(10 * time.Second):
			return errors.New("l'agent ne s'est pas arrêté")
		}
		return nil
	}
	if p := orphan(state); p != nil {
		return stopPID(p.PID)
	}
	return errors.New("aucun agent lancé par Glose ne travaille")
}

// Wait waits for the end of the run in progress (tests).
func (r *Runner) Wait() {
	r.mu.Lock()
	ru := r.cur
	r.mu.Unlock()
	if ru != nil {
		<-ru.done
	}
}

// orphan returns the presence of an agent launched by Glose whose process
// still runs, when this server did not launch it: the same process (its
// identity, where the system gives one), before its deadline. A number
// reused by another process, or a run past its deadline, is not an orphan.
func orphan(state string) *navette.Presence {
	p := navette.ReadPresence(state)
	if p == nil || !p.Launched || p.PID <= 0 || p.PID == os.Getpid() || !proc.Alive(p.PID) {
		return nil
	}
	if p.Identity != "" && proc.Identity(p.PID) != p.Identity {
		return nil
	}
	if d, err := time.Parse(time.RFC3339Nano, p.Deadline); err != nil || time.Now().After(d.Add(time.Minute)) {
		return nil
	}
	return p
}

// pastDeadline returns the number of an orphan agent past its deadline,
// still running (the same process): to stop.
func pastDeadline(state string) int {
	p := navette.ReadPresence(state)
	if p == nil || !p.Launched || p.PID <= 0 || p.PID == os.Getpid() || p.Identity == "" || !proc.Alive(p.PID) || proc.Identity(p.PID) != p.Identity {
		return 0
	}
	if d, err := time.Parse(time.RFC3339Nano, p.Deadline); err == nil && time.Now().After(d) {
		return p.PID
	}
	return 0
}

// Watch stops, every period until done is closed, an orphan agent of the
// state directory past its maximum duration (a server stopped during the
// turn of the agent it launched).
func (r *Runner) Watch(state string, period time.Duration, done <-chan struct{}) {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		r.mu.Lock()
		busy := r.cur != nil
		r.mu.Unlock()
		if pid := pastDeadline(state); pid != 0 && !busy {
			stopPID(pid)
			navette.ClearLaunched(state, pid)
		}
		select {
		case <-done:
			return
		case <-t.C:
		}
	}
}

// openLog opens the log of the agent, refusing a link (the log is never
// written outside the project).
func openLog(state, name string) (*os.File, error) {
	if err := noLink(state, name); err != nil {
		return nil, err
	}
	return store.OpenLog(filepathJoin(state, name))
}

// noLink checks that neither the directory of name (under state) nor the
// file itself is a symbolic link.
func noLink(state, name string) error {
	p := state
	for _, part := range strings.Split(name, "/") {
		p = filepath.Join(p, part)
		fi, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s est un lien : le journal de l'agent n'est pas écrit à travers un lien", p)
		}
	}
	return nil
}

// Tail returns the end of the log name of the state directory, max bytes
// at most, cut at the start of a line.
func Tail(state, name string, max int64) (string, error) {
	if name == "" || strings.Contains(name, "..") {
		return "", errors.New("journal de l'agent inconnu")
	}
	if err := noLink(state, name); err != nil {
		return "", err
	}
	f, err := os.Open(filepathJoin(state, name))
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	from := int64(0)
	if fi.Size() > max {
		from = fi.Size() - max
	}
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	if from > 0 {
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return strings.ToValidUTF8(string(b), "�"), nil
}

// filepathJoin joins the state directory and a slash-separated name.
func filepathJoin(state, name string) string {
	return filepath.Join(state, filepath.FromSlash(name))
}

// touchLaunched records the presence of a launched agent; tests replace it
// to simulate a failure.
var touchLaunched = navette.TouchLaunched
