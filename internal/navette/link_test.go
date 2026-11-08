package navette

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestLinkCodes(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if _, err := UseLink(dir, "ABCD-EF", "Sol", "", now); !errors.Is(err, ErrNoLink) {
		t.Fatalf("sans code : %v", err)
	}
	first, err := NewLink(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-HJKMNP-Z2-9]{4}-[A-HJKMNP-Z2-9]{2}$`).MatchString(first.Code) {
		t.Fatalf("forme du code : %q", first.Code)
	}
	second, _ := NewLink(dir, now)
	for second.Code == first.Code {
		second, _ = NewLink(dir, now)
	}
	// The second code invalidates the first.
	if _, err := UseLink(dir, first.Code, "Sol", "", now); !errors.Is(err, ErrLinkWrong) {
		t.Fatalf("premier code : %v", err)
	}
	// Typed in small letters, without the dash: accepted, then taken by Sol.
	typed := strings.ToLower(strings.ReplaceAll(second.Code, "-", ""))
	if again, err := UseLink(dir, typed, "Sol", "", now.Add(time.Minute)); err != nil || again {
		t.Fatalf("bon code : %v %v", again, err)
	}
	if l := ReadLink(dir); l == nil || l.Used != "Sol" {
		t.Fatalf("%+v", l)
	}
	// P10.12: Sol may use it again, even after its deadline, to wait for
	// each turn; another agent may not.
	if again, err := UseLink(dir, second.Code, "Sol", "", now.Add(time.Hour)); err != nil || !again {
		t.Fatalf("réemploi : %v %v", again, err)
	}
	if _, err := UseLink(dir, second.Code, "Luna", "", now); !errors.Is(err, ErrLinkUsed) {
		t.Fatalf("autre agent : %v", err)
	}
	third, _ := NewLink(dir, now)
	if _, err := UseLink(dir, third.Code, "Sol", "", now.Add(LinkTTL)); !errors.Is(err, ErrLinkExpired) {
		t.Fatalf("code expiré : %v", err)
	}
}

// Codex review: several « glose join » at once with the same code, from
// different agents: one only takes it.
func TestLinkTakenOnceUnderConcurrency(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for round := 0; round < 20; round++ {
		l, err := NewLink(dir, now)
		if err != nil {
			t.Fatal(err)
		}
		const n = 8
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			go func(i int) {
				_, err := UseLink(dir, l.Code, fmt.Sprint("agent", i), "", now)
				errs <- err
			}(i)
		}
		ok := 0
		for i := 0; i < n; i++ {
			if <-errs == nil {
				ok++
			}
		}
		if ok != 1 {
			t.Fatalf("tour %d : %d réussites", round, ok)
		}
	}
}

// Codex audit: a session identified by the agent keeps the code; another
// session of the same name is refused.
func TestLinkSession(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l, _ := NewLink(dir, now)
	if _, err := UseLink(dir, l.Code, "Sol", "S=1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := UseLink(dir, l.Code, "Sol", "S=2", now); !errors.Is(err, ErrLinkUsed) {
		t.Fatalf("autre session : %v", err)
	}
	// Codex: a session without identifier does not take a code bound to one.
	if _, err := UseLink(dir, l.Code, "Sol", "", now); !errors.Is(err, ErrLinkUsed) {
		t.Fatalf("sans identifiant : %v", err)
	}
	if again, err := UseLink(dir, l.Code, "Sol", "S=1", now); err != nil || !again {
		t.Fatalf("même session : %v %v", again, err)
	}
	if !StillLinked(dir, l.Code, "Sol") {
		t.Fatal("liaison perdue")
	}
	NewLink(dir, now)
	if StillLinked(dir, l.Code, "Sol") {
		t.Fatal("un nouveau code n'annule pas l'ancien")
	}
}

func TestOwner(t *testing.T) {
	dir := t.TempDir()
	if ReadOwner(dir, 2) != nil {
		t.Fatal("propriétaire inventé")
	}
	undo, err := SetOwner(dir, Owner{Turn: 2, Kind: OwnerLaunch, Name: "Sol", ID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if o := ReadOwner(dir, 2); o == nil || o.Kind != OwnerLaunch || o.Name != "Sol" {
		t.Fatalf("%+v", o)
	}
	if ReadOwner(dir, 3) != nil {
		t.Fatal("autre tour")
	}
	undo()
	if ReadOwner(dir, 2) != nil {
		t.Fatal("annulation sans effet")
	}
}

// Codex counter-check: the turn is taken under the lock of the project,
// never beside a launch, and passes from an old binding to a new one.
func TestTakeTurn(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Texte.\n"})
	dir := w.n.P.State()
	now := time.Now()
	l, _ := NewLink(dir, now)
	UseLink(dir, l.Code, "Sol", "", now)
	if ok, _ := w.n.TakeTurn("Sol", l.Code); ok {
		t.Fatal("main prise pendant le tour de l'auteur")
	}
	w.n.Handoff("author", "")
	h, _ := w.n.History()
	// A turn reserved for a launch of the same name stays the launch's.
	SetOwner(dir, Owner{Turn: h.Turn(), Kind: OwnerLaunch, Name: "Sol", ID: "r"})
	if ok, _ := w.n.TakeTurn("Sol", l.Code); ok {
		t.Fatal("session liée homonyme à côté d'un lancement")
	}
	// Owned by an old binding (its code replaced since): passes to the new.
	SetOwner(dir, Owner{Turn: h.Turn(), Kind: OwnerLink, Name: "Sol", ID: "ANCIEN"})
	l2, _ := NewLink(dir, now)
	UseLink(dir, l2.Code, "Luna", "", now)
	if ok, err := w.n.TakeTurn("Luna", l2.Code); !ok || err != nil {
		t.Fatalf("transfert : %v", err)
	}
	if o := ReadOwner(dir, h.Turn()); o.Kind != OwnerLink || o.Name != "Luna" {
		t.Fatalf("%+v", o)
	}
	// The old code no longer takes anything.
	if ok, _ := w.n.TakeTurn("Sol", l.Code); ok {
		t.Fatal("ancien code")
	}
}

// Codex counter-check: the signs of a bound session carry its binding and
// model again once a launched agent has left the presence.
func TestTouchBoundRestores(t *testing.T) {
	dir := t.TempDir()
	TouchJoined(dir, "Sol", 1, "AB2CDE", "m/x", ModelDeclared)
	TouchLaunched(dir, "Luna", 2, 0, "", time.Now(), "")
	TouchBound(dir, "Sol", ActWait, 2, "AB2CDE", "m/x", ModelDeclared)
	if p := ReadPresence(dir); p.Name != "Sol" || p.Link != "AB2CDE" || p.Model != "m/x" {
		t.Fatalf("%+v", p)
	}
}

// Codex: a bound session whose part is closed does not take the same turn
// again; a launch is reserved under the lock, never over a bound session.
func TestTakeTurnAfterItsPartAndReserveLaunch(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Texte.\n"})
	dir := w.n.P.State()
	now := time.Now()
	l, _ := NewLink(dir, now)
	UseLink(dir, l.Code, "Claude Code", "", now)
	w.n.Handoff("author", "")
	h, _ := w.n.History()
	WritePasses(dir, &Passes{ID: "p", Turn: h.Turn(), Linked: "Claude Code", Passes: []Pass{{Alias: "Luna", State: PassRunning}}})
	if ok, _ := w.n.TakeTurn("Claude Code", l.Code); !ok {
		t.Fatal("part de la session liée")
	}
	ClosePass(dir, h.Turn(), "", "Claude Code", "fait")
	if ok, _ := w.n.TakeTurn("Claude Code", l.Code); ok {
		t.Fatal("même tour repris après sa part")
	}
	WritePasses(dir, nil)
	if ok, _ := w.n.TakeTurn("Claude Code", l.Code); !ok {
		t.Fatal("tour sans passages")
	}
	if o, err := w.n.ReserveLaunch("Sol"); err != nil || o == nil || o.Name != "Claude Code" {
		t.Fatalf("lancement par-dessus une session liée : %+v %v", o, err)
	}
}
