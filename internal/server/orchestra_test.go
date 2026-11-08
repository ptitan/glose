package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/review"
)

// passWorld is a project with two short names and two comments of the
// author: one without recipient (for Sol, the default), one for Luna.
func passWorld(t *testing.T, mode string) (*Server, string) {
	t.Helper()
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("GLOSE_AGENT", "")
	useFakeAgent(t, mode)
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}, {Name: "Luna", Agent: "opencode", Model: "x/y"}}})
	s, dir := newServer(t, map[string]string{"a.md": "Premier{>>Pour le défaut.<<}{>>glose:v1 id=aaaa by=author<<} et second{>>Pour Luna.<<}{>>glose:v1 id=bbbb by=author to=Luna<<}.\n"})
	t.Cleanup(func() {
		s.orchMu.Lock()
		if s.orch != nil {
			s.orch.stop()
		}
		s.orchMu.Unlock()
		s.runner.Stop(s.P.State())
		s.runner.Wait()
	})
	return s, dir
}

func waitHand(t *testing.T, s *Server, want string) {
	t.Helper()
	for end := time.Now().Add(15 * time.Second); hand(t, s) != want; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("la main n'est pas à %s ; passages : %+v", want, navette.ReadPasses(s.P.State()))
		}
	}
}

// P10.6: one pass per recipient, one after the other, then Glose hands
// over; each message signed via= its agent.
func TestPassesOneAfterTheOther(t *testing.T) {
	s, dir := passWorld(t, "pass")
	cfg := mustConfig(t)
	gs := recipients(s.cur(), cfg)
	if len(gs) != 2 || gs[0].key != "" || gs[1].key != "Luna" || gs[0].ids[0] != "aaaa" || gs[1].ids[0] != "bbbb" {
		t.Fatalf("%+v", gs)
	}
	sol, _ := cfg.Alias("Sol")
	if ps := plan(gs, cfg, sol, ""); len(ps) != 2 || ps[0].Alias != "Sol" || ps[0].IDs[0] != "aaaa" || ps[1].Alias != "Luna" {
		t.Fatalf("%+v", ps)
	}
	handoffAndLaunch(t, s, nil)
	if ps := navette.ReadPasses(s.P.State()); ps == nil || len(ps.Passes) != 2 {
		t.Fatalf("plan : %+v", ps)
	}
	waitHand(t, s, review.Author)
	b, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	got := string(b)
	// Each answered in the thread addressed to it.
	at, luna := strings.Index(got, "Réponse de Sol."), strings.Index(got, "Réponse de Luna.")
	if at < 0 || luna < 0 || at > strings.Index(got, " et second") || luna < strings.Index(got, "Pour Luna.") || !strings.Contains(got, "via=Sol") || !strings.Contains(got, "via=Luna") {
		h, _ := s.N.History()
		log, _ := os.ReadFile(filepath.Join(dir, ".glose", "agent", "tour-1.log"))
		t.Fatalf("%s\nnote : %q\n%s", got, h.Last().TurnEv.Note, log)
	}
	h, _ := s.N.History()
	if note := h.Last().TurnEv.Note; !strings.Contains(note, "Sol : fait par Sol") || !strings.Contains(note, "Luna : fait par Luna") {
		t.Fatalf("note : %q", note)
	}
	for end := time.Now().Add(5 * time.Second); navette.ReadPasses(s.P.State()) != nil; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("passages non effacés")
		}
	}
	log, _ := os.ReadFile(filepath.Join(dir, ".glose", "agent", "tour-1.log"))
	if !strings.Contains(string(log), "Sol (codex) lancé") || !strings.Contains(string(log), "Luna (opencode, x/y) lancé") {
		t.Fatalf("%s", log)
	}
}

func mustConfig(t *testing.T) agents.Config {
	c, err := agents.Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A bound session answers the annotations without recipient; Glose
// launches Luna for hers, and hands over once both are done.
func TestPassBesideABoundSession(t *testing.T) {
	s, _ := passWorld(t, "pass")
	navette.TouchJoined(s.P.State(), "Claude Code", 0, "ABCD-EF", "", "")
	handoffAndLaunch(t, s, nil)
	ps := navette.ReadPasses(s.P.State())
	if ps == nil || len(ps.Passes) != 1 || ps.Passes[0].Alias != "Luna" || ps.Linked != "Claude Code" {
		t.Fatalf("plan : %+v", ps)
	}
	// Luna ends her pass; Glose waits for the bound session.
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if p := navette.ReadPasses(s.P.State()); p != nil && p.Passes[0].State == navette.PassDone {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("%+v", navette.ReadPasses(s.P.State()))
		}
	}
	time.Sleep(300 * time.Millisecond)
	if hand(t, s) != review.Agent {
		t.Fatal("main rendue avant la session liée")
	}
	h, _ := s.N.History()
	if err := navette.ClosePass(s.P.State(), h.Turn(), "", "Claude Code", "j'ai répondu"); err != nil {
		t.Fatal(err)
	}
	waitHand(t, s, review.Author)
	h, _ = s.N.History()
	if note := h.Last().TurnEv.Note; !strings.Contains(note, "Claude Code : j'ai répondu") || !strings.Contains(note, "Luna : fait par Luna") {
		t.Fatalf("note : %q", note)
	}
}

