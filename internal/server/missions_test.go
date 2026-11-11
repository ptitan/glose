package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ptitan/glose/internal/journal"
)

func TestMissionLabelsInDocumentDoNotRewriteExistingAnnotations(t *testing.T) {
	needPandoc(t)
	src := []byte("\ufeff{++Texte++}{>>glose:v1 id=abcd by=agent task=rewrite<<}\r\n")
	s, dir := newServer(t, map[string]string{"a.md": string(src)})
	_, e := s.N.J.Append(journal.Event{Type: journal.Turn, Actor: "author", TurnEv: &journal.TurnInfo{Number: 1, Task: &journal.TaskInfo{ID: "rewrite", Name: "Réécrire", Icon: "pen-line"}}})
	if e != nil {
		t.Fatal(e)
	}
	w := do(t, s, "GET", "/api/doc", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var d docResponse
	if e := json.Unmarshal(w.Body.Bytes(), &d); e != nil {
		t.Fatal(e)
	}
	if d.Annotations[0].Task != "rewrite" || d.TaskLabels["rewrite"].Name != "Réécrire" || d.TaskLabels["rewrite"].Icon != "pen-line" {
		t.Fatal(w.Body)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !bytes.Equal(after, src) {
		t.Fatal("annotation réécrite")
	}
}
