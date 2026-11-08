package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/agentrun"
	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/review"
)

// launchWorld is a project with an agent configured.
func launchWorld(t *testing.T, mode string) (*Server, string) {
	t.Helper()
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("GLOSE_AGENT", "")
	useFakeAgent(t, mode)
	if _, err := agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex", Model: "gpt-essai"}}}); err != nil {
		t.Fatal(err)
	}
	s, dir := newServer(t, map[string]string{"a.md": "Un texte à relire.\n"})
	t.Cleanup(func() { s.runner.Stop(s.P.State()); s.runner.Wait() })
	return s, dir
}

func handoffAndLaunch(t *testing.T, s *Server, launch any) map[string]any {
	t.Helper()
	body := map[string]any{"note": "À toi."}
	if launch != nil {
		body["launch"] = launch
	}
	w := do(t, s, "POST", "/api/handoff", body)
	if w.Code != 200 {
		t.Fatalf("remise : %d %s", w.Code, w.Body)
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func hand(t *testing.T, s *Server) string {
	h, err := s.N.History()
	if err != nil {
		t.Fatal(err)
	}
	return h.Hand()
}

// P10.5: at the handover, Glose launches the agent, which answers and hands
// over; its output is kept in .glose/agent/tour-1.log.
func TestLaunchAtTheHandover(t *testing.T) {
	s, dir := launchWorld(t, "answer")
	out := handoffAndLaunch(t, s, nil)
	if run, _ := out["run"].(map[string]any); run == nil || run["alias"] != "Sol" || out["launchError"] != nil {
		t.Fatalf("%v", out)
	}
	s.runner.Wait()
	if hand(t, s) != review.Author {
		t.Fatal("la main n'est pas revenue")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !strings.Contains(string(b), "Réponse du faux agent.") {
		t.Fatalf("%s", b)
	}
	log, _ := os.ReadFile(filepath.Join(dir, ".glose", "agent", "tour-1.log"))
	for _, want := range []string{"— Glose : Sol (codex, gpt-essai) lancé le", "faux agent answer, GLOSE_AGENT=Sol", "consigne reçue",
		"-s workspace-write", "-m gpt-essai", "— Glose : fin le"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("%q absent du journal :\n%s", want, log)
		}
	}
	var lg struct {
		Run  agentrun.Status
		Text string
	}
	json.Unmarshal(do(t, s, "GET", "/api/agent/log", nil).Body.Bytes(), &lg)
	if lg.Run.Running || lg.Run.ExitCode != 0 || !strings.Contains(lg.Text, "consigne reçue") {
		t.Fatalf("%+v", lg)
	}
}

// An agent that fails stays recorded, the hand at the agent: the interface
// says that it stopped without handing over; it may be launched again.
func TestLaunchedAgentThatFails(t *testing.T) {
	s, _ := launchWorld(t, "crash")
	handoffAndLaunch(t, s, true)
	s.runner.Wait()
	st, ok := s.runner.Status(s.P.State())
	if !ok || st.Running || st.ExitCode != 2 || hand(t, s) != review.Agent {
		t.Fatalf("%+v", st)
	}
	t.Setenv(fakeAgentEnv, "answer")
	if w := do(t, s, "POST", "/api/agent/launch", map[string]any{}); w.Code != 200 {
		t.Fatalf("relance : %d %s", w.Code, w.Body)
	}
	s.runner.Wait()
	if hand(t, s) != review.Author {
		t.Fatal("la relance n'a pas rendu la main")
	}
	if w := do(t, s, "POST", "/api/agent/launch", map[string]any{}); w.Code != 409 {
		t.Fatalf("lancement pendant le tour de l'auteur : %d", w.Code)
	}
}

// One agent at a time; the author stops it.
func TestStopTheLaunchedAgent(t *testing.T) {
	s, _ := launchWorld(t, "sleep")
	handoffAndLaunch(t, s, true)
	if w := do(t, s, "POST", "/api/agent/launch", map[string]any{}); w.Code != 409 || !strings.Contains(w.Body.String(), "busy") {
		t.Fatalf("second agent : %d %s", w.Code, w.Body)
	}
	start := time.Now()
	w := do(t, s, "POST", "/api/agent/stop", map[string]any{})
	var out struct{ Run agentrun.Status }
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.Run.Running || !out.Run.Stopped || time.Since(start) > 5*time.Second {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do(t, s, "POST", "/api/agent/stop", map[string]any{}); w.Code != 409 {
		t.Fatalf("rien à arrêter : %d", w.Code)
	}
}

// The maximum duration stops the agent.
func TestLaunchedAgentTimesOut(t *testing.T) {
	s, _ := launchWorld(t, "sleep")
	if _, err := s.N.Handoff(review.Author, ""); err != nil {
		t.Fatal(err)
	}
	h, _ := s.N.History()
	_, err := s.runner.Start(agentrun.Launch{Spec: agentrun.Spec{Agent: "codex", Dir: s.P.Root, Name: "Sol", Prompt: "x"},
		State: s.P.State(), Turn: h.Turn(), Max: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	s.runner.Wait()
	st, _ := s.runner.Status(s.P.State())
	if !st.TimedOut || st.Running {
		t.Fatalf("%+v", st)
	}
}

// An agent that listens (glose wait, a bound session) takes the turn: no
// launch unless asked; and nothing is launched without a configuration.
func TestNoLaunchWhenAnAgentListens(t *testing.T) {
	s, _ := launchWorld(t, "answer")
	navette.Touch(s.P.State(), "OpenCode", navette.ActWait, 0)
	out := handoffAndLaunch(t, s, nil)
	if out["run"] != nil {
		t.Fatalf("lancé alors qu'un agent écoute : %v", out)
	}
	s2, _ := launchWorld(t, "answer")
	agents.Save(agents.Config{})
	out = handoffAndLaunch(t, s2, true)
	if out["run"] != nil || !strings.Contains(out["launchError"].(string), "aucun agent configuré") {
		t.Fatalf("%v", out)
	}
	if hand(t, s2) != review.Agent {
		t.Fatal("la remise doit réussir sans lancement")
	}
}

// A server restarted while its agent runs finds it again by its process
// number, and can stop it.
func TestOrphanAgent(t *testing.T) {
	s, _ := launchWorld(t, "sleep")
	self, _ := os.Executable()
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), fakeAgentEnv+"=sleep")
	agentrun.Group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	navette.TouchLaunched(s.P.State(), "Sol", 2, cmd.Process.Pid, agentrun.ProcessIdentity(cmd.Process.Pid), time.Now().Add(time.Minute), "")
	st, ok := s.runner.Status(s.P.State())
	if !ok || !st.Orphan || !st.Running || st.PID != cmd.Process.Pid {
		t.Fatalf("%+v", st)
	}
	if _, err := s.runner.Start(agentrun.Launch{Spec: agentrun.Spec{Agent: "codex", Dir: s.P.Root, Name: "Sol"}, State: s.P.State()}); err != agentrun.ErrBusy {
		t.Fatalf("second agent à côté d'un orphelin : %v", err)
	}
	if w := do(t, s, "POST", "/api/agent/stop", map[string]any{}); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("orphelin non arrêté")
	}
}

// Codex review: a number reused by another process, or a run past its
// deadline, is not an orphan; past its deadline, the watch stops it.
func TestOrphanIdentityAndDeadline(t *testing.T) {
	s, _ := launchWorld(t, "sleep")
	self, _ := os.Executable()
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), fakeAgentEnv+"=sleep")
	agentrun.Group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() { cmd.Process.Kill() })
	pid := cmd.Process.Pid
	id := agentrun.ProcessIdentity(pid)
	if id != "" {
		navette.TouchLaunched(s.P.State(), "Sol", 2, pid, "autre-processus", time.Now().Add(time.Minute), "")
		if st, _ := s.runner.Status(s.P.State()); st.Orphan {
			t.Fatalf("numéro réutilisé pris pour l'agent : %+v", st)
		}
		if w := do(t, s, "POST", "/api/agent/stop", map[string]any{}); w.Code != 409 {
			t.Fatalf("processus étranger arrêté : %d", w.Code)
		}
	}
	navette.TouchLaunched(s.P.State(), "Sol", 2, pid, id, time.Now().Add(-2*time.Minute), "")
	if st, _ := s.runner.Status(s.P.State()); st.Orphan {
		t.Fatalf("orphelin au-delà de son échéance : %+v", st)
	}
	if id == "" {
		return // no identity on this system: the watch never stops it
	}
	stop := make(chan struct{})
	go s.runner.Watch(s.P.State(), 20*time.Millisecond, stop)
	defer close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("orphelin au-delà de son échéance non arrêté")
	}
	for end := time.Now().Add(5 * time.Second); navette.ReadPresence(s.P.State()).Launched; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("présence non effacée : %+v", navette.ReadPresence(s.P.State()))
		}
	}
}

