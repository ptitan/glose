package navette

import (
	"bytes"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/review"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/journal"
)

func TestTaskHandoverFidelityAndJournal(t *testing.T) {
	original := "\xef\xbb\xbf# Titre\r\n\nDu texte é 😀.\r\n"
	w := newWorld(t, map[string]string{"a.md": original, "b.md": "Autre\r\n"})
	task := &journal.TaskInfo{ID: "rewrite", Name: "Réécrire", Agent: agents.Alias{Name: "Sol", Agent: "codex"}, Files: []string{"a.md"}, Scope: "file", WhenAnnotations: "block"}
	ho, err := w.n.HandoffTaskThen("Note", task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ho.Event.TurnEv.Task.ID != "rewrite" {
		t.Fatal(ho)
	}
	if w.read("a.md") != original || w.read("b.md") != "Autre\r\n" {
		t.Fatal("octets modifiés")
	}
	st, h, _, err := w.n.State()
	if err != nil || st.Task == nil || h.Log()[0].Task == nil {
		t.Fatal(st, err)
	}
	if st.Task.Name != "Réécrire" || st.Task.Files[0] != "a.md" {
		t.Fatal(st.Task)
	}
}
func TestTaskBlockedBeforeAnyWrite(t *testing.T) {
	original := "\xef\xbb\xbfTexte {>>Remarque<<}.\r\n"
	w := newWorld(t, map[string]string{"a.md": original})
	task := &journal.TaskInfo{ID: "rewrite", Files: []string{"a.md"}}
	called := false
	_, err := w.n.HandoffTaskThen("", task, func(int) (func(), error) { called = true; return nil, nil })
	if err == nil || called {
		t.Fatal(err, called)
	}
	if !bytes.Equal([]byte(original), []byte(w.read("a.md"))) {
		t.Fatal("annotation identifiée malgré refus")
	}
	if _, err := os.Stat(filepath.Join(w.dir, ".glose", "journal.jsonl")); !os.IsNotExist(err) {
		t.Fatal("journal écrit", err)
	}
	task.WhenAnnotations = "process"
	if _, err := w.n.HandoffTaskThen("", task, nil); err != nil {
		t.Fatal(err)
	}
}
func TestTaskRejectsFilesOutsideScope(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Texte\r\n"})
	if _, err := w.n.HandoffTaskThen("", &journal.TaskInfo{Files: []string{"../outside.md"}}, nil); err == nil {
		t.Fatal("portée acceptée")
	}
	if w.read("a.md") != "Texte\r\n" {
		t.Fatal("octets modifiés")
	}
}

func TestTaskBlocksOnlyScopedFiles(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Texte\r\n", "b.md": "{>>Remarque<<}\r\n"})
	task := &journal.TaskInfo{Files: []string{"a.md"}}
	if blocks := w.n.TaskBlocks(task, w.n.ReadFiles()); len(blocks) != 0 {
		t.Fatal(blocks)
	}
	if _, err := w.n.HandoffTaskThen("", task, nil); err != nil {
		t.Fatal(err)
	}
	w = newWorld(t, map[string]string{"a.md": "{>>Remarque<<}\r\n"})
	blocks := w.n.TaskBlocks(task, w.n.ReadFiles())
	_, err := w.n.HandoffTaskThen("", task, nil)
	if len(blocks) != 1 || err == nil || err.Error() != blocks[0].Message {
		t.Fatal(blocks, err)
	}
	if w.read("a.md") != "{>>Remarque<<}\r\n" {
		t.Fatal("octets modifiés")
	}
}

func TestTaskLabelsExplicitReplyFreeAndHandwritten(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "\ufeffUn chat.\r\n\r\n{>>À relire<<}\r\n", "b.md": "Autre.\n"})
	task := &journal.TaskInfo{ID: "rewrite", Name: "Réécrire", Files: []string{"a.md"}, WhenAnnotations: "process"}
	if _, e := w.n.HandoffTaskThen("", task, nil); e != nil {
		t.Fatal(e)
	}
	head := review.Read([]byte(w.read("a.md"))).Annotations[0]
	_, e := w.n.Do(review.Agent, 0, "", "reply", "", func(src []byte, g *review.Gen) (patch.Edit, error) {
		p, _, e := review.Reply(src, head.Span.Start, head.ID, "Réponse", review.Agent, g)
		return p, e
	})
	if e != nil {
		t.Fatal(e)
	}
	doc := review.Read([]byte(w.read("a.md")))
	if doc.Annotations[0].Task != "" || doc.Annotations[0].Replies()[0].Task != "rewrite" {
		t.Fatal(doc)
	}
	text := strings.Replace(w.read("a.md"), "chat", "chien", 1)
	w.write("a.md", text)
	w.write("b.md", "Autre.\n\n{>>Ajout manuscrit<<}\n")
	if _, e := w.n.Handoff(review.Agent, ""); e != nil {
		t.Fatal(e)
	}
	out := []byte(w.read("a.md"))
	doc = review.Read(append(append([]byte{}, out...), []byte(w.read("b.md"))...))
	seen := map[string]bool{}
	for _, a := range doc.Annotations {
		if a.Task != "" {
			if a.Task != "rewrite" {
				t.Fatal(a)
			}
			seen[a.Kind.String()] = true
		}
	}
	if len(seen) < 2 {
		t.Fatalf("modification libre ou commentaire sans mission : %s", out)
	}
	if !bytes.Contains(out, []byte("\ufeffUn ")) || !bytes.Contains(out, []byte("\r\n\r\n")) {
		t.Fatal("octets voisins modifiés")
	}
}

func TestTaskIsKeptInArchivedDecision(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Un chat.\r\n"})
	if _, e := w.n.HandoffTaskThen("", &journal.TaskInfo{ID: "rewrite", Files: []string{"a.md"}}, nil); e != nil {
		t.Fatal(e)
	}
	w.write("a.md", "Un chien.\r\n")
	if _, e := w.n.Handoff("agent", ""); e != nil {
		t.Fatal(e)
	}
	_, e := w.n.Do("author", 0, "", "decide", Accept, func(src []byte, _ *review.Gen) (patch.Edit, error) { return DecideRange(src, 0, len(src), Accept) })
	if e != nil {
		t.Fatal(e)
	}
	h, _ := w.n.History()
	found := false
	for _, op := range h.Ops {
		for _, c := range op.Op.Closed {
			if c.Task == "rewrite" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("mission perdue au journal")
	}
	if w.read("a.md") != "Un chien.\r\n" {
		t.Fatal("octets voisins modifiés")
	}
}
