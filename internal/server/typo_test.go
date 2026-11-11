package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

func TestTypoProposesAndAcceptsOnlyItsCorrections(t *testing.T) {
	needPandoc(t)
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	original := []byte("\ufeffUn texte...\r\n\r\nVoici : art. 12 ; n° 3 ! Vraiment ?\n\nIl dit \"Bonjour\". Un appel [^1].\n\n[^1]: Note.\n\n`code...` et [lien...](cible).\n\n{~~ancien~>nouveau~~}{>>glose:v1 id=abcd by=agent via=Autre<<}\n")
	s, dir := newServer(t, map[string]string{"a.md": string(original)})
	req := map[string]any{"file": 0, "hash": store.Hash(original)}
	w := do(t, s, "POST", "/api/typo", req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !bytes.Equal(projection.Project(after, projection.Current).Text, projection.Project(original, projection.Current).Text) {
		t.Fatal("original changé")
	}
	anns := render.Annotations(0, after)
	count := 0
	for _, a := range anns {
		if a.Via == "Typo" {
			count++
		}
	}
	if count < 9 {
		t.Fatalf("corrections insuffisantes : %d\n%s", count, after)
	}
	if w = do(t, s, "POST", "/api/typo", req); w.Code != 409 {
		t.Fatal("empreinte périmée", w.Code)
	}
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": store.Hash(after), "start": 0, "end": len(after), "decision": "accept", "via": "Typo"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	accepted, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	a := review.Read(accepted).Annotations
	if len(a) != 1 || a[0].Via != "Autre" {
		t.Fatalf("autre proposition changée : %s", accepted)
	}
	if !bytes.Contains(accepted, []byte("`code...` et [lien...](cible)")) {
		t.Fatal("code ou lien modifié")
	}
	h, _ := s.N.History()
	if len(h.Ops) != 2 || h.Ops[0].Op.Kind != "typo" {
		t.Fatal("journal")
	}
	j, _ := os.ReadFile(filepath.Join(dir, ".glose", journal.Name))
	if !bytes.Contains(j, []byte(`"decision":"accept"`)) {
		t.Fatal("décisions absentes")
	}
}
func TestTypoRefusesAgentTurnAndInvalidRules(t *testing.T) {
	needPandoc(t)
	cfg := t.TempDir()
	t.Setenv("GLOSE_CONFIG_DIR", cfg)
	src := []byte("Texte...\r\n")
	s, dir := newServer(t, map[string]string{"a.md": string(src)})
	os.WriteFile(filepath.Join(cfg, "typo.json"), []byte("invalide"), 0600)
	req := map[string]any{"file": 0, "hash": store.Hash(src)}
	if w := do(t, s, "POST", "/api/typo", req); w.Code != 422 {
		t.Fatal(w.Code, w.Body)
	}
	if _, e := s.N.Handoff(review.Author, ""); e != nil {
		t.Fatal(e)
	}
	j, _ := os.ReadFile(filepath.Join(dir, ".glose", journal.Name))
	if w := do(t, s, "POST", "/api/typo", req); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	j2, _ := os.ReadFile(filepath.Join(dir, ".glose", journal.Name))
	if !bytes.Equal(src, after) || !bytes.Equal(j, j2) {
		t.Fatal("écriture après refus")
	}
}
