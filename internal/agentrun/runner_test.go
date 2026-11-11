package agentrun

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Audit A3: if the presence of the launched agent cannot be recorded, the
// launch fails and the agent is stopped at once; another launch is possible.
func TestStartFailsWhenThePresenceIsNotRecorded(t *testing.T) {
	fakeAgent(t, `echo $$ > "$PWD/pid"; sleep 30`)
	dir := t.TempDir()
	state := filepath.Join(dir, ".glose")
	prev := touchLaunched
	touchLaunched = func(string, string, int, int, string, time.Time, string) error { return errors.New("disque plein") }
	defer func() { touchLaunched = prev }()
	var r Runner
	start := time.Now()
	_, err := r.Start(Launch{Spec: Spec{Agent: "codex", Dir: dir, Name: "Sol", Prompt: "x"}, State: state, Turn: 2, Max: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "disque plein") {
		t.Fatalf("lancement accepté : %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("agent non arrêté")
	}
	if st, ok := r.Status(state); ok && st.Running {
		t.Fatalf("toujours en cours : %+v", st)
	}
	log, _ := os.ReadFile(filepath.Join(state, LogName(2)))
	if !strings.Contains(string(log), "présence non enregistrée") {
		t.Fatalf("journal de l'agent : %q", log)
	}
	touchLaunched = prev
	if _, err := r.Start(Launch{Spec: Spec{Agent: "codex", Dir: dir, Name: "Sol", Prompt: "x"}, State: state, Turn: 2, Max: time.Minute}); err != nil {
		t.Fatalf("nouveau lancement refusé : %v", err)
	}
	r.Stop(state)
}

// Audit A10: stop and close may run at the same time (the author stops the
// agent as it ends): no race (run with -race).
func TestTreeStopAndCloseTogether(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep absent")
	}
	cmd := exec.Command("sleep", "30")
	ownTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	tr, err := newTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { tr.close(); close(done) }()
	tr.stop()
	<-done
}

// Audit A10: once closed, stop sends nothing, since the number of the group
// or of the process may then belong to another program. A process that ends
// by itself with its own code proves that it was not stopped.
func TestTreeStopAfterCloseSendsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh absent")
	}
	cmd := exec.Command("sh", "-c", "sleep 0.3; exit 7")
	ownTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	tr, err := newTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	tr.close()
	if err := tr.stop(); err != nil {
		t.Fatal(err)
	}
	var ee *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatalf("arrêté après la fermeture : %v", err)
	}
}
