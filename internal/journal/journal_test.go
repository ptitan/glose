package journal

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/store"
)

type fixture struct {
	t     *testing.T
	root  string
	j     *Journal
	files map[string]string
}

func newFixture(t *testing.T, files map[string]string) *fixture {
	root := t.TempDir()
	for name, content := range files {
		os.WriteFile(filepath.Join(root, name), []byte(content), 0o644)
	}
	return &fixture{t, root, Open(filepath.Join(root, ".glose")), files}
}

func (f *fixture) abs(p string) string { return filepath.Join(f.root, p) }

func (f *fixture) read(p string) string {
	b, _ := os.ReadFile(f.abs(p))
	return string(b)
}

func (f *fixture) change(p, after string) Change {
	b, _ := os.ReadFile(f.abs(p))
	return Change{Path: p, Abs: f.abs(p), Before: b, After: []byte(after)}
}

func TestAppendAndRead(t *testing.T) {
	f := newFixture(t, nil)
	if ev, pb, err := f.j.Read(); len(ev) != 0 || len(pb) != 0 || err != nil {
		t.Fatal("journal absent")
	}
	f.j.Append(Event{Type: Turn, Actor: "author", TurnEv: &TurnInfo{Number: 1}})
	f.j.Append(Event{Type: Turn, Actor: "agent", TurnEv: &TurnInfo{Number: 2, Note: "fait"}})
	ev, pb, err := f.j.Read()
	if err != nil || len(pb) != 0 || len(ev) != 2 || ev[0].Seq != 1 || ev[1].Seq != 2 || ev[1].TurnEv.Note != "fait" {
		t.Fatalf("%+v %+v %v", ev, pb, err)
	}
	info, _ := os.Stat(f.j.Path())
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %v", info.Mode().Perm())
	}
}

func TestTruncatedLastLineIsReportedAndRepaired(t *testing.T) {
	f := newFixture(t, nil)
	f.j.Append(Event{Type: Turn, Actor: "author", TurnEv: &TurnInfo{Number: 1}})
	fh, _ := os.OpenFile(f.j.Path(), os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(`{"seq":2,"type":"prep`)
	fh.Close()
	ev, pb, _ := f.j.Read()
	if len(ev) != 1 || len(pb) != 1 || !strings.Contains(pb[0].Message, "tronquée") {
		t.Fatalf("%+v %+v", ev, pb)
	}
	out, err := f.j.Append(Event{Type: Turn, Actor: "agent", TurnEv: &TurnInfo{Number: 2}})
	if err != nil || out[0].Seq != 3 {
		t.Fatalf("%+v %v", out, err)
	}
	ev, pb, _ = f.j.Read()
	// The broken line stays, reported as unreadable; a repair event names it.
	if len(ev) != 3 || ev[1].Type != Repair || len(pb) != 1 || pb[0].Message != "ligne illisible" {
		t.Fatalf("%+v %+v", ev, pb)
	}
}

func TestCommit(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un", "b.md": "deux"})
	evs, err := f.j.Commit("author", Op{Kind: "comment"}, []Change{f.change("a.md", "un{>>x<<}"), f.change("b.md", "deux{>>y<<}")})
	if err != nil || len(evs) != 2 || evs[1].Type != Committed {
		t.Fatalf("%+v %v", evs, err)
	}
	if f.read("a.md") != "un{>>x<<}" || f.read("b.md") != "deux{>>y<<}" {
		t.Fatal("fichiers non écrits")
	}
	ev, _, _ := f.j.Read()
	eff := Effective(ev)
	if len(eff) != 1 || eff[0].Kind != "comment" || eff[0].Files[0].After != store.Hash([]byte("un{>>x<<}")) || eff[0].Actor != "author" {
		t.Fatalf("%+v", eff)
	}
}

func TestCommitRollsBackOnConflict(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un", "b.md": "deux"})
	ca, cb := f.change("a.md", "UN"), f.change("b.md", "DEUX")
	os.WriteFile(f.abs("b.md"), []byte("changé ailleurs"), 0o644)
	_, err := f.j.Commit("agent", Op{Kind: "track"}, []Change{ca, cb})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("erreur %v", err)
	}
	if f.read("a.md") != "un" || f.read("b.md") != "changé ailleurs" {
		t.Fatalf("a.md = %q", f.read("a.md"))
	}
	ev, _, _ := f.j.Read()
	if len(ev) != 2 || ev[1].Type != Aborted || len(Effective(ev)) != 0 || len(Pending(ev)) != 0 {
		t.Fatalf("%+v", ev)
	}
}

