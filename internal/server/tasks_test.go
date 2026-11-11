package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/journal"
)

func TestTaskPreviewAndHandover(t *testing.T) {
	s, dir := launchWorld(t, "crash")
	cfg, err := agents.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tasks = []agents.Task{{ID: "rewrite", Name: "Réécrire", Icon: "suggerer", Prompt: "Relis le texte.", Place: []string{"bar"}, Scope: "file", MaxMinutes: 7}}
	cfg.Access = agents.Access{Web: true}
	if _, err := agents.Save(cfg); err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "GET", "/api/tasks/preview?id=rewrite&file=a.md", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var reply struct {
		Task journal.TaskInfo `json:"task"`
		Hash string           `json:"hash"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	preview := reply.Task
	if preview.Workdir != dir || preview.MaxMinutes != 7 || !preview.Access.Web || len(preview.Warnings) == 0 {
		t.Fatal(preview)
	}
	for _, body := range []map[string]any{
		{"task": "rewrite", "files": []string{"a.md"}},
		{"task": "inconnue", "files": []string{"a.md"}, "confirmAccess": true},
		{"task": "rewrite", "files": []string{"../secret.md"}, "confirmAccess": true},
	} {
		w = do(t, s, "POST", "/api/handoff", body)
		if w.Code == 200 {
			t.Fatal(w.Body)
		}
	}
	original, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	w = do(t, s, "POST", "/api/handoff", map[string]any{"task": "rewrite", "files": []string{"a.md"}, "confirmAccess": true, "taskHash": reply.Hash})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	s.runner.Wait()
	h, err := s.N.History()
	if err != nil {
		t.Fatal(err)
	}
	task := h.Turns[0].TurnEv.Task
	if task == nil || task.ID != "rewrite" || task.MaxMinutes != 7 || !task.Access.Web {
		t.Fatal(task)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if string(original) != string(after) {
		t.Fatal("manuscrit modifié")
	}
	log, _ := os.ReadFile(filepath.Join(dir, ".glose", "agent", "tour-1.log"))
	if !strings.Contains(string(log), "consigne reçue") {
		t.Fatal(string(log))
	}
}

func TestTaskChangedAccessRefusesHandover(t *testing.T) {
	s, dir := launchWorld(t, "crash")
	cfg, _ := agents.Load()
	cfg.Tasks = []agents.Task{{ID: "rewrite", Name: "Réécrire", Icon: "suggerer", Prompt: "Relis.", Place: []string{"handover"}, Scope: "project"}}
	if _, err := agents.Save(cfg); err != nil {
		t.Fatal(err)
	}
	task, err := s.resolveTask(s.cur(), "rewrite", nil)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := taskHash(task)
	cfg.Access.Scripts = "shell"
	if _, err := agents.Save(cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	w := do(t, s, "POST", "/api/handoff", map[string]any{"task": "rewrite", "files": []string{"a.md"}, "confirmAccess": true, "taskHash": oldHash, "note": "Conserver cette note."})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "task-changed") {
		t.Fatal(w.Code, w.Body)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if string(before) != string(after) {
		t.Fatal("fichier modifié")
	}
	h, _ := s.N.History()
	if len(h.Turns) != 0 {
		t.Fatal("remise enregistrée malgré le refus")
	}
}

func TestTaskProjectMergeAndWorkdir(t *testing.T) {
	s, dir := launchWorld(t, "crash")
	cfg, _ := agents.Load()
	cfg.Tasks = []agents.Task{{ID: "rewrite", Name: "Global", Icon: "suggerer", Prompt: "Relis.", Place: []string{"bar"}}}
	cfg.Access.Web = true
	if _, err := agents.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "travail"), 0700); err != nil {
		t.Fatal(err)
	}
	c := s.cur()
	c.P.Tasks = []agents.Task{{ID: "rewrite", Name: "Projet", Icon: "suggerer", Prompt: "Réécris.", Place: []string{"handover"}, Scope: "files", Workdir: "travail", Access: agents.Access{Scripts: "python"}}}
	profile, _ := json.Marshal(map[string]any{"tasks": c.P.Tasks})
	if err := os.WriteFile(filepath.Join(dir, "glose.json"), profile, 0600); err != nil {
		t.Fatal(err)
	}
	task, err := s.resolveTask(c, "rewrite", []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Name != "Projet" || !task.Access.Web || task.Access.Scripts != "python" || task.Workdir != filepath.Join(dir, "travail") {
		t.Fatal(task)
	}
	if _, err := s.resolveTask(c, "rewrite", nil); err == nil {
		t.Fatal("portée vide acceptée")
	}
}

func TestTaskProfileChangedAfterPreview(t *testing.T) {
	s, dir := launchWorld(t, "crash")
	task := agents.Task{ID: "rewrite", Name: "Réécrire", Icon: "suggerer", Prompt: "Relis.", Place: []string{"bar"}}
	write := func(tk agents.Task) {
		b, _ := json.Marshal(map[string]any{"tasks": []agents.Task{tk}})
		if err := os.WriteFile(filepath.Join(dir, "glose.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(task)
	preview, err := s.resolveTask(s.cur(), "rewrite", []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	task.Access.Web = true
	write(task)
	w := do(t, s, "POST", "/api/handoff", map[string]any{"task": "rewrite", "files": []string{"a.md"}, "confirmAccess": true, "taskHash": taskHash(preview)})
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	s.refresh()
	if len(s.cur().P.Tasks) != 1 || !s.cur().P.Tasks[0].Access.Web {
		t.Fatal("consignes non actualisées")
	}
}

func TestTaskScopedPreviewAndRefusal(t *testing.T) {
	s, dir := launchWorld(t, "crash")
	cfg, _ := agents.Load()
	cfg.Tasks = []agents.Task{{ID: "rewrite", Name: "Réécrire", Icon: "suggerer", Prompt: "Relis.", Place: []string{"bar"}}}
	if _, err := agents.Save(cfg); err != nil {
		t.Fatal(err)
	}
	original := "\xef\xbb\xbfTexte {>>Remarque<<}.\r\n"
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "GET", "/api/tasks/preview?id=rewrite&file=a.md", nil)
	var preview struct {
		Hash   string `json:"hash"`
		Blocks []struct {
			Message string `json:"message"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(preview.Blocks) != 1 {
		t.Fatal(w.Code, w.Body)
	}
	w = do(t, s, "POST", "/api/handoff", map[string]any{"task": "rewrite", "files": []string{"a.md"}, "taskHash": preview.Hash, "confirmAccess": true})
	if w.Code == 200 || !strings.Contains(w.Body.String(), preview.Blocks[0].Message) {
		t.Fatal(w.Code, w.Body)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if string(after) != original {
		t.Fatal("octets modifiés")
	}
}

func TestTaskOtherFileDoesNotBlockLaunch(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("GLOSE_AGENT", "")
	useFakeAgent(t, "crash")
	task := agents.Task{ID: "rewrite", Name: "Réécrire", Icon: "suggerer", Prompt: "Relis.", Place: []string{"bar"}}
	if _, err := agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}}, Tasks: []agents.Task{task}}); err != nil {
		t.Fatal(err)
	}
	s, dir := newServer(t, map[string]string{"a.md": "\xef\xbb\xbfTexte.\r\n", "b.md": "Autre {>>Remarque<<}.\r\n"})
	t.Cleanup(func() { s.runner.Stop(s.P.State()); s.runner.Wait() })
	preview, err := s.resolveTask(s.cur(), "rewrite", []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "POST", "/api/handoff", map[string]any{"task": "rewrite", "files": []string{"a.md"}, "taskHash": taskHash(preview), "confirmAccess": true})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	s.runner.Wait()
	data, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if string(data) != "\xef\xbb\xbfTexte.\r\n" {
		t.Fatal("fichier choisi modifié")
	}
	h, err := s.N.History()
	if err != nil || len(h.Turns) != 1 {
		t.Fatal(h, err)
	}
}