// Codex review: what the agent leaves running is stopped when it ends.
func TestDescendantsStopWithTheAgent(t *testing.T) {
	s, dir := launchWorld(t, "spawn")
	handoffAndLaunch(t, s, true)
	s.runner.Wait()
	b, err := os.ReadFile(filepath.Join(dir, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(5 * time.Second)
	for agentrun.ProcessIdentity(pid) != "" && processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("descendant %d encore en vie", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Codex review: the log of the agent is never written through a link.
func TestLogNotWrittenThroughALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques")
	}
	s, _ := launchWorld(t, "answer")
	outside := t.TempDir()
	os.MkdirAll(s.P.State(), 0o700)
	if err := os.Symlink(outside, filepath.Join(s.P.State(), "agent")); err != nil {
		t.Fatal(err)
	}
	out := handoffAndLaunch(t, s, true)
	if out["run"] != nil || !strings.Contains(fmt.Sprint(out["launchError"]), "lien") {
		t.Fatalf("%v", out)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("écrit hors du projet")
	}
}

// Codex counter-check: a turn taken by a bound session is never launched
// beside it, even under the same name; a turn reserved for a launch that
// failed may be launched again from the menu, with another agent; a
// handover refused writes no owner.
func TestOwnerOfTheTurn(t *testing.T) {
	s, _ := launchWorld(t, "sleep")
	state := s.P.State()
	navette.TouchJoined(state, "OpenCode", 0, "ABCD-EF", "", "")
	handoffAndLaunch(t, s, false)
	h, _ := s.N.History()
	navette.SetOwner(state, navette.Owner{Turn: h.Turn(), Kind: navette.OwnerLink, Name: "Sol", ID: "ABCDEF"})
	if w := do(t, s, "POST", "/api/agent/launch", map[string]any{}); w.Code != 409 || !strings.Contains(w.Body.String(), "session liée") {
		t.Fatalf("lancement à côté d'une session liée : %d %s", w.Code, w.Body)
	}
	navette.SetOwner(state, navette.Owner{Turn: h.Turn(), Kind: navette.OwnerLaunch, Name: "Luna", ID: "r"})
	if w := do(t, s, "POST", "/api/agent/launch", map[string]any{}); w.Code != 200 {
		t.Fatalf("relance après un lancement raté : %d %s", w.Code, w.Body)
	}
	if o := navette.ReadOwner(state, h.Turn()); o.Kind != navette.OwnerLaunch || o.Name != "Sol" {
		t.Fatalf("%+v", o)
	}
	before, _ := os.ReadFile(filepath.Join(state, "owner.json"))
	if w := do(t, s, "POST", "/api/handoff", map[string]any{"note": "x", "launch": true}); w.Code != 409 {
		t.Fatalf("remise pendant le tour de l'agent : %d", w.Code)
	}
	if after, _ := os.ReadFile(filepath.Join(state, "owner.json")); string(after) != string(before) {
		t.Fatal("une remise refusée a écrit le propriétaire")
	}
}
