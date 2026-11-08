package home

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
)

// tree records every entry under dir: its kind, permissions, date and bytes.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		v := fmt.Sprint(fi.Mode(), " ", fi.ModTime().UnixNano())
		// NTFS gives the date of a directory late, after its children are
		// created: on Windows, the entries and the files are what is compared.
		if d.IsDir() && runtime.GOOS == "windows" {
			v = "dossier"
		}
		if d.Type().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			v += " " + string(b)
		}
		out[path] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range after {
		if w, ok := before[k]; !ok {
			t.Errorf("créé : %s", k)
		} else if v != w {
			t.Errorf("changé : %s\n%q\n%q", k, w, v)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Errorf("supprimé : %s", k)
		}
	}
}

func entry(root string) Entry { return Entry{Root: root, Opened: "2026-11-01T10:00:00Z"} }

// A project never opened: nothing is written, .glose is not created.
func TestSummaryOfAFreshProjectWritesNothing(t *testing.T) {
	_, base := isolate(t)
	dir := makeProject(t, filepath.Join(base, "neuf"))
	os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"title":"Neuf","project":{"type":"cours","status":"fini"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Un {>>remarque<<} texte {++ajout++}.\n"), 0o644)
	before := tree(t, dir)
	s := Summarize(entry(dir), func(string) (*Server, bool) { return nil, false })
	sameTree(t, before, tree(t, dir))
	if _, err := os.Stat(filepath.Join(dir, project.StateDir)); err == nil {
		t.Fatal(".glose créé")
	}
	if s.State != StateOK || s.Title != "Neuf" || s.Meta.Type != "cours" || s.Meta.Status != "" || len(s.Warnings) != 1 {
		t.Fatalf("%+v", s)
	}
	if s.Hand != review.Author || s.Round != 1 || s.Annotations != 2 || s.Progress != nil || s.LastHandover != nil || s.Server != nil || s.Agent != nil {
		t.Fatalf("%+v", s)
	}
	if s.LastActivity != s.Opened {
		t.Fatalf("activité %q", s.LastActivity)
	}
}

// A project in full use: a turn handed over, an operation left pending by an
// interruption, a sign of life of the agent, a lock free to take. The summary
// reports all that, and changes nothing: no reconciliation, no sign of life,
// no lock taken.
func TestSummaryOfAProjectInUseWritesNothing(t *testing.T) {
	_, base := isolate(t)
	dir := makeProject(t, filepath.Join(base, "livre"))
	os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"files":["a.md","manque.md"]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Un {>>remarque<<} texte.\n"), 0o644)
	p, err := project.Load(dir)
	if err == nil {
		t.Fatal("un fichier nommé et absent doit être refusé au chargement, ou ce test est à revoir")
	}
	os.WriteFile(filepath.Join(dir, "manque.md"), []byte("Autre.\n"), 0o644)
	if p, err = project.Load(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := navette.New(p).Handoff(review.Author, "à vous"); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, project.StateDir)
	if err := navette.TouchJoined(state, "Sol", 2, "ABCD-EF", "gpt", navette.ModelDeclared); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(state, "journal.jsonl")
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, `{"seq":99,"time":"2026-11-02T10:00:00Z","type":"prepared","actor":"author","op":{"id":"pendante","kind":"comment","files":[{"path":"a.md","before":"x","after":"y"}]}}`+"\n")
	f.Close()
	os.WriteFile(filepath.Join(state, project.ProgressFile), []byte(`{"version":1,"sections":{"a.md#t1":{"status":"relue"},"a.md#t2":{"status":"en-cours"},"a.md#ancien":{"status":"relue"}},"headings":{"a.md":["t1","t2","t3","t4"],"dehors.md":["x"]}}`), 0o600)
	before := tree(t, dir)
	asked := ""
	s := Summarize(entry(dir), func(d string) (*Server, bool) {
		asked = d
		return &Server{URL: "http://127.0.0.1:1", PID: 7}, true
	})
	sameTree(t, before, tree(t, dir))
	if asked != state {
		t.Fatalf("serveur demandé pour %q", asked)
	}
	if s.State != StateOK || s.Hand != review.Agent || s.Round != 1 || s.Annotations != 1 {
		t.Fatalf("%+v", s)
	}
	if s.LastHandover == nil || s.LastHandover.Actor != review.Author {
		t.Fatalf("dernière remise %+v", s.LastHandover)
	}
	pending := false
	for _, pb := range s.Problems {
		pending = pending || strings.Contains(pb, "pendante")
	}
	if !pending {
		t.Fatalf("opération en suspens non signalée : %q", s.Problems)
	}
	if s.Agent == nil || s.Agent.Name != "Sol" || s.Agent.Model != "gpt" || !s.Agent.Listening {
		t.Fatalf("agent %+v", s.Agent)
	}
	if s.Server == nil || s.Server.PID != 7 {
		t.Fatalf("serveur %+v", s.Server)
	}
	// Two files, one displayed: the total is said to be partial.
	want := Progress{Read: 1, Total: 4, FilesSeen: 1, Files: 2}
	if s.Progress == nil || *s.Progress != want {
		t.Fatalf("avancement %+v", s.Progress)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "ABCD-EF") {
		t.Fatalf("code de liaison dans le résumé : %s", b)
	}
	if s.LastActivity != "2026-11-02T10:00:00Z" {
		t.Fatalf("activité %q", s.LastActivity)
	}
}

