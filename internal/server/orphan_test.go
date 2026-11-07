package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/project"
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("délai écoulé : %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readState(path string) (State, bool) {
	var st State
	b, err := os.ReadFile(path)
	return st, err == nil && json.Unmarshal(b, &st) == nil
}

func serveInBackground(t *testing.T) (p *project.Project, statePath string, ended chan error) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Texte.\n"), 0o644)
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := watchEvery
	t.Cleanup(func() { watchEvery = old }) // after the test: its server has stopped
	watchEvery = 50 * time.Millisecond
	ended = make(chan error, 1)
	go func() { ended <- Serve(p) }()
	statePath = filepath.Join(p.State(), stateFile)
	waitFor(t, "état écrit", func() bool { _, ok := readState(statePath); return ok })
	return p, statePath, ended
}

// P6.10 : le dossier .glose effacé pendant que le serveur tourne : son
// état est réécrit, et glose stop le trouve et l'arrête.
func TestServerSurvivesTheRemovalOfItsStateDirectory(t *testing.T) {
	p, statePath, ended := serveInBackground(t)
	before, _ := readState(statePath)
	os.RemoveAll(p.State())
	waitFor(t, "état réécrit", func() bool {
		st, ok := readState(statePath)
		return ok && st.Token == before.Token && st.PID == before.PID
	})
	if err := StopRunning(p.State()); err != nil {
		t.Fatalf("glose stop : %v", err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("serveur non arrêté")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("état laissé après l'arrêt")
	}
}

// P6.10 : un autre serveur a écrit son état : celui-ci s'arrête, et ne
// retire pas l'état de l'autre en partant.
func TestServerYieldsToAnotherOne(t *testing.T) {
	_, statePath, ended := serveInBackground(t)
	other, _ := json.Marshal(State{PID: 1, Port: 1, Token: "autre"})
	os.WriteFile(statePath, other, 0o600)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("serveur toujours en marche")
	}
	if st, ok := readState(statePath); !ok || st.Token != "autre" {
		t.Fatal("état de l'autre serveur retiré")
	}
}
