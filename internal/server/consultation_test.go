package server

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectConsultationDoesNotWriteManuscriptsOrJournal(t *testing.T) {
	needPandoc(t)
	a := "\ufeff# Introduction\r\n\r\nCafé {~~ancien~>nouveau~~}{>>secret<<}.\r\n"
	b := "# Suite {#suite}\n\nUn café dans un autre fichier.\n"
	s, dir := newServer(t, map[string]string{"a.md": a, "chapitre/b.md": b})
	for _, q := range []string{"café", "secret", "nouveau"} {
		w := do(t, s, "GET", "/api/search?q="+url.QueryEscape(q), nil)
		if w.Code != 200 {
			t.Fatalf("recherche : %d %s", w.Code, w.Body)
		}
		var res struct {
			Total   int
			Matches []struct {
				Path       string
				Start, End int
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		want := 0
		if q == "café" {
			want = 2
		}
		if res.Total != want {
			t.Fatalf("%q : %s", q, w.Body)
		}
	}
	w := do(t, s, "GET", "/api/search?q=nouveau&view=proposed", nil)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"total":1`)) {
		t.Fatal(w.Body)
	}
	w = do(t, s, "GET", "/api/resolve?from=a.md&href="+url.QueryEscape("chapitre/b.md#suite"), nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"path":"chapitre/b.md"`)) {
		t.Fatalf("renvoi : %d %s", w.Code, w.Body)
	}
	for _, route := range []string{"/api/search?q=café", "/api/resolve?from=a.md&href=%23gl-introduction"} {
		if w := do(t, s, "GET", route, nil, noAuth()); w.Code != 401 {
			t.Fatalf("consultation publique : %s", route)
		}
	}
	for _, route := range []string{"/api/search?q=", "/api/search?q=" + url.QueryEscape(string(bytes.Repeat([]byte("x"), 513))), "/api/resolve?from=absent.md&href=x.md"} {
		if w := do(t, s, "GET", route, nil); w.Code != 400 {
			t.Fatalf("entrée acceptée : %s %d", route, w.Code)
		}
	}
	for p, want := range map[string]string{"a.md": a, "chapitre/b.md": b} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || string(got) != want {
			t.Fatalf("source modifiée : %s %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".glose", "journal.jsonl")); !os.IsNotExist(err) {
		t.Fatal("journal créé par une consultation", err)
	}
}