// The author stops the passes: the next ones are not launched, the hand
// stays at the agent.
func TestStopThePasses(t *testing.T) {
	s, _ := passWorld(t, "sleep")
	handoffAndLaunch(t, s, nil)
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if st, ok := s.runner.Status(s.P.State()); ok && st.Running {
			break
		}
		if time.Now().After(end) {
			t.Fatal("premier passage non lancé")
		}
	}
	if w := do(t, s, "POST", "/api/agent/stop", map[string]any{}); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for end := time.Now().Add(5 * time.Second); navette.ReadPasses(s.P.State()) != nil; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("passages non abandonnés")
		}
	}
	time.Sleep(200 * time.Millisecond)
	if st, _ := s.runner.Status(s.P.State()); st.Running || st.Alias != "Sol" {
		t.Fatalf("second passage lancé : %+v", st)
	}
	if hand(t, s) != review.Agent {
		t.Fatal("main rendue")
	}
}

// Codex review: a reply of the author to a proposal of the agent, addressed
// to another agent, gives that agent a pass; an annotation addressed
// explicitly to the default agent is not taken by a bound session.
func TestPlanFollowsTheRepliesAndTheExplicitRecipients(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}, {Name: "Luna", Agent: "opencode"}}})
	s, _ := newServer(t, map[string]string{"a.md": "Un {++ajout++}{>>glose:v1 id=pppp by=agent via=Sol<<}{>>Non, @Luna ?<<}{>>glose:v1 id=rrrr re=pppp by=author to=Luna<<} et" +
		" deux{>>Pour Sol.<<}{>>glose:v1 id=ssss by=author to=Sol<<} et trois{>>Sans destinataire.<<}{>>glose:v1 id=tttt by=author<<}.\n"})
	cfg := mustConfig(t)
	sol, _ := cfg.Alias("Sol")
	ps := plan(recipients(s.cur(), cfg), cfg, sol, "")
	if len(ps) != 2 || ps[0].Alias != "Sol" || strings.Join(ps[0].IDs, ",") != "ssss,tttt" && strings.Join(ps[0].IDs, ",") != "tttt,ssss" ||
		ps[1].Alias != "Luna" || strings.Join(ps[1].IDs, ",") != "pppp" {
		t.Fatalf("%+v", ps)
	}
	// A bound session « Claude Code » takes only the one without recipient.
	ps = plan(recipients(s.cur(), cfg), cfg, sol, "Claude Code")
	if len(ps) != 2 || ps[0].Alias != "Sol" || strings.Join(ps[0].IDs, ",") != "ssss" {
		t.Fatalf("session liée : %+v", ps)
	}
}

// Codex review: a handover refused (the hand is at the agent) neither
// writes a plan nor removes the plan of the turn in progress; an old plan
// is never removed by another orchestration.
func TestRefusedHandoverKeepsThePlan(t *testing.T) {
	s, _ := passWorld(t, "sleep")
	handoffAndLaunch(t, s, nil)
	first := navette.ReadPasses(s.P.State())
	if first == nil {
		t.Fatal("pas de plan")
	}
	if w := do(t, s, "POST", "/api/handoff", map[string]any{"note": "encore"}); w.Code != 409 {
		t.Fatalf("seconde remise : %d", w.Code)
	}
	if p := navette.ReadPasses(s.P.State()); p == nil || p.ID != first.ID {
		t.Fatalf("plan remplacé ou effacé : %+v", p)
	}
	navette.RemovePlan(s.P.State(), "autre")
	if navette.ReadPasses(s.P.State()) == nil {
		t.Fatal("plan effacé par un autre identifiant")
	}
}

// Codex review: an agent that runs glose done as soon as it starts finds its
// pass open.
func TestQuickDone(t *testing.T) {
	s, _ := passWorld(t, "quickdone")
	handoffAndLaunch(t, s, nil)
	waitHand(t, s, review.Author)
	h, _ := s.N.History()
	if note := h.Last().TurnEv.Note; note != "Sol : aussitôt\nLuna : aussitôt" {
		t.Fatalf("note : %q", note)
	}
}

// Codex review: a server restarted during the passes takes them up: the
// pass that was running is closed as interrupted, the next one launched,
// then the hand handed back.
func TestPassesResumedAfterARestart(t *testing.T) {
	s, _ := passWorld(t, "pass")
	if _, err := s.N.Handoff(review.Author, ""); err != nil {
		t.Fatal(err)
	}
	h, _ := s.N.History()
	navette.WritePasses(s.P.State(), &navette.Passes{ID: "avant", Turn: h.Turn(), Passes: []navette.Pass{
		{Alias: "Sol", Agent: "codex", State: navette.PassRunning, IDs: []string{"aaaa"}},
		{Alias: "Luna", Agent: "opencode", Model: "x/y", State: navette.PassWaiting, IDs: []string{"bbbb"}}}})
	s.resumePasses()
	waitHand(t, s, review.Author)
	h, _ = s.N.History()
	if note := h.Last().TurnEv.Note; !strings.Contains(note, "Sol : interrompu par un redémarrage du serveur") || !strings.Contains(note, "Luna : fait par Luna") {
		t.Fatalf("note : %q", note)
	}
}