// Interruption at each step of the protocol: on restart, Reconcile leaves a
// consistent state.
func TestInterruptionAtEachStep(t *testing.T) {
	cases := []struct {
		step string
		want string // committed, aborted, anomaly
		a, b string
	}{
		{"prepared", Aborted, "un", "deux"},
		{"written:a.md", "anomalie", "UN", "deux"},
		{"written:b.md", Committed, "UN", "DEUX"},
		{"before-commit", Committed, "UN", "DEUX"},
	}
	for _, c := range cases {
		f := newFixture(t, map[string]string{"a.md": "un", "b.md": "deux"})
		failpoint = func(step string) error {
			if step == c.step {
				return errors.New("interruption simulée")
			}
			return nil
		}
		_, err := f.j.Commit("author", Op{Kind: "decide"}, []Change{f.change("a.md", "UN"), f.change("b.md", "DEUX")})
		failpoint = nil
		if err == nil {
			t.Fatalf("%s : pas d'interruption", c.step)
		}
		if f.read("a.md") != c.a || f.read("b.md") != c.b {
			t.Fatalf("%s : fichiers %q %q", c.step, f.read("a.md"), f.read("b.md"))
		}
		ev, _, _ := f.j.Read()
		if len(Pending(ev)) != 1 {
			t.Fatalf("%s : %d opérations en suspens", c.step, len(Pending(ev)))
		}
		written, anomalies, err := f.j.Reconcile(f.abs)
		if err != nil || len(written) != 1 {
			t.Fatalf("%s : %+v %v", c.step, written, err)
		}
		switch c.want {
		case "anomalie":
			if written[0].Type != Aborted || len(anomalies) != 1 {
				t.Fatalf("%s : %+v %v", c.step, written, anomalies)
			}
		default:
			if written[0].Type != c.want || len(anomalies) != 0 {
				t.Fatalf("%s : %+v %v", c.step, written, anomalies)
			}
		}
		ev, _, _ = f.j.Read()
		if len(Pending(ev)) != 0 {
			t.Fatalf("%s : encore en suspens", c.step)
		}
		// Reconcile is idempotent.
		if w, _, _ := f.j.Reconcile(f.abs); len(w) != 0 {
			t.Fatalf("%s : seconde reprise %+v", c.step, w)
		}
	}
}

