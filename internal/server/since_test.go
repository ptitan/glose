package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/review"
)

func TestSinceReferencesAndFiles(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "\ufeffUn chat.\r\n", "b.md": "Disparu.\n"})
	if _, e := s.N.Handoff(review.Author, ""); e != nil {
		t.Fatal(e)
	}
	journalBefore, _ := os.ReadFile(filepath.Join(dir, ".glose", journal.Name))
	current := []byte("\ufeffUn chien.\r\n")
	os.WriteFile(filepath.Join(dir, "a.md"), current, 0600)
	os.Remove(filepath.Join(dir, "b.md"))
	os.WriteFile(filepath.Join(dir, "c.md"), []byte("Nouveau."), 0600)
	s.refresh()
	w := do(t, s, "GET", "/api/since", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var out sinceResult
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if out.Turn != 1 || len(out.References) != 1 || len(out.Changes) != 3 {
		t.Fatalf("%s", w.Body)
	}
	seen := map[string]sinceChange{}
	for _, c := range out.Changes {
		seen[c.Path] = c
	}
	if seen["a.md"].Old != "chat" || seen["a.md"].New != "chien" || seen["c.md"].Status != "appeared" || seen["b.md"].Status != "disappeared" {
		t.Fatal(seen)
	}
	if do(t, s, "GET", "/api/since?turn=2", nil).Code != 400 {
		t.Fatal("référence absente acceptée")
	}
	if do(t, s, "GET", "/api/since", nil, noAuth()).Code != 401 {
		t.Fatal("sans jeton")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	j, _ := os.ReadFile(filepath.Join(dir, ".glose", journal.Name))
	if !bytes.Equal(current, got) || !bytes.Equal(journalBefore, j) {
		t.Fatal("consultation écrite")
	}
	h, _ := s.N.History()
	hash := h.Turns[0].TurnEv.Files[0].Hash
	os.Remove(filepath.Join(dir, ".glose", journal.SnapshotDir, hash))
	w = do(t, s, "GET", "/api/since", nil)
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.References) != 0 || out.Turn != 0 {
		t.Fatal(w.Body)
	}
}

func TestSinceDefaultsToLatestAuthorHandover(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Un chat.\n"})
	if _, e := s.N.Handoff(review.Author, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := s.N.Handoff(review.Agent, ""); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Un chien.\n"), 0600)
	if _, e := s.N.Handoff(review.Author, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := s.N.Handoff(review.Agent, ""); e != nil {
		t.Fatal(e)
	}
	current := []byte("Un {~~chien~>loup~~}.\n")
	os.WriteFile(filepath.Join(dir, "a.md"), current, 0600)
	for _, tc := range []struct {
		route         string
		turn, changes int
	}{{"/api/since", 3, 0}, {"/api/since?turn=1", 1, 1}} {
		w := do(t, s, "GET", tc.route, nil)
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		var got sinceResult
		if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Turn != tc.turn || len(got.Changes) != tc.changes || len(got.References) != 2 {
			t.Fatal(w.Body)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !bytes.Equal(after, current) {
		t.Fatal("comparaison écrite")
	}
}
