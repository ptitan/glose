// Package agentrun launches an agent in command line (D-46, lot P10): not
// interactive, in the directory of the project, without a shell, with the
// fewest permissions that let it lead a turn: run glose, read and write the
// files of the text. The options of each agent were checked on claude
// 2.1.318, codex 0.166.0 and opencode 2.2.6 (7 November 2026).
//
// The agent never decides (D-14): the prompt says so, and glose has no
// command for it.
package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ptitan/glose/internal/proc"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/ptitan/glose/internal/agents"
)

// Spec is what to launch.
type Spec struct {
	Agent string // claude, codex, opencode
	Model string // empty: the agent's own default
	Dir   string // directory of the project: the agent works there
	Name  string // name given to glose (GLOSE_AGENT): the short name
	// Prompt is the instruction of the run.
	Prompt string
	// DryRun: the agent may read and run « glose status » or « glose
	// list », nothing else (button « Essayer », P10.4).
	DryRun bool
	// MCP are the servers required by the text (« mcp:NAME »): Claude Code
	// is told to allow their tools.
	MCP []string
	// Env is added to the environment (tests, orchestration P10.6).
	Env []string
}

// ErrWindows means that the agent found is a program of Windows seen from
// WSL: Glose does not launch it.
var ErrWindows = errors.New("installé côté Windows : Glose ne le lance pas depuis WSL")

// lookPath finds the program of an agent; tests replace it.
var lookPath = exec.LookPath

var windowsPath = regexp.MustCompile(`^/mnt/[a-z]/`)

// Command builds the command that runs the agent. Its standard input
// carries the prompt when the agent reads it there (Stdin is set).
func Command(ctx context.Context, s Spec) (*exec.Cmd, error) {
	k, ok := agents.KindOf(s.Agent)
	if !ok {
		return nil, fmt.Errorf("agent inconnu : %q", s.Agent)
	}
	path, err := lookPath(k.Bin)
	if err != nil {
		return nil, fmt.Errorf("%s introuvable dans le PATH (glose set)", k.Name)
	}
	if runtime.GOOS == "linux" && windowsPath.MatchString(path) {
		return nil, fmt.Errorf("%s : %w", k.Name, ErrWindows)
	}
	if s.Model != "" {
		if err := agents.CheckModel(s.Model); err != nil {
			return nil, err
		}
	}
	var args, env []string
	stdin := false
	switch s.Agent {
	case "claude":
		// « dontAsk »: what is not allowed beforehand is refused, never
		// asked (nobody would answer). Files are allowed under the working
		// directory only (« /** » anchors there for rules given on the
		// command line); the prompt goes through the standard input, since
		// --allowedTools takes several values and would swallow it.
		allowed := []string{"Bash(glose *)", "Read(/**)", "Edit(/**)"}
		if s.DryRun {
			allowed = []string{"Bash(glose status*)", "Bash(glose list*)", "Read(/**)"}
		}
		for _, m := range s.MCP {
			// A server of the user, and a connector of claude.ai.
			allowed = append(allowed, "mcp__"+m, "mcp__claude_ai_"+m)
		}
		// .glose holds the token of the server, the author's key to decide
		// (D-14): never read nor written by the agent's tools.
		args = []string{"-p", "--output-format", "text", "--permission-mode", "dontAsk", "--allowedTools", strings.Join(allowed, ","),
			"--disallowedTools", "Read(/.glose/**),Edit(/.glose/**)"}
		if s.Model != "" {
			args = append(args, "--model", s.Model)
		}
		stdin = true
	case "codex":
		// The sandbox lets its commands write in the project only, without
		// network; it never asks. It does not restrict which programs run
		// inside the sandbox (D-46, limit written in DECISIONS).
		sandbox := "workspace-write"
		if s.DryRun {
			sandbox = "read-only"
		}
		args = []string{"exec", "--skip-git-repo-check", "-C", s.Dir, "-s", sandbox, "-c", `approval_policy="never"`}
		if s.Model != "" {
			args = append(args, "-m", s.Model)
		}
		args = append(args, "-")
		stdin = true
	case "opencode":
		// OpenCode 2: ordered rules, the last one that matches wins
		// (opencode.ai/docs/permissions). Every command is refused but
		// glose, every file outside the project, questions and subagents
		// (which have permissions of their own). The rules come through
		// OPENCODE_CONFIG_CONTENT, read by a private server (--standalone):
		// the background service would not see them. --auto approves what
		// is not refused.
		rule := func(action, resource, effect string) map[string]string {
			return map[string]string{"action": action, "resource": resource, "effect": effect}
		}
		rules := []map[string]string{
			rule("shell", "*", "deny"), rule("shell", "glose *", "allow"),
			rule("external_directory", "*", "deny"),
			rule("question", "*", "deny"), rule("subagent", "*", "deny"),
		}
		if s.DryRun {
			rules = []map[string]string{
				rule("shell", "*", "deny"), rule("shell", "glose status *", "allow"), rule("shell", "glose list *", "allow"),
				rule("external_directory", "*", "deny"), rule("edit", "*", "deny"),
				rule("question", "*", "deny"), rule("subagent", "*", "deny"),
			}
		}
		// .glose holds the token of the server (see claude above).
		rules = append(rules, rule("read", ".glose/*", "deny"), rule("edit", ".glose/*", "deny"))
		b, _ := json.Marshal(map[string]any{"permissions": rules})
		env = append(env, "OPENCODE_CONFIG_CONTENT="+string(b))
		args = []string{"run", "--standalone", "--auto"}
		if s.Model != "" {
			args = append(args, "-m", s.Model)
		}
		args = append(args, s.Prompt)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = s.Dir
	cmd.Env = append(append(environ(), env...), s.Env...)
	if s.Name != "" {
		cmd.Env = append(cmd.Env, "GLOSE_AGENT="+s.Name)
	}
	if s.Model != "" {
		cmd.Env = append(cmd.Env, "GLOSE_MODEL="+s.Model) // signed on its messages (P10.11)
	}
	if stdin {
		cmd.Stdin = strings.NewReader(s.Prompt)
	}
	return cmd, nil
}

// environ is the environment of the agent: the one of Glose, with the
// directory of this glose first in the PATH, so that the agent runs the
// same glose.
func environ() []string {
	env := os.Environ()
	self, err := os.Executable()
	if err != nil {
		return env
	}
	dir := filepath.Dir(self)
	for i, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && strings.EqualFold(k, "PATH") {
			env[i] = k + "=" + dir + string(os.PathListSeparator) + v
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// SetLookPath replaces the search of the agents' programs, for the tests
// of other packages (a fake agent); it returns the function that restores
// it.
func SetLookPath(f func(string) (string, error)) (restore func()) {
	prev := lookPath
	lookPath = f
	return func() { lookPath = prev }
}

// Group puts cmd in a process group of its own, as for a launched agent
// (tests of other packages).
func Group(cmd *exec.Cmd) { ownGroup(cmd) }

// ProcessIdentity returns what distinguishes the process pid from a later
// process with the same number (tests of other packages).
func ProcessIdentity(pid int) string { return proc.Identity(pid) }

// Alive reports whether the process pid runs (tests of other packages).
func Alive(pid int) bool { return proc.Alive(pid) }