// What cannot be read is one line of state, never an error for the others.
func TestSummaryOfProjectsThatCannotBeRead(t *testing.T) {
	_, base := isolate(t)
	missing := filepath.Join(base, "disparu")
	noProfile := filepath.Join(base, "parent", "sans-profil")
	os.MkdirAll(noProfile, 0o755)
	// A parent project: it must not be taken for the one that lost its profile.
	os.WriteFile(filepath.Join(base, "parent", "glose.json"), []byte(`{"title":"Parent"}`), 0o644)
	os.WriteFile(filepath.Join(base, "parent", "p.md"), []byte("Texte.\n"), 0o644)
	badProfile := makeProject(t, filepath.Join(base, "profil"))
	os.WriteFile(filepath.Join(badProfile, "glose.json"), []byte(`{ abîmé`), 0o644)
	noMarkdown := filepath.Join(base, "vide")
	os.MkdirAll(noMarkdown, 0o755)
	os.WriteFile(filepath.Join(noMarkdown, "glose.json"), []byte(`{}`), 0o644)
	damaged := makeProject(t, filepath.Join(base, "journal"))
	os.MkdirAll(filepath.Join(damaged, project.StateDir), 0o700)
	os.WriteFile(filepath.Join(damaged, project.StateDir, "journal.jsonl"), []byte("pas du json\n"), 0o600)
	blocked := makeProject(t, filepath.Join(base, "journal-dossier"))
	os.MkdirAll(filepath.Join(blocked, project.StateDir, "journal.jsonl"), 0o700)
	badProgress := makeProject(t, filepath.Join(base, "progression"))
	os.MkdirAll(filepath.Join(badProgress, project.StateDir), 0o700)
	os.WriteFile(filepath.Join(badProgress, project.StateDir, project.ProgressFile), []byte("{"), 0o600)
	good := makeProject(t, filepath.Join(base, "bon"))

	want := map[string]string{missing: StateMissing, noProfile: StateNoProfile, badProfile: StateProfile, noMarkdown: StateProfile,
		damaged: StateJournal, blocked: StateJournal, badProgress: StateOK, good: StateOK}
	var entries []Entry
	for r := range want {
		entries = append(entries, entry(r))
	}
	before := tree(t, base)
	list := Summaries(entries, nil)
	sameTree(t, before, tree(t, base))
	if len(list) != len(want) {
		t.Fatalf("%d résumés", len(list))
	}
	for _, s := range list {
		if s.State != want[s.Root] {
			t.Errorf("%s : état %q (%s), attendu %q", s.Root, s.State, s.Message, want[s.Root])
		}
		if s.State != StateOK && s.Message == "" {
			t.Errorf("%s : aucune explication", s.Root)
		}
		if s.Title == "Parent" {
			t.Errorf("%s : pris pour le projet parent", s.Root)
		}
		if s.State == StateOK && s.Progress != nil {
			t.Errorf("%s : avancement %+v", s.Root, s.Progress)
		}
	}
}

func TestDefaultOrderAndTotals(t *testing.T) {
	list := []Summary{
		{Title: "illisible", State: StateMissing},
		{Title: "agent", Hand: review.Agent, Annotations: 3, State: StateOK, Meta: project.Meta{Deadline: "2026-11-09"}},
		{Title: "b sans date", Hand: review.Author, State: StateOK},
		{Title: "A sans date", Hand: review.Author, State: StateOK, Meta: project.Meta{Status: project.StatusReview}},
		{Title: "dans 7 jours", Hand: review.Author, State: StateOK, Annotations: 2, Meta: project.Meta{Deadline: "2026-11-15"}},
		{Title: "dans 8 jours", Hand: review.Author, State: StateOK, Meta: project.Meta{Deadline: "2026-11-16"}},
		{Title: "dépassée", Hand: review.Author, State: StateOK, Meta: project.Meta{Deadline: "2026-11-07"}},
		{Title: "terminé", Hand: review.Author, State: StateOK, Meta: project.Meta{Deadline: "2026-11-01", Status: project.StatusDone}},
	}
	Sort(list)
	var titles []string
	for _, s := range list {
		titles = append(titles, s.Title)
	}
	want := "terminé|dépassée|dans 7 jours|dans 8 jours|A sans date|b sans date|agent|illisible"
	if got := strings.Join(titles, "|"); got != want {
		t.Fatalf("ordre\n%s\n%s", got, want)
	}
	got := Compute(list, time.Date(2026, 11, 8, 23, 30, 0, 0, time.Local))
	wantT := Totals{Projects: 8, Annotations: 5, InReview: 1, AgentHand: 1, DueSoon: 2, Overdue: 1, Unreadable: 1}
	if got != wantT {
		t.Fatalf("%+v\n%+v", got, wantT)
	}
}
