package agents

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeAgents puts in a PATH of its own shell scripts named like the agents.
func fakeAgents(t *testing.T, scripts map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux agents en script shell")
	}
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestDetect(t *testing.T) {
	fakeAgents(t, map[string]string{
		"claude": `echo "2.1.318 (Claude Code)"`,
		"codex":  `echo "codex-cli 0.166.0"; echo "seconde ligne"`,
	})
	d := Detect(context.Background())
	if len(d.Agents) != len(Kinds) || d.At == "" {
		t.Fatalf("%+v", d)
	}
	got := map[string]Detected{}
	for _, a := range d.Agents {
		got[a.ID] = a
	}
	if a := got["claude"]; !a.Found || a.Version != "2.1.318 (Claude Code)" || a.Name != "Claude Code" {
		t.Fatalf("claude : %+v", a)
	}
	if a := got["codex"]; !a.Found || a.Version != "codex-cli 0.166.0" {
		t.Fatalf("codex : %+v", a)
	}
	if a := got["opencode"]; a.Found {
		t.Fatalf("opencode absent : %+v", a)
	}
}

func TestDetectReportsABrokenAgent(t *testing.T) {
	fakeAgents(t, map[string]string{"opencode": `echo "boum" >&2; exit 3`})
	for _, a := range Detect(context.Background()).Agents {
		if a.ID == "opencode" && (!a.Found || !strings.Contains(a.Error, "boum")) {
			t.Fatalf("%+v", a)
		}
	}
}

func TestModels(t *testing.T) {
	fakeAgents(t, map[string]string{
		"opencode": `echo "anthropic/claude-sonnet-5"; echo ""; echo "ollama-cloud/glm-5.3"; echo "pas un modèle !"`,
		"codex":    `echo '{"models":[{"slug":"gpt-6.1-sol","visibility":"list"},{"slug":"cache","visibility":"hide"},{"slug":"gpt-6-luna"}]}'`,
	})
	ctx := context.Background()
	m, from, err := Models(ctx, "opencode")
	if err != nil || !from || strings.Join(m, ",") != "anthropic/claude-sonnet-5,ollama-cloud/glm-5.3" {
		t.Fatalf("opencode : %v %v %v", m, from, err)
	}
	m, from, err = Models(ctx, "codex")
	if err != nil || !from || strings.Join(m, ",") != "gpt-6.1-sol,gpt-6-luna" {
		t.Fatalf("codex : %v %v %v", m, from, err)
	}
	// Claude Code gives no list: the known names.
	m, from, err = Models(ctx, "claude")
	if err != nil || from || len(m) == 0 {
		t.Fatalf("claude : %v %v %v", m, from, err)
	}
	if _, _, err := Models(ctx, "inconnu"); err == nil {
		t.Fatal("agent inconnu accepté")
	}
}

func TestConfig(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	c, err := Load()
	if err != nil || c.Configured() || len(c.Aliases) != 0 {
		t.Fatalf("sans configuration : %+v %v", c, err)
	}
	c, err = Save(Config{Aliases: []Alias{{Name: "Sol", Agent: "codex", Model: "gpt-6.1-sol"}, {Name: "Claude", Agent: "claude"}}})
	if err != nil || c.Default != "Sol" || !c.Configured() {
		t.Fatalf("défaut implicite : %+v %v", c, err)
	}
	if _, err := Save(Config{Default: "claude", Aliases: c.Aliases}); err != nil {
		t.Fatal(err)
	}
	c, err = Load()
	if err != nil || c.Default != "Claude" {
		t.Fatalf("relu : %+v %v", c, err)
	}
	if a, ok := c.Alias("sol"); !ok || a.Model != "gpt-6.1-sol" {
		t.Fatalf("alias : %+v", a)
	}
	for _, bad := range []Config{
		{Aliases: []Alias{{Name: "Sol Levant", Agent: "codex"}}},
		{Aliases: []Alias{{Name: "Sol", Agent: "codex"}, {Name: "sol", Agent: "claude"}}},
		{Aliases: []Alias{{Name: "Sol", Agent: "gemini"}}},
		{Aliases: []Alias{{Name: "Sol", Agent: "codex", Model: "gpt 6"}}},
		{Default: "Luna", Aliases: []Alias{{Name: "Sol", Agent: "codex"}}},
	} {
		if _, err := Save(bad); err == nil {
			t.Errorf("accepté : %+v", bad)
		}
	}
	// An unreadable file is an error, never overwritten silently.
	dir, _ := ConfigDir()
	os.WriteFile(filepath.Join(dir, configFile), []byte("{"), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("fichier illisible accepté")
	}
}

func TestCheckAlias(t *testing.T) {
	for _, ok := range []string{"Sol", "claude-opus", "gpt_6.1", "a"} {
		if CheckAlias(ok) != nil {
			t.Errorf("%q refusé", ok)
		}
	}
	for _, bad := range []string{"", "Sol Levant", "élan", "a=b", strings.Repeat("a", 33), "x<<}"} {
		if CheckAlias(bad) == nil {
			t.Errorf("%q accepté", bad)
		}
	}
}

