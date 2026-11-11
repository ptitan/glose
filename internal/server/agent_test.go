package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
)

// P10.1: each poll carries the presence of the agent, computed for the
// current hand, even when neither the files nor the journal changed.
func TestHashesCarryThePresenceOfTheAgent(t *testing.T) {
	s, _ := newServer(t, map[string]string{"a.md": "Texte.\n"})
	poll := func() *navette.AgentState {
		var out struct {
			Agent *navette.AgentState `json:"agent"`
		}
		w := do(t, s, "GET", "/api/hashes", nil)
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Agent
	}
	if a := poll(); a != nil {
		t.Fatalf("personne : %+v", a)
	}
	navette.Touch(s.P.State(), "OpenCode", navette.ActWait, 0)
	if a := poll(); a == nil || a.Name != "OpenCode" || !a.Fresh || a.Taken {
		t.Fatalf("attend : %+v", a)
	}
	if _, err := s.N.Handoff("author", ""); err != nil {
		t.Fatal(err)
	}
	navette.Touch(s.P.State(), "OpenCode", navette.ActRead, 1)
	if a := poll(); a == nil || !a.Taken {
		t.Fatalf("main prise : %+v", a)
	}
}

// P10.2: the interface asks for a code; « glose join » uses it.
func TestLinkGivesAnInstruction(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	s, _ := newServer(t, map[string]string{"a.md": "Texte.\n"})
	var out struct{ Code, Expires, Instruction, Command string }
	w := do(t, s, "POST", "/api/link", map[string]any{})
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// P10.10: ready to paste, with the name of the default agent ("agent"
	// without configuration); P10.12: it says to run it again.
	if !strings.Contains(out.Instruction, "glose join "+out.Code+` --agent "agent"`) || !strings.Contains(out.Instruction, s.P.Root) ||
		strings.Contains(out.Instruction, "TON NOM") || !strings.Contains(out.Instruction, "relance-la telle quelle") || out.Expires == "" {
		t.Fatalf("%+v", out)
	}
	if _, err := navette.UseLink(s.P.State(), out.Code, "Sol", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	// With glose set: the agent of the text, else the default one.
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}}})
	json.Unmarshal(do(t, s, "POST", "/api/link", map[string]any{}).Body.Bytes(), &out)
	if !strings.Contains(out.Instruction, ` --agent "Sol"`) {
		t.Fatalf("%+v", out)
	}
	// Without the token, no code.
	if w := do(t, s, "POST", "/api/link", map[string]any{}, noAuth()); w.Code != 401 {
		t.Fatalf("sans jeton : %d", w.Code)
	}
}

// P10.3: the settings of the agents, general (GLOSE_CONFIG_DIR).
func TestAgentsSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux agents en script shell")
	}
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\necho codex-cli 9.9\n"), 0o755)
	t.Setenv("PATH", bin)
	s, _ := newServer(t, map[string]string{"a.md": "Texte.\n", "glose.json": `{"agent":{"alias":"Sol","requires":["mcp:lexharness"]}}`})
	var st struct {
		Configured bool
		Config     struct {
			Default string
			Aliases []struct{ Name, Agent, Model string }
		}
		Detection struct {
			Agents []struct{ ID, Version string }
		}
		Project struct{ Agent struct{ Alias string } }
	}
	read := func(w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		json.Unmarshal(w.Body.Bytes(), &st)
	}
	read(do(t, s, "GET", "/api/agents", nil))
	if st.Configured || st.Project.Agent.Alias != "Sol" || len(st.Detection.Agents) != 3 || st.Detection.Agents[1].Version != "codex-cli 9.9" {
		t.Fatalf("%+v", st)
	}
	var h struct{ AgentsConfigured bool }
	json.Unmarshal(do(t, s, "GET", "/api/hashes", nil).Body.Bytes(), &h)
	if h.AgentsConfigured {
		t.Fatal("configuré sans réglage")
	}
	if w := do(t, s, "POST", "/api/agents", map[string]any{"hash": "", "aliases": []map[string]string{{"name": "Sol Levant", "agent": "codex"}}}); w.Code != 400 {
		t.Fatalf("nom invalide : %d", w.Code)
	}
	if w := do(t, s, "POST", "/api/agents", map[string]any{"aliases": []map[string]string{{"name": "Sol", "agent": "codex"}}}); w.Code != 400 {
		t.Fatalf("sans empreinte : %d", w.Code)
	}
	read(do(t, s, "POST", "/api/agents", map[string]any{"hash": "", "aliases": []map[string]string{{"name": "Sol", "agent": "codex", "model": "gpt-6.1-sol"}}}))
	if !st.Configured || st.Config.Default != "Sol" {
		t.Fatalf("%+v", st)
	}
	// Codex review: a second page, opened before this save, does not
	// overwrite it.
	if w := do(t, s, "POST", "/api/agents", map[string]any{"hash": "", "aliases": []map[string]string{{"name": "Luna", "agent": "codex"}}}); w.Code != 409 ||
		!strings.Contains(w.Body.String(), "config-changed") {
		t.Fatalf("écrasement : %d %s", w.Code, w.Body)
	}
	var cur struct{ Hash string }
	json.Unmarshal(do(t, s, "GET", "/api/agents", nil).Body.Bytes(), &cur)
	if w := do(t, s, "POST", "/api/agents", map[string]any{"hash": cur.Hash, "aliases": []map[string]string{{"name": "Sol", "agent": "codex", "model": "gpt-6.1-sol"}}}); w.Code != 200 {
		t.Fatalf("avec la bonne empreinte : %d %s", w.Code, w.Body)
	}
	json.Unmarshal(do(t, s, "GET", "/api/hashes", nil).Body.Bytes(), &h)
	if !h.AgentsConfigured {
		t.Fatal("non configuré après enregistrement")
	}
	var m struct {
		Models    []string
		FromAgent bool
	}
	json.Unmarshal(do(t, s, "GET", "/api/agents/models?agent=claude", nil).Body.Bytes(), &m)
	if len(m.Models) == 0 || m.FromAgent {
		t.Fatalf("%+v", m)
	}
	// The page is public (like the interface); the settings need the token.
	if w := do(t, s, "GET", "/set", nil, noAuth()); w.Code != 200 || !strings.Contains(w.Body.String(), "reglages-agents.js") {
		t.Fatalf("page : %d", w.Code)
	}
	if w := do(t, s, "GET", "/api/agents", nil, noAuth()); w.Code != 401 {
		t.Fatalf("sans jeton : %d", w.Code)
	}
	if w := do(t, s, "GET", "/static/reglages-agents.js", nil, noAuth()); w.Code != 200 {
		t.Fatalf("script : %d", w.Code)
	}
	// An unreadable configuration is never overwritten.
	dir, _ := agents.ConfigDir()
	os.WriteFile(filepath.Join(dir, "agents.json"), []byte("{"), 0o600)
	if w := do(t, s, "POST", "/api/agents", map[string]any{"hash": "", "aliases": []map[string]string{}}); w.Code != 409 {
		t.Fatalf("fichier illisible écrasé : %d", w.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "agents.json")); string(b) != "{" {
		t.Fatal("fichier modifié")
	}
}

