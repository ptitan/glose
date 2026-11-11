package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

// State is the content of .glose/server.json: how to reach the running server.
type State struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	URL     string `json:"url"`
	Root    string `json:"root"`
	Started string `json:"started"`
	Source  string `json:"-"` // what the running server shows (a directory or a file)
	Build   string `json:"-"` // build of the executable that runs the server
}

// Base returns the address of the server, without the token.
func (st *State) Base() string { return "http://127.0.0.1:" + strconv.Itoa(st.Port) }

const (
	stateFile  = "server.json"
	serverLock = "server.lock"
	logFile    = "server.log"
)

// ErrRunning means that a server already runs for the project.
var ErrRunning = errors.New("un serveur Glose tourne déjà pour ce projet")

// ErrNotRunning means that no server answers for the project.
var ErrNotRunning = errors.New("aucun serveur Glose ne tourne pour ce projet")

// Serve runs the server of p in the current process until it is asked to stop
// (glose stop, or a signal). It is the hidden subcommand « glose serve ».
func Serve(p *project.Project) error {
	// A server being replaced (glose open after a new build) removes its
	// state file just before releasing the lock: wait a little for it.
	lock, err := store.TryLockNamed(p.State(), serverLock)
	for i := 0; i < 30 && errors.Is(err, store.ErrLocked); i++ {
		time.Sleep(100 * time.Millisecond)
		lock, err = store.TryLockNamed(p.State(), serverLock)
	}
	if errors.Is(err, store.ErrLocked) {
		return ErrRunning
	}
	if err != nil {
		return err
	}
	defer lock.Unlock()
	ln, port, err := ListenFor(p.Root)
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	s := New(p, port, token)
	st := State{PID: os.Getpid(), Port: port, Token: token, Root: p.Root, Started: time.Now().Format(time.RFC3339)}
	st.URL = st.Base() + "/#t=" + token
	data, _ := json.MarshalIndent(st, "", "  ")
	statePath := filepath.Join(p.State(), stateFile)
	// The state of a running server needs no durability: after a power
	// cut, the server is gone anyway.
	if err := store.WriteAtomic(statePath, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return err
	}
	// Removed at the end only if it is still ours: another server may have
	// written its own since (P6.10).
	defer removeIfOurs(statePath, st)
	// P6.10 : the state directory removed while the server runs (rm -rf
	// .glose) left a server that « glose stop » could not find. It is
	// watched: written again if it disappears, and the server stops if
	// another one now holds the project.
	watchDone, watchEnded := make(chan struct{}), make(chan struct{})
	defer func() { close(watchDone); <-watchEnded }()
	lost := make(chan struct{})
	go watchState(p.State(), statePath, st, data, lock, lost, watchDone, watchEnded)

	// An agent launched by a server stopped during its turn is stopped at
	// its deadline (P10.5).
	runDone := make(chan struct{})
	defer close(runDone)
	go s.runner.Watch(p.State(), 30*time.Second, runDone)
	// A turn with several recipients interrupted by a restart (P10.6).
	s.resumePasses()

	// A browser opens connections in advance: without a request yet, Shutdown
	// would wait 5 s for them. They are closed first: nothing is lost.
	var waiting sync.Map
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ConnState: func(c net.Conn, st http.ConnState) {
			if st == http.StateNew {
				waiting.Store(c, true)
			} else {
				waiting.Delete(c)
			}
		}}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	fmt.Fprintf(os.Stderr, "%s glose : serveur sur %s pour %s\n", time.Now().Format(time.RFC3339), st.Base(), p.Root)
	select {
	case err := <-done:
		return err
	case <-sig:
	case <-s.Stop:
	case <-lost:
		fmt.Fprintf(os.Stderr, "%s glose : un autre serveur tient ce projet ; arrêt\n", time.Now().Format(time.RFC3339))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waiting.Range(func(c, _ any) bool {
		c.(net.Conn).Close()
		return true
	})
	srv.Shutdown(ctx)
	fmt.Fprintf(os.Stderr, "%s glose : serveur arrêté\n", time.Now().Format(time.RFC3339))
	return nil
}

// watchEvery spaces the checks of the state file.
var watchEvery = 3 * time.Second

// ours reports whether the state file at path is the one of st.
func ours(path string, st State) (exists, mine bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	var cur State
	if json.Unmarshal(b, &cur) != nil {
		return true, false
	}
	return true, cur.PID == st.PID && cur.Token == st.Token
}

// removeIfOurs removes the state file if it is still the one of st.
func removeIfOurs(path string, st State) {
	if _, mine := ours(path, st); mine {
		store.Remove(path)
	}
}

// heldAgain reports whether the last lock taken again is still in place.
func heldAgain(held []*store.Lock) bool {
	return len(held) > 0 && held[len(held)-1].Held()
}

