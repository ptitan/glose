package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
)

func TestProgressFidelityAndValidation(t *testing.T) {
	original := []byte("\xef\xbb\xbf# Texte\r\n\nCafé 😀.\r\n")
	dir := mkproject(t, map[string]string{"a.md": string(original)})
	t.Setenv("GLOSE_AGENT", "Sol")
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := navette.New(p)
	if code, _, _ := run("progress", "--text", "Lecture", dir); code == ExitOK {
		t.Fatal("main auteur")
	}
	if _, err := n.Handoff("author", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(p.State(), "journal.jsonl"))
	for _, text := range []string{"", strings.Repeat("é", 121), "deux\nlignes", "tab\ttexte"} {
		if code, _, _ := run("progress", "--text", text, dir); code == ExitOK {
			t.Fatal(text)
		}
	}
	text := strings.Repeat("é", 120)
	if code, out, e := run("progress", "--agent", "Sol", "--text", text, dir); code != ExitOK {
		t.Fatal(code, out, e)
	}
	st, _, _, err := n.State()
	if err != nil || st.Agent == nil || st.Agent.Progress != text || st.Agent.ProgressAt == "" {
		t.Fatal(st, err)
	}
	if code, _, e := run("status", "--agent", "Sol", dir); code != ExitOK {
		t.Fatal(e)
	}
	if navette.ReadPresence(p.State()).Progress != text {
		t.Fatal("phrase effacée par status")
	}
	after, _ := os.ReadFile(filepath.Join(p.State(), "journal.jsonl"))
	manuscript, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !bytes.Equal(before, after) || !bytes.Equal(original, manuscript) {
		t.Fatal("texte ou journal modifié")
	}
	if err := navette.Touch(p.State(), "Sol", navette.ActDone, 2); err != nil {
		t.Fatal(err)
	}
	if navette.ReadPresence(p.State()).Progress != "" {
		t.Fatal("avancement conservé après done")
	}
}
