package agentrun

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakePath(t *testing.T, path string) {
	t.Helper()
	prev := lookPath
	lookPath = func(string) (string, error) { return path, nil }
	t.Cleanup(func() { lookPath = prev })
}

func env(cmdEnv []string, key string) (string, bool) {
	for i := len(cmdEnv) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(cmdEnv[i], "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func TestCommandLines(t *testing.T) {
	fakePath(t, "/usr/bin/agent")
	ctx := context.Background()
	dir := t.TempDir()
	cases := []struct {
		spec       Spec
		want, not  []string
		stdin      bool
		permission string
	}{
		{Spec{Agent: "claude", Model: "opus", MCP: []string{"lexharness"}},
			[]string{"-p", "--permission-mode dontAsk", "Bash(glose *)", "Read(/**)", "Edit(/**)", "mcp__lexharness", "mcp__claude_ai_lexharness", "--model opus",
				"--disallowedTools Read(/.glose/**),Edit(/.glose/**)"},
			[]string{"bypass", "dangerously", "Read,", "Edit,", "Write"}, true, ""},
		{Spec{Agent: "claude", DryRun: true}, []string{"Bash(glose status*)"}, []string{"Edit(/**)", "Write", "Bash(glose *)"}, true, ""},
		{Spec{Agent: "codex", Model: "gpt-6.1-sol"}, []string{"exec", "-s workspace-write", `approval_policy="never"`, "-m gpt-6.1-sol", "-C " + dir}, []string{"danger"}, true, ""},
		{Spec{Agent: "codex", DryRun: true}, []string{"-s read-only"}, []string{"workspace-write"}, true, ""},
		{Spec{Agent: "opencode", Model: "ollama-cloud/glm-5.3"}, []string{"run", "--standalone", "--auto", "-m ollama-cloud/glm-5.3", "CONSIGNE"}, nil, false,
			`{"action":"shell","effect":"allow","resource":"glose *"}`},
		{Spec{Agent: "opencode", DryRun: true}, []string{"run"}, nil, false, `{"action":"edit","effect":"deny","resource":"*"}`},
	}
	for _, c := range cases {
		c.spec.Dir, c.spec.Prompt, c.spec.Name = dir, "CONSIGNE", "Sol"
		cmd, err := Command(ctx, c.spec)
		if err != nil {
			t.Fatalf("%+v : %v", c.spec, err)
		}
		line := strings.Join(cmd.Args, " ")
		for _, w := range c.want {
			if !strings.Contains(line, w) {
				t.Errorf("%s : %q absent de %q", c.spec.Agent, w, line)
			}
		}
		for _, w := range c.not {
			if strings.Contains(line, w) {
				t.Errorf("%s : %q présent dans %q", c.spec.Agent, w, line)
			}
		}
		if (cmd.Stdin != nil) != c.stdin {
			t.Errorf("%s : entrée standard %v", c.spec.Agent, cmd.Stdin != nil)
		}
		if c.stdin {
			if b, _ := io.ReadAll(cmd.Stdin); string(b) != "CONSIGNE" {
				t.Errorf("%s : consigne %q", c.spec.Agent, b)
			}
		}
		if cmd.Dir != dir {
			t.Errorf("%s : dossier %q", c.spec.Agent, cmd.Dir)
		}
		if v, _ := env(cmd.Env, "GLOSE_AGENT"); v != "Sol" {
			t.Errorf("%s : GLOSE_AGENT %q", c.spec.Agent, v)
		}
		perm, ok := env(cmd.Env, "OPENCODE_CONFIG_CONTENT")
		if c.permission != "" {
			var m struct{ Permissions []map[string]string }
			if !ok || json.Unmarshal([]byte(perm), &m) != nil || !strings.Contains(perm, c.permission) ||
				!strings.Contains(perm, `{"action":"external_directory","effect":"deny","resource":"*"}`) ||
				!strings.Contains(perm, `{"action":"read","effect":"deny","resource":".glose/*"}`) ||
				m.Permissions[0]["action"] != "shell" || m.Permissions[0]["effect"] != "deny" {
				t.Errorf("permissions d'OpenCode : %s", perm)
			}
		}
	}
	if _, err := Command(ctx, Spec{Agent: "gemini"}); err == nil {
		t.Error("agent inconnu accepté")
	}
	if _, err := Command(ctx, Spec{Agent: "codex", Model: "gpt 6; rm"}); err == nil {
		t.Error("modèle invalide accepté")
	}
	if runtime.GOOS == "linux" {
		fakePath(t, "/mnt/d/Programmes/codex")
		if _, err := Command(ctx, Spec{Agent: "codex"}); err == nil || !strings.Contains(err.Error(), "Windows") {
			t.Errorf("agent côté Windows lancé : %v", err)
		}
	}
}

// fakeAgent writes a shell script standing for an agent.
func fakeAgent(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("faux agent en script shell")
	}
	p := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakePath(t, p)
	return p
}

func TestTry(t *testing.T) {
	fakeAgent(t, `echo "args: $*"; echo "agent: $GLOSE_AGENT"; cat; exit 4`)
	r := Try(context.Background(), Spec{Agent: "codex", Dir: t.TempDir(), Name: "Sol", Prompt: TryPrompt(true, []string{"mcp:lexharness"})})
	if r.ExitCode != 4 || r.TimedOut || !strings.Contains(r.Output, "-s read-only") || !strings.Contains(r.Output, "agent: Sol") ||
		!strings.Contains(r.Output, "glose status") || !strings.Contains(r.Output, "mcp:lexharness") {
		t.Fatalf("%+v", r)
	}
}

func TestTryStopsAfterItsDelay(t *testing.T) {
	// The agent launches a child that would outlive it: the whole group
	// is stopped.
	fakeAgent(t, `sleep 30 & sleep 30`)
	prev := TryTimeout
	TryTimeout = 300 * time.Millisecond
	defer func() { TryTimeout = prev }()
	start := time.Now()
	r := Try(context.Background(), Spec{Agent: "codex", Dir: t.TempDir(), Prompt: "x"})
	if !r.TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("%+v en %s", r, time.Since(start))
	}
}

func TestTailKeepsTheEnd(t *testing.T) {
	tl := &tail{max: 5}
	tl.Write([]byte("abc"))
	tl.Write([]byte("defgh"))
	if tl.String() != "defgh" {
		t.Fatal(tl.String())
	}
}

// The launched agent is told that its permissions admit one command at a time
// (seen on 8 November 2026: a composed command refused, a step wasted).
func TestTurnPromptSaysOneCommandAtATime(t *testing.T) {
	if p := TurnPrompt("Sol", "/racine", "", nil); !strings.Contains(p, "sans l'enchaîner") {
		t.Fatalf("consigne : %s", p)
	}
}

func TestTurnPromptRequestsProgress(t *testing.T) {
	p := TurnPrompt("Sol", "racine", "", nil)
	if !strings.Contains(p, "glose progress --text") || !strings.Contains(p, "120 caractères") || !strings.Contains(p, "chaque étape notable") {
		t.Fatal(p)
	}
}