func TestDetectionCache(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	fakeAgents(t, map[string]string{"claude": `echo 1.0`})
	d := CachedDetection(context.Background())
	if d.Agents[0].Version != "1.0" {
		t.Fatalf("%+v", d)
	}
	fakeAgents(t, map[string]string{"claude": `echo 2.0`})
	if d := CachedDetection(context.Background()); d.Agents[0].Version != "1.0" {
		t.Fatalf("cache non gardé : %+v", d)
	}
}

// Codex review: a save from a page read before another save is refused.
func TestSaveIfUnchanged(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	_, h0, err := LoadWithHash()
	if err != nil || h0 != "" {
		t.Fatal(h0, err)
	}
	if _, err := SaveIfUnchanged(Config{Aliases: []Alias{{Name: "Sol", Agent: "codex"}}}, h0); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveIfUnchanged(Config{Aliases: []Alias{{Name: "Luna", Agent: "codex"}}}, h0); err != ErrConfigChanged {
		t.Fatalf("écrasement : %v", err)
	}
	c, h1, _ := LoadWithHash()
	if c.Default != "Sol" || h1 == "" {
		t.Fatalf("%+v", c)
	}
	if _, err := SaveIfUnchanged(Config{Aliases: []Alias{{Name: "Luna", Agent: "codex"}}}, h1); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	fakeAgents(t, map[string]string{"opencode": `echo "✓ lexharness  connected"; echo "✓ context7 connected"`})
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".opencode", "skills", "juriste-fr"), 0o755)
	os.WriteFile(filepath.Join(root, ".opencode", "skills", "juriste-fr", "SKILL.md"), []byte("---\nname: juriste-fr\n---\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "synced", "abc", "redaction"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "synced", "abc", "redaction", "SKILL.md"), []byte("x"), 0o644)
	checks, err := CheckTools(context.Background(), "opencode", root,
		[]string{"mcp:lexharness", "mcp:lex", "skill:juriste-fr", "skill:absent", "skill:redaction"})
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, true, false, true}
	for i, c := range checks {
		if c.Found != want[i] {
			t.Errorf("%s : %+v", c.Require, c)
		}
	}
	if !strings.Contains(checks[0].Detail, "lexharness") || !strings.Contains(checks[3].Detail, "emplacements connus") {
		t.Errorf("%+v", checks)
	}
	// An agent without MCP listing: an error per MCP, said.
	checks, _ = CheckTools(context.Background(), "codex", root, []string{"mcp:lexharness"})
	if checks[0].Found || !strings.Contains(checks[0].Detail, "illisible") {
		t.Errorf("%+v", checks)
	}
}

// Real use: OpenCode lists nothing, then « pending », in a directory it has
// not seen; Claude Code keeps removed skills in skills/.trash.
func TestCheckToolsWaitsForTheList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	count := filepath.Join(t.TempDir(), "n")
	fakeAgents(t, map[string]string{"opencode": `n=0; [ -f ` + count + ` ] && read n < ` + count + `; n=$((n+1)); echo $n > ` + count + `
if [ $n -eq 1 ]; then echo "No MCP servers configured"; exit 0; fi
if [ $n -eq 2 ]; then echo "○ lexharness  pending"; exit 0; fi
echo "✓ lexharness  connected"`})
	prev := mcpRetry
	mcpRetry = 10 * time.Millisecond
	defer func() { mcpRetry = prev }()
	os.MkdirAll(filepath.Join(home, ".claude", "skills", ".trash", "123", "juriste-fr"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", ".trash", "123", "juriste-fr", "SKILL.md"), []byte("x"), 0o644)
	checks, err := CheckTools(context.Background(), "opencode", t.TempDir(), []string{"mcp:lexharness", "skill:juriste-fr"})
	if err != nil || !checks[0].Found || !strings.Contains(checks[0].Detail, "lexharness") || checks[1].Found {
		t.Fatalf("%+v %v", checks, err)
	}
}

func TestOpencodeModel(t *testing.T) {
	head := []byte(`{
  "info": {
    "id": "ses_x",
    "agent": "god",
    "model": {
      "id": "opus",
      "providerID": "claude-agent",
      "variant": "high"
    },
    "title": "…`)
	if got := opencodeModel(head); got != "claude-agent/opus#high" {
		t.Fatal(got)
	}
	if got := opencodeModel([]byte(`{"info":{"model":{"id":"glm-5.3","providerID":"ollama-cloud"}}}`)); got != "ollama-cloud/glm-5.3" {
		t.Fatal(got)
	}
	if opencodeModel([]byte(`{"info":{}}`)) != "" {
		t.Fatal("modèle inventé")
	}
	t.Setenv("OPENCODE_SESSION_ID", "")
	if SessionModel(context.Background()) != "" {
		t.Fatal("hors d'OpenCode")
	}
}