// watchState keeps the state file of a running server (P6.10): if it
// disappears with its directory, the server takes the lock again and writes
// it back; if the lock or the file now belongs to another server, lost is
// closed and the server stops.
func watchState(dir, path string, st State, data []byte, lock *store.Lock, lost, done, ended chan struct{}) {
	t := time.NewTicker(watchEvery)
	var held []*store.Lock // the locks taken again, released at the end
	defer func() {
		t.Stop()
		for _, l := range held {
			l.Unlock()
		}
		close(ended)
	}()
	for {
		select {
		case <-done:
			return
		case <-t.C:
		}
		exists, mine := ours(path, st)
		if mine {
			continue
		}
		if exists {
			close(lost) // another server wrote its state
			return
		}
		// The lock still in place (on Windows, an open file survives the
		// removal of its directory): only the state is written back.
		if !lock.Held() && !heldAgain(held) {
			l, err := store.TryLockNamed(dir, serverLock)
			if err != nil {
				close(lost) // another server holds the new lock
				return
			}
			held = append(held, l)
		}
		if err := store.WriteAtomic(path, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
			fmt.Fprintf(os.Stderr, "%s glose : état du serveur non réécrit : %v\n", time.Now().Format(time.RFC3339), err)
		}
	}
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Running returns the state of the server whose state directory is dir, if
// it answers, or ErrNotRunning.
func Running(dir string) (*State, error) {
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotRunning
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, ErrNotRunning
	}
	ping := map[string]any{}
	if err := call(&st, http.MethodGet, "/api/ping", &ping); err != nil {
		return nil, ErrNotRunning
	}
	st.Source, _ = ping["source"].(string)
	st.Build, _ = ping["build"].(string)
	return &st, nil
}

// Build identifies the executable that runs: its size and date. A server
// started by an older build of glose is restarted by « glose open ».
func Build() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	fi, err := os.Stat(self)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().UnixNano())
}

// buildAtStart is the build of this process, read once: the file may be
// replaced by a new build while the server runs.
var buildAtStart = Build()

func call(st *State, method, path string, out any) error {
	req, err := http.NewRequest(method, st.Base()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Glose-Token", st.Token)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("réponse %s", resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// ErrOtherSource means that the server of the directory shows something else
// (the whole directory rather than one file, or another file).
var ErrOtherSource = errors.New("un serveur Glose tourne déjà dans ce dossier pour d'autres fichiers ; « glose stop » d'abord")

// Start launches the server of p in the background (glose open) and waits
// until it answers. If one already runs for the same files, it returns its
// state.
func Start(p *project.Project) (st *State, started bool, err error) {
	reuse := func(st *State) (*State, bool, error) {
		if st.Source != p.Source {
			return nil, false, ErrOtherSource
		}
		return st, false, nil
	}
	if st, err := Running(p.State()); err == nil {
		if st.Build == Build() || st.Source != p.Source {
			return reuse(st)
		}
		// A server of an older build of glose: replace it.
		if err := StopRunning(p.State()); err != nil {
			return nil, false, fmt.Errorf("ancien serveur impossible à arrêter : %w", err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		return nil, false, err
	}
	logf, err := store.OpenLog(filepath.Join(p.State(), logFile))
	if err != nil {
		return nil, false, err
	}
	defer logf.Close()
	cmd := exec.Command(self, "serve", p.Source)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Dir = p.Root
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	var exitErr error
	childGone := false
	deadline := time.After(15 * time.Second)
	for {
		select {
		case err := <-exited:
			// The child may have lost the lock to another « glose open »:
			// keep waiting for the winner until the deadline.
			childGone, exitErr = true, err
			exited = nil
		case <-deadline:
			if childGone {
				return nil, false, fmt.Errorf("le serveur s'est arrêté au démarrage (%v) ; voir %s", exitErr, filepath.Join(p.State(), logFile))
			}
			return nil, false, fmt.Errorf("le serveur ne répond pas ; voir %s", filepath.Join(p.State(), logFile))
		case <-time.After(100 * time.Millisecond):
			if st, err := Running(p.State()); err == nil {
				if !childGone {
					cmd.Process.Release()
					return st, true, nil
				}
				return reuse(st)
			}
		}
	}
}

// StopRunning asks the server whose state directory is dir to stop, and
// waits until it is gone.
func StopRunning(dir string) error {
	st, err := Running(dir)
	if err != nil {
		return err
	}
	if err := call(st, http.MethodPost, "/api/stop", nil); err != nil {
		return err
	}
	// Longer than the 5 s that Serve leaves to Shutdown.
	for i := 0; i < 80; i++ {
		if _, err := os.Stat(filepath.Join(dir, stateFile)); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("le serveur (processus %d) ne s'est pas arrêté", st.PID)
}