// P10.3: the settings server of « glose set » outside a project.
func TestServeSettings(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	urls := make(chan string, 1)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- ServeSettings("/set", func(u string) { urls <- u }, stop) }()
	u := <-urls
	m := regexp.MustCompile(`^(http://127\.0\.0\.1:\d+)/set#t=([0-9a-f]{64})$`).FindStringSubmatch(u)
	if m == nil {
		t.Fatal(u)
	}
	get := func(path, tok string) int {
		req, _ := http.NewRequest("GET", m[1]+path, nil)
		if tok != "" {
			req.Header.Set("X-Glose-Token", tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if get("/set", "") != 200 || get("/api/agents", m[2]) != 200 || get("/api/agents", "") != 401 {
		t.Fatal("routes du serveur de réglages")
	}
	// Nothing of a project there.
	if get("/api/doc", m[2]) != 404 {
		t.Fatal("route d'un projet servie")
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// P10.4: tools required by the text, and the dry run « Essayer ».
func TestAgentToolsAndTry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux agents en script shell")
	}
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\nif [ \"$1\" = mcp ]; then echo '✓ lexharness connected'; exit 0; fi\necho \"permissions: $OPENCODE_CONFIG_CONTENT\"; echo \"$@\"\n"), 0o755)
	t.Setenv("PATH", bin)
	s, dir := newServer(t, map[string]string{"a.md": "Texte.\n", "glose.json": `{"agent":{"alias":"Lex","requires":["mcp:lexharness","skill:juriste-fr"]}}`})
	before, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if w := do(t, s, "GET", "/api/agents/tools", nil); w.Code != 404 {
		t.Fatalf("sans agent configuré : %d", w.Code)
	}
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}, {Name: "Lex", Agent: "opencode", Model: "ollama-cloud/glm-5.3"}}})
	var tools struct {
		Alias  agents.Alias
		Checks []agents.ToolCheck
	}
	w := do(t, s, "GET", "/api/agents/tools", nil)
	json.Unmarshal(w.Body.Bytes(), &tools)
	// The alias of the text, not the default one.
	if tools.Alias.Name != "Lex" || len(tools.Checks) != 2 || !tools.Checks[0].Found || tools.Checks[1].Found {
		t.Fatalf("%s", w.Body)
	}
	var res struct {
		Output   string
		ExitCode int
	}
	w = do(t, s, "POST", "/api/agents/try", map[string]string{"name": "Lex", "agent": "opencode", "model": "ollama-cloud/glm-5.3"})
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.ExitCode != 0 || !strings.Contains(res.Output, `{"action":"edit","effect":"deny","resource":"*"}`) || !strings.Contains(res.Output, "glose status") || !strings.Contains(res.Output, "-m ollama-cloud/glm-5.3") {
		t.Fatalf("%s", w.Body)
	}
	if w := do(t, s, "POST", "/api/agents/try", map[string]string{"agent": "opencode", "model": "x; rm -rf"}); w.Code != 400 {
		t.Fatalf("modèle invalide : %d", w.Code)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if string(before) != string(after) {
		t.Fatal("manuscrit modifié")
	}
}

func TestHashesCarryProgressWithoutJournalChange(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "\xef\xbb\xbfTexte.\r\n"})
	if _, err := s.N.Handoff("author", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.P.State(), "journal.jsonl"))
	if err := navette.Progress(s.P.State(), "Sol", 2, "Je vérifie les références."); err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "GET", "/api/hashes", nil)
	var out struct {
		Agent *navette.AgentState `json:"agent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || out.Agent == nil || out.Agent.Progress != "Je vérifie les références." || out.Agent.ProgressAt == "" {
		t.Fatal(w.Code, w.Body)
	}
	after, _ := os.ReadFile(filepath.Join(s.P.State(), "journal.jsonl"))
	text, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if string(before) != string(after) || string(text) != "\xef\xbb\xbfTexte.\r\n" {
		t.Fatal("octets modifiés")
	}
}
