package navette

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/proc"
)

func TestCheckAgentName(t *testing.T) {
	for _, ok := range []string{"agent", "OpenCode", "Claude Code", "Sol (codex)", "é"} {
		if CheckAgentName(ok) != nil {
			t.Errorf("%q refusé", ok)
		}
	}
	long := ""
	for i := 0; i < 41; i++ {
		long += "a"
	}
	for _, bad := range []string{"", " x", "x ", "a\nb", "a\tb", long, "\xff"} {
		if CheckAgentName(bad) == nil {
			t.Errorf("%q accepté", bad)
		}
	}
}

func TestTouchAndReadPresence(t *testing.T) {
	dir := t.TempDir()
	if ReadPresence(dir) != nil {
		t.Fatal("présence sans fichier")
	}
	if err := Touch(dir, "OpenCode", ActWait, 0); err != nil {
		t.Fatal(err)
	}
	p := ReadPresence(dir)
	if p == nil || p.Name != "OpenCode" || p.Activity != ActWait || p.Since != p.Seen {
		t.Fatalf("%+v", p)
	}
	first := p.Since
	time.Sleep(2 * time.Millisecond)
	Touch(dir, "OpenCode", ActWrite, 2)
	p = ReadPresence(dir)
	if p.Since != first || p.Seen == first || p.Activity != ActWrite || p.Turn != 2 {
		t.Fatalf("même agent : %+v", p)
	}
	// Another name: the last writer wins, and his first sign is now.
	Touch(dir, "Codex", ActRead, 2)
	if p = ReadPresence(dir); p.Name != "Codex" || p.Since == first {
		t.Fatalf("autre agent : %+v", p)
	}
	if Touch(dir, "a\nb", ActRead, 0) == nil {
		t.Fatal("nom invalide écrit")
	}
}

func TestCorruptPresenceIsNobody(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []string{"", "{", `{"name":"x"}`, `{"name":"","seen":"2026-11-07T10:00:00Z"}`, `{"name":"x","seen":"hier"}`} {
		os.WriteFile(filepath.Join(dir, PresenceFile), []byte(c), 0o600)
		if p := ReadPresence(dir); p != nil {
			t.Errorf("%q : %+v", c, p)
		}
	}
	// A corrupt file is replaced by the next sign.
	if err := Touch(dir, "agent", ActWait, 0); err != nil || ReadPresence(dir) == nil {
		t.Fatal(err)
	}
}

func TestAgentStateFreshness(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339Nano) }
	handover := at(45 * time.Minute)
	cases := []struct {
		name, act, hand, since string
		seen                   time.Duration
		fresh, taken           bool
	}{
		{"attend", ActWait, "author", "", 5 * time.Second, true, false},
		{"plus de signe", ActWait, "author", "", 61 * time.Second, false, false},
		{"attend encore la remise", ActWait, "agent", handover, 5 * time.Second, true, false},
		{"a pris la main", ActRead, "agent", handover, 5 * time.Minute, true, true},
		{"travaille longtemps", ActWrite, "agent", handover, 29 * time.Minute, true, true},
		{"parti", ActWrite, "agent", handover, 31 * time.Minute, false, true},
		{"signe avant la remise", ActRead, "agent", handover, 50 * time.Minute, false, false},
		{"date dans le futur", ActWait, "author", "", -2 * time.Minute, false, false},
	}
	for _, c := range cases {
		a := agentState(&Presence{Name: "X", Activity: c.act, Seen: at(c.seen)}, c.hand, c.since, now)
		if a == nil || a.Fresh != c.fresh || a.Taken != c.taken {
			t.Errorf("%s : %+v", c.name, a)
		}
	}
	if agentState(nil, "author", "", now) != nil {
		t.Error("personne")
	}
}

func TestStateCarriesTheAgent(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Texte.\n"})
	st, _, _, err := w.n.State()
	if err != nil || st.Agent != nil {
		t.Fatalf("%v %+v", err, st.Agent)
	}
	before := w.read("a.md")
	if err := Touch(w.n.P.State(), "OpenCode", ActWait, 0); err != nil {
		t.Fatal(err)
	}
	st, _, _, _ = w.n.State()
	if st.Agent == nil || st.Agent.Name != "OpenCode" || !st.Agent.Fresh || st.Agent.Taken {
		t.Fatalf("%+v", st.Agent)
	}
	if w.read("a.md") != before {
		t.Fatal("manuscrit modifié")
	}
}

