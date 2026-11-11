package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/trust"
)

// marker is a filter that leaves a file when it runs: what an agent could
// write to run its own code with the rights of the author (audit A1).
const marker = `function Pandoc(doc)
  local f = assert(io.open("ran.txt", "wb"))
  f:write("filtre exécuté")
  f:close()
  return doc
end
`

func gatedServer(t *testing.T) (*Server, string) {
	t.Helper()
	needPandoc(t)
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	s, dir := newServer(t, map[string]string{"a.md": "Un texte.\n", "glose.json": `{"filter":"f.lua"}`, "f.lua": marker})
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s = New(p, port, token)
	return s, dir
}

func approvalOf(t *testing.T, s *Server) *trust.Need {
	t.Helper()
	w := do(t, s, "GET", "/api/doc", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var d struct{ Approval *trust.Need }
	json.Unmarshal(w.Body.Bytes(), &d)
	return d.Approval
}

// D-64: a filter is not run before the author approves it, nor once changed.
func TestFilterRunsOnlyOnceApproved(t *testing.T) {
	s, dir := gatedServer(t)
	ran := filepath.Join(dir, "ran.txt")
	need := approvalOf(t, s)
	if need == nil || need.Filter != "f.lua" || need.FilterHash == "" {
		t.Fatalf("approbation non demandée : %+v", need)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("filtre exécuté sans approbation")
	}
	// /api/hashes says it too, for the interface to notice a change.
	w := do(t, s, "GET", "/api/hashes", nil)
	if !strings.Contains(w.Body.String(), `"approval":{"filter":"f.lua"`) {
		t.Fatalf("hashes : %s", w.Body)
	}
	// No route approves (Codex can read the token of the session): the
	// author approves in a terminal (glose approve).
	if w := do(t, s, "POST", "/api/approve", need); w.Code == 200 {
		t.Fatalf("approbation par l'API : %d", w.Code)
	}
	if got := approvalOf(t, s); got == nil {
		t.Fatal("approuvé par l'API")
	}
	if _, err := trust.Approve(dir, *need); err != nil {
		t.Fatal(err)
	}
	if got := approvalOf(t, s); got != nil {
		t.Fatalf("toujours en attente : %+v", got)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Fatal("filtre approuvé non exécuté")
	}
	// Changed under the same name (an agent): not run any more.
	os.Remove(ran)
	os.WriteFile(filepath.Join(dir, "f.lua"), []byte(marker+"-- changé\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Un autre texte.\n"), 0o644)
	again := approvalOf(t, s)
	if again == nil || again.FilterHash == need.FilterHash {
		t.Fatalf("filtre changé non signalé : %+v", again)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("filtre changé exécuté")
	}
}

// D-64: the program named by the profile runs only once approved; one of
// the project itself never.
func TestPandocOfTheProfileRunsOnlyOnceApproved(t *testing.T) {
	needPandoc(t)
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	_, dir := newServer(t, map[string]string{"a.md": "Un texte.\n", "glose.json": `{"pandoc":"/absent/pandoc"}`})
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(p, port, token)
	// Not approved: rendered with the pandoc of the PATH, and said.
	need := approvalOf(t, s)
	if need == nil || need.Pandoc != "/absent/pandoc" {
		t.Fatalf("%+v", need)
	}
	// Inside the project: never approved.
	if _, err := trust.Approve(dir, trust.Of(dir, "./outils/pandoc", "")); err != trust.ErrInside {
		t.Fatalf("programme du projet approuvé : %v", err)
	}
}