// An interruption in the middle of the "prepared" line: the line is
// reported, the files are untouched, nothing is pending.
func TestInterruptionInsideAnAppend(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un"})
	f.j.Append(Event{Type: Turn, Actor: "author", TurnEv: &TurnInfo{Number: 1}})
	fh, _ := os.OpenFile(f.j.Path(), os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(`{"seq":2,"type":"prepared","op":{"id":"x","kind":"comm`)
	fh.Close()
	ev, pb, _ := f.j.Read()
	if len(Pending(ev)) != 0 || len(pb) != 1 || f.read("a.md") != "un" {
		t.Fatalf("%+v %+v", ev, pb)
	}
	if _, err := f.j.Commit("author", Op{Kind: "comment"}, []Change{f.change("a.md", "un{>>x<<}")}); err != nil {
		t.Fatal(err)
	}
	ev, pb, _ = f.j.Read()
	if len(Effective(ev)) != 1 || len(pb) != 1 {
		t.Fatalf("%+v %+v", ev, pb)
	}
}

func TestSnapshots(t *testing.T) {
	dir := t.TempDir()
	h, err := SaveSnapshot(dir, []byte("texte\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadSnapshot(dir, h)
	if err != nil || string(b) != "texte\r\n" {
		t.Fatalf("%q %v", b, err)
	}
	os.WriteFile(filepath.Join(dir, SnapshotDir, h), []byte("altéré"), 0o600)
	if _, err := LoadSnapshot(dir, h); err == nil {
		t.Fatal("copie altérée acceptée")
	}
}

// Revue Codex, défaut 4 : un événement complet sans sa fin de ligne compte
// dès la lecture, et l'ajout suivant ne le « réveille » pas.
func TestWholeLastLineWithoutLineEnding(t *testing.T) {
	f := newFixture(t, nil)
	f.j.Append(Event{Type: Turn, Actor: "author", TurnEv: &TurnInfo{Number: 1}})
	fh, _ := os.OpenFile(f.j.Path(), os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(`{"seq":2,"time":"x","type":"turn","actor":"agent","turn":{"number":2,"files":null}}`)
	fh.Close()
	ev, pb, _ := f.j.Read()
	if len(ev) != 2 || len(pb) != 0 || len(Turns(ev)) != 2 {
		t.Fatalf("%+v %+v", ev, pb)
	}
	out, err := f.j.Append(Event{Type: Turn, Actor: "author", TurnEv: &TurnInfo{Number: 3}})
	if err != nil || out[0].Seq != 3 {
		t.Fatalf("%+v %v", out, err)
	}
	ev, pb, _ = f.j.Read()
	if len(ev) != 3 || len(pb) != 0 || ev[2].Seq != 3 {
		t.Fatalf("%+v %+v", ev, pb)
	}
}

// Revue Codex, défaut 5 : une opération interrompue à moitié reste signalée
// après la reprise.
func TestPartialOperationStaysReported(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un", "b.md": "deux"})
	failpoint = func(step string) error {
		if step == "written:a.md" {
			return errors.New("interruption simulée")
		}
		return nil
	}
	f.j.Commit("author", Op{Kind: "identify"}, []Change{f.change("a.md", "UN"), f.change("b.md", "DEUX")})
	failpoint = nil
	f.j.Reconcile(f.abs)
	ev, _, _ := f.j.Read()
	p := Partials(ev)
	if len(p) != 1 || len(p[0].Files) != 2 || !strings.Contains(p[0].Reason, "à vérifier") {
		t.Fatalf("%+v", p)
	}
}

// Revue générale : le renommage réussit, puis la synchronisation du dossier
// échoue. Le fichier qui contenait déjà les nouveaux octets est restauré : le
// journal dit « abandonnée » et les fichiers le confirment.
func TestCommitRestoresAFileWrittenBeforeAFailedSync(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un"})
	// La restauration non plus ne se synchronise pas : elle compte quand même.
	restore := store.SetAfterRename(func() error { return errors.New("synchronisation impossible") })
	_, err := f.j.Commit("author", Op{Kind: "comment"}, []Change{f.change("a.md", "un{>>x<<}")})
	restore()
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if f.read("a.md") != "un" {
		t.Fatalf("fichier non restauré : %q", f.read("a.md"))
	}
	ev, _, _ := f.j.Read()
	if len(Effective(ev)) != 0 || ev[len(ev)-1].Type != Aborted || ev[len(ev)-1].Partial {
		t.Fatalf("%+v", ev)
	}
}

// Contre-vérification : un autre programme a déjà écrit exactement le
// nouveau contenu. L'écriture de Glose est refusée (conflit) et ce fichier
// n'est jamais « restauré » : la modification extérieure reste.
func TestCommitNeverRestoresAFileChangedByAnotherProgram(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un"})
	c := f.change("a.md", "un{>>x<<}")
	os.WriteFile(f.abs("a.md"), []byte("un{>>x<<}"), 0o644)
	if _, err := f.j.Commit("author", Op{Kind: "comment"}, []Change{c}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("erreur : %v", err)
	}
	if f.read("a.md") != "un{>>x<<}" {
		t.Fatalf("modification extérieure écrasée : %q", f.read("a.md"))
	}
}

// Revue de Codex (P5.4) : une restauration dont le dossier ne peut être
// synchronisé, même après un nouvel essai, n'est pas tenue pour durable :
// l'abandon est signalé comme partiel, avec le fichier, pour vérification.
func TestRestorationNotOnDiskIsSignalled(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "un"})
	dir := filepath.Dir(f.abs("a.md"))
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	// La panne commence avec l'écriture du texte (le journal est déjà là)
	// et dure : la restauration et son nouvel essai échouent aussi.
	failing := false
	restoreSync := store.SetSyncFailure(func(p string) error {
		if failing && p == dir {
			return errors.New("disque indisponible")
		}
		return nil
	})
	restoreRename := store.SetAfterRename(func() error {
		if !failing {
			failing = true
			return errors.New("disque indisponible")
		}
		return nil
	})
	_, err := f.j.Commit("author", Op{Kind: "comment"}, []Change{f.change("a.md", "un{>>x<<}")})
	restoreRename()
	restoreSync()
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if f.read("a.md") != "un" {
		t.Fatalf("fichier non restauré : %q", f.read("a.md"))
	}
	ev, _, _ := f.j.Read()
	last := ev[len(ev)-1]
	if len(Effective(ev)) != 0 || last.Type != Aborted || !last.Partial || len(last.Files) != 1 || last.Files[0] != "a.md" ||
		!strings.Contains(last.Reason, "non synchronisés") {
		t.Fatalf("%+v", last)
	}
}