// Codex review: after « done », an agent no longer listens, even with a
// fresh sign; « glose join » binds with its code.
func TestListening(t *testing.T) {
	now := time.Now()
	seen := now.Add(-5 * time.Second).Format(time.RFC3339Nano)
	for act, want := range map[string]bool{ActWait: true, ActJoined: true, ActDone: false, ActRead: false, ActWrite: false} {
		a := agentState(&Presence{Name: "X", Activity: act, Seen: seen}, "author", "", now)
		if a.Listening != want {
			t.Errorf("%s : %+v", act, a)
		}
	}
	dir := t.TempDir()
	TouchJoined(dir, "X", 0, "ab2c-de", "", "")
	Touch(dir, "X", ActWait, 0)
	if p := ReadPresence(dir); p.Link != "AB2CDE" {
		t.Fatalf("code gardé : %+v", p)
	}
	Touch(dir, "Y", ActWait, 0)
	if p := ReadPresence(dir); p.Link != "" {
		t.Fatalf("code d'un autre agent : %+v", p)
	}
}

// P10.8: the sign of another name does not erase the presence of the agent
// launched by Glose while its process runs.
func TestLaunchedPresenceIsKept(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid() // a process that runs: this test
	if err := TouchLaunched(dir, "opus", 2, pid, proc.Identity(pid), time.Now().Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	if err := Touch(dir, "verif", ActRead, 2); !errors.Is(err, ErrLaunchedAgent) {
		t.Fatalf("%v", err)
	}
	if p := ReadPresence(dir); p.Name != "opus" || !p.Launched || p.PID != pid {
		t.Fatalf("présence effacée : %+v", p)
	}
	// Its own signs keep its process.
	Touch(dir, "opus", ActWrite, 2)
	if p := ReadPresence(dir); p.Activity != ActWrite || p.PID != pid || !p.Launched {
		t.Fatalf("%+v", p)
	}
	// Codex audit: past its deadline, the same process (known identity) is
	// kept until it is gone, so that the watch can stop it; without an
	// identity, the deadline bounds it (its number may be reused).
	TouchLaunched(dir, "opus", 2, pid, proc.Identity(pid), time.Now().Add(-time.Minute), "")
	if proc.Identity(pid) != "" && LaunchedAgent(dir) == nil {
		t.Fatal("agent vivant oublié après son échéance")
	}
	TouchLaunched(dir, "opus", 2, pid, "", time.Now().Add(-time.Minute), "")
	if err := Touch(dir, "verif", ActRead, 2); err != nil {
		t.Fatal(err)
	}
	if id := proc.Identity(pid); id != "" {
		TouchLaunched(dir, "opus", 2, pid, "autre", time.Now().Add(time.Minute), "")
		if LaunchedAgent(dir) != nil {
			t.Fatal("identité différente prise pour l'agent")
		}
	}
}

func TestProgressDoesNotCrossAgentsOrTurns(t *testing.T) {
	dir := t.TempDir()
	if err := Progress(dir, "Sol", 2, "Lecture du texte."); err != nil {
		t.Fatal(err)
	}
	if err := Touch(dir, "Sol", ActRead, 2); err != nil {
		t.Fatal(err)
	}
	if ReadPresence(dir).Progress != "Lecture du texte." {
		t.Fatal("phrase perdue")
	}
	if err := Touch(dir, "Sol", ActRead, 4); err != nil {
		t.Fatal(err)
	}
	if ReadPresence(dir).Progress != "" {
		t.Fatal("ancien tour")
	}
	if err := Progress(dir, "Sol", 4, "Révision."); err != nil {
		t.Fatal(err)
	}
	if err := Touch(dir, "Autre", ActRead, 4); err != nil {
		t.Fatal(err)
	}
	if ReadPresence(dir).Progress != "" {
		t.Fatal("autre agent")
	}
}
