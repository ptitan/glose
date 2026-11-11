package agents

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func taskFixture() Task {
	return Task{ID: "rewrite", Name: "Réécrire", Icon: "suggerer", Prompt: "Relis le texte.", Place: []string{"bar", "handover"}}
}

func TestTasksValidationAndMerge(t *testing.T) {
	a := taskFixture()
	b := a
	b.Name = "Projet"
	if err := CheckTasks([]Task{a}, true); err != nil {
		t.Fatal(err)
	}
	got := MergeTasks([]Task{a}, []Task{b})
	if len(got) != 1 || got[0].Name != "Projet" || a.Name != "Réécrire" {
		t.Fatal(got)
	}
	for _, mutate := range []func(*Task){
		func(x *Task) { x.Icon = "inconnue" }, func(x *Task) { x.Scope = "elsewhere" },
		func(x *Task) { x.Place = []string{"bar", "bar"} }, func(x *Task) { x.Workdir = "../hors" },
		func(x *Task) { x.Workdir = "C:/secret" }, func(x *Task) { x.Access = Access{ExternalDirs: "list", Dirs: []string{"/secret"}} },
		func(x *Task) { x.MaxMinutes = 241 },
	} {
		x := taskFixture()
		mutate(&x)
		if CheckTasks([]Task{x}, true) == nil {
			t.Fatalf("accepté : %+v", x)
		}
	}
	if CheckTasks([]Task{a, a}, false) == nil {
		t.Fatal("doublon accepté")
	}
}

func TestTasksStorageFidelity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GLOSE_CONFIG_DIR", dir)
	manuscript := filepath.Join(dir, "texte.md")
	original := []byte("\xef\xbb\xbf# Essai\r\n\nUn émoji 😀.\r\n")
	if err := os.WriteFile(manuscript, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Tasks: []Task{taskFixture()}, Access: Access{Web: true}}
	saved, err := SaveIfUnchanged(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.MarshalIndent(saved, "", "  ")
	if !bytes.Equal(raw, want) {
		t.Fatal("octets de configuration différents")
	}
	loaded, hash, err := LoadWithHash()
	if err != nil || loaded.Tasks[0].ID != "rewrite" {
		t.Fatal(loaded, err)
	}
	if _, err = SaveIfUnchanged(cfg, ""); err != ErrConfigChanged {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, configFile))
	if !bytes.Equal(raw, after) {
		t.Fatal("écrasement")
	}
	if _, err = SaveIfUnchanged(loaded, hash); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(manuscript)
	if !bytes.Equal(original, after) {
		t.Fatal("manuscrit modifié")
	}
}

func TestMergeAccessWidens(t *testing.T) {
	a := Access{ExternalDirs: "list", Dirs: []string{"a"}, Web: true, Scripts: "shell", MCP: []string{"one"}}
	b := Access{ExternalDirs: "none", Scripts: "python", MCP: []string{"two"}}
	got := MergeAccess(a, b)
	if !got.Web || got.Scripts != "shell" || got.ExternalDirs != "list" || len(got.MCP) != 2 {
		t.Fatal(got)
	}
}
