package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/review"
)

// D-57 : l'auteur modifie directement le texte. Aucune marque n'est écrite,
// tous les autres octets restent (BOM, fins de ligne CRLF, annotation
// voisine), le journal garde une copie de chaque modification, l'état la
// montre à l'agent, et l'annulation rend le fichier octet par octet.
func TestDirectChange(t *testing.T) {
	needPandoc(t)
	src := "\xef\xbb\xbfLe chat dort.\r\n\r\nFin {>>à revoir<<} du texte.\r\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "GET", "/api/block?file=0&hash="+hash+"&at=6", nil)
	var blk map[string]any
	json.Unmarshal(w.Body.Bytes(), &blk)
	if w.Code != http.StatusOK || blk["text"] != "Le chat dort." {
		t.Fatalf("bloc : %d %v", w.Code, blk)
	}
	start, end := int(blk["start"].(float64)), int(blk["end"].(float64))
	req := map[string]any{"file": 0, "hash": hash, "at": start + 3, "start": start, "end": end, "text": "Le chien dort bien.", "direct": true}

	// A message for the agent goes with a suggestion, not a direct change.
	bad := map[string]any{}
	for k, v := range req {
		bad[k] = v
	}
	bad["note"] = "Pourquoi ?"
	if w := do(t, s, "POST", "/api/edit", bad); w.Code != http.StatusBadRequest || read() != src {
		t.Fatalf("message joint : %d %q", w.Code, read())
	}

	w = do(t, s, "POST", "/api/edit", req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	want := strings.Replace(src, "Le chat dort.", "Le chien dort bien.", 1)
	if w.Code != http.StatusOK || read() != want {
		t.Fatalf("modification directe : %d %v\n%q\n%q", w.Code, resp, read(), want)
	}
	if marks, _ := critic.Parse([]byte(read())); len(marks) != 1 {
		t.Fatalf("marques : %d", len(marks))
	}

	// The journal keeps the changes; the state shows them.
	var st navette.State
	json.Unmarshal(do(t, s, "GET", "/api/state", nil).Body.Bytes(), &st)
	if len(st.Imposed) != 2 {
		t.Fatalf("imposées : %+v", st.Imposed)
	}
	for _, c := range st.Imposed {
		if c.Decision != navette.Direct || c.By != "author" || c.File != "a.md" || c.Line != 1 || c.ID == "" {
			t.Fatalf("copie : %+v", c)
		}
	}
	if c := st.Imposed[0]; c.Kind != "substitution" || c.Text != "chat" || c.New != "chien" {
		t.Fatalf("copie : %+v", c)
	}
	var lg struct{ Turns []navette.TurnLog }
	json.Unmarshal(do(t, s, "GET", "/api/log", nil).Body.Bytes(), &lg)
	ops := lg.Turns[len(lg.Turns)-1].Ops
	if len(ops) != 1 || ops[0].Kind != navette.Direct || len(ops[0].Closed) != 2 || len(ops[0].Created) != 0 {
		t.Fatalf("journal : %+v", ops)
	}
	// No identity of these changes is « disappeared without decision ».
	if len(st.Problems) != 0 {
		t.Fatalf("problèmes : %v", st.Problems)
	}

	// Undone like a decision: the file comes back byte for byte, and the
	// changes are no longer imposed.
	w = do(t, s, "POST", "/api/undecide", map[string]any{"file": 0, "hash": resp["hash"]})
	if w.Code != http.StatusOK || read() != src {
		t.Fatalf("annuler : %d %q", w.Code, read())
	}
	json.Unmarshal(do(t, s, "GET", "/api/state", nil).Body.Bytes(), &st)
	if len(st.Imposed) != 0 {
		t.Fatalf("imposées après annulation : %+v", st.Imposed)
	}
	json.Unmarshal(do(t, s, "GET", "/api/log", nil).Body.Bytes(), &lg)
	ops = lg.Turns[len(lg.Turns)-1].Ops
	if len(ops) != 2 || ops[1].Kind != navette.Undo || len(ops[1].Reverted) != 2 {
		t.Fatalf("journal après annulation : %+v", ops)
	}

	// Redone, then handed over: the agent sees the changes of the author's
	// part; the author can no longer change the text directly.
	req["hash"] = getDoc(t, s).Files[0].Hash
	if w := do(t, s, "POST", "/api/edit", req); w.Code != http.StatusOK || read() != want {
		t.Fatalf("rétablir : %d %q", w.Code, read())
	}
	if w := do(t, s, "POST", "/api/handoff", map[string]any{"note": ""}); w.Code != http.StatusOK {
		t.Fatalf("remise : %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(do(t, s, "GET", "/api/state", nil).Body.Bytes(), &st)
	if st.Hand != "agent" || len(st.Imposed) != 2 {
		t.Fatalf("état après remise : %+v", st)
	}
	handed := read()
	b := getDoc(t, s).Files[0]
	blkAt := strings.Index(read(), "Le chien")
	w = do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": b.Hash, "at": blkAt + 3, "start": blkAt, "end": blkAt + len("Le chien dort bien."),
		"text": "Le loup.", "direct": true})
	resp = map[string]any{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusConflict || resp["error"] != "turn" || read() != handed {
		t.Fatalf("pendant le tour de l'agent : %d %v", w.Code, resp)
	}
}

// Une note de bas de page saisie « ^[…] » dans une modification directe :
// l'appel et la définition sont écrits, sans marque.
func TestDirectFootnote(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "at": 3, "start": 0, "end": 13,
		"text": "Le chat dort^[Profondément.].", "direct": true})
	got, _ := os.ReadFile(path)
	if w.Code != http.StatusOK || strings.Contains(string(got), "{") || !strings.Contains(string(got), "dort[^") ||
		!strings.Contains(string(got), "]: Profondément.") {
		t.Fatalf("note : %d %s %q", w.Code, w.Body.String(), got)
	}
}

// Seul l'auteur modifie directement : la navette refuse une opération de
// l'agent qui en porterait une.
func TestDirectRefusedToTheAgent(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte.\n"})
	n := s.cur().N
	if w := do(t, s, "POST", "/api/handoff", map[string]any{"note": ""}); w.Code != http.StatusOK {
		t.Fatalf("remise : %d %s", w.Code, w.Body.String())
	}
	spec := &navette.Spec{Kind: navette.Direct}
	_, err := n.Run("agent", 0, "", spec, func(src []byte, _ *review.Gen, _ *navette.History) (patch.Edit, error) {
		spec.Direct = []journal.Closed{{ID: "x1", Decision: navette.Direct}}
		return patch.Edit{Start: 0, End: 6, Insert: []byte("Autre")}, nil
	})
	if !errors.Is(err, navette.ErrDirectAgent) {
		t.Fatalf("erreur : %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "Texte.\n" {
		t.Fatalf("fichier modifié : %q", b)
	}
}
