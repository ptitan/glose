package agentrun

import (
	"github.com/ptitan/glose/internal/journal"
	"strings"
	"testing"
)

func TestTaskPrompt(t *testing.T) {
	base := TurnPrompt("Sol", "racine", "Consignes du texte", nil)
	p := TaskPrompt(base, "racine", journal.TaskInfo{Name: "Réécrire", Prompt: "Relis tout.", Workdir: "travail", Scope: "files", Files: []string{"a.md", "chapitre/b.md"}})
	if !strings.HasPrefix(p, base) {
		t.Fatal("règles de navette remplacées")
	}
	for _, want := range []string{"Relis tout.", `["a.md","chapitre/b.md"]`, "AGENTS.md et CLAUDE.md", "travail", "racine", "modifications libres", "glose propose --whole", "glose done"} {
		if !strings.Contains(p, want) {
			t.Fatal(want)
		}
	}
}
