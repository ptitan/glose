package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptitan/glose/internal/agentrun"
	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

func (s *Server) resolveTask(c current, id string, files []string) (*journal.TaskInfo, error) {
	p, err := project.Load(c.P.Source)
	if err != nil {
		return nil, err
	}
	c.P = p
	cfg, err := agents.Load()
	if err != nil {
		return nil, err
	}
	var task *agents.Task
	for _, t := range agents.MergeTasks(cfg.Tasks, c.P.Tasks) {
		if t.ID == id {
			cp := t
			task = &cp
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("consigne inconnue : %s", id)
	}
	a, ok := textAgent(cfg, c.P)
	if task.Agent != "" {
		a, ok = cfg.Alias(task.Agent)
	}
	if !ok {
		return nil, fmt.Errorf("agent de la consigne inconnu : %s", task.Agent)
	}
	scope := task.Scope
	if scope == "" {
		scope = "file"
	}
	if scope == "project" {
		files = append([]string{}, c.P.Files...)
	}
	if len(files) == 0 || (scope == "file" && len(files) != 1) {
		return nil, fmt.Errorf("choisissez les fichiers de la consigne (portée %s)", scope)
	}
	known := map[string]bool{}
	for _, p := range c.P.Files {
		known[p] = true
	}
	seen := map[string]bool{}
	for _, p := range files {
		if !known[p] || seen[p] {
			return nil, fmt.Errorf("fichier de consigne inconnu ou répété : %s", p)
		}
		seen[p] = true
	}
	// Relative task directories are resolved from the current project root.
	access := agents.MergeAccess(cfg.Access, task.Access)
	for _, r := range c.P.Agent.Requires {
		if m, ok := strings.CutPrefix(r, "mcp:"); ok {
			access = agents.MergeAccess(access, agents.Access{MCP: []string{m}})
		}
	}
	for i, d := range access.Dirs {
		if !filepath.IsAbs(d) {
			d = filepath.Join(c.P.Root, d)
		}
		resolved, err := filepath.EvalSymlinks(d)
		if err != nil {
			return nil, fmt.Errorf("dossier d'accès : %w", err)
		}
		fi, err := os.Stat(resolved)
		if err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("dossier d'accès introuvable : %s", d)
		}
		access.Dirs[i] = resolved
	}
	dir := task.Workdir
	if dir == "" {
		dir = c.P.Root
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(c.P.Root, dir)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("dossier de lancement : %w", err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("dossier de lancement introuvable : %s", dir)
	}
	// A project override must remain inside the project even through a symlink.
	for _, t := range c.P.Tasks {
		if t.ID == id {
			rel, e := filepath.Rel(c.P.Root, dir)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("dossier de lancement extérieur au projet")
			}
		}
	}
	minutes := task.MaxMinutes
	if minutes == 0 {
		minutes = int(cfg.Max().Minutes())
	}
	warnings := agentrun.AccessWarnings(a.Agent, access)
	if rel, e := filepath.Rel(c.P.Root, dir); e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		warnings = append(warnings, "Le dossier de lancement extérieur est lisible pour charger ses consignes.")
		if a.Agent == "codex" {
			warnings = append(warnings, "Codex : le dossier de lancement est aussi accessible en écriture ; externalWrite=false n'y est pas applicable.")
		}
	}
	return &journal.TaskInfo{ID: task.ID, Icon: task.Icon, Name: task.Name, Agent: a, Scope: scope, Files: append([]string{}, files...), Access: access, Warnings: warnings, Workdir: dir, MaxMinutes: minutes, Prompt: task.Prompt, WhenAnnotations: task.WhenAnnotations}, nil
}

// taskPreview resolves grants with exactly the same code used at handover.
func (s *Server) taskPreview(w http.ResponseWriter, r *http.Request) {
	files := r.URL.Query()["file"]
	task, err := s.resolveTask(s.cur(), r.URL.Query().Get("id"), files)
	if err != nil {
		jsonError(w, 400, "task", err.Error())
		return
	}
	n := navette.New(s.cur().P)
	writeJSON(w, 200, map[string]any{"task": task, "hash": taskHash(task), "blocks": n.TaskBlocks(task, n.ReadFiles())})
}

func taskHash(t *journal.TaskInfo) string { b, _ := json.Marshal(t); return store.Hash(b) }
