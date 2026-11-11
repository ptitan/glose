package agentrun

import (
	"context"
	"github.com/ptitan/glose/internal/agents"
	"strings"
	"testing"
)

func TestTaskAccessCommands(t *testing.T) {
	restore := SetLookPath(func(s string) (string, error) { return s, nil })
	defer restore()
	for _, agent := range []string{"claude", "codex", "opencode"} {
		for _, tc := range []struct {
			name   string
			access agents.Access
			want   string
		}{
			{"defaults", agents.Access{}, ""},
			{"list", agents.Access{ExternalDirs: "list", Dirs: []string{"/reference"}}, ""},
			{"write", agents.Access{ExternalDirs: "list", Dirs: []string{"/reference"}, ExternalWrite: true}, ""},
			{"all", agents.Access{ExternalDirs: "all"}, ""},
			{"web", agents.Access{Web: true}, ""},
			{"python", agents.Access{Scripts: "python"}, ""},
			{"shell", agents.Access{Scripts: "shell"}, ""},
			{"subagents", agents.Access{Subagents: true}, ""},
			{"mcp", agents.Access{MCP: []string{"lexharness"}}, ""},
		} {
			t.Run(agent+"/"+tc.name, func(t *testing.T) {
				cmd, err := Command(context.Background(), Spec{Agent: agent, Dir: "/project", Access: &tc.access})
				if err != nil {
					t.Fatal(err)
				}
				all := strings.Join(append(cmd.Args, cmd.Env...), " ")
				switch agent {
				case "claude":
					want := map[string]string{"list": "Read(//reference/**)", "write": "Edit(//reference/**)", "web": "WebSearch,WebFetch", "python": "Bash(python *)", "shell": ",Bash", "subagents": ",Agent", "mcp": "mcp__lexharness", "all": "Read(//**)"}
					if s := want[tc.name]; s != "" && !strings.Contains(all, s) {
						t.Fatalf("absent %q : %s", s, all)
					}
				case "codex":
					if strings.Contains(all, "--add-dir") != (tc.name == "write") {
						t.Fatal(cmd.Args)
					}
					if len(AccessWarnings(agent, tc.access)) == 0 {
						t.Fatal("limites absentes")
					}
				case "opencode":
					want := map[string]string{"list": "/reference/*", "write": "/reference/*", "web": `"action":"webfetch","effect":"allow"`, "python": "python *", "shell": `"action":"shell","effect":"allow","resource":"*"`, "subagents": `"action":"subagent","effect":"allow"`, "mcp": "lexharness_*"}
					if s := want[tc.name]; s != "" && !strings.Contains(all, s) {
						t.Fatalf("absent %q : %s", s, all)
					}
				}
			})
		}
	}
}

func TestTaskDryRunNeverGrantsWriteOrScripts(t *testing.T) {
	restore := SetLookPath(func(s string) (string, error) { return s, nil })
	defer restore()
	a := agents.Access{ExternalDirs: "list", Dirs: []string{"/reference"}, ExternalWrite: true, Scripts: "shell", Subagents: true}
	for _, agent := range []string{"claude", "codex", "opencode"} {
		cmd, err := Command(context.Background(), Spec{Agent: agent, DryRun: true, Access: &a})
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(cmd.Args, " ")
		if strings.Contains(args, "--add-dir") || (agent == "claude" && strings.Contains(strings.Split(args, "--disallowedTools")[0], "Edit(//reference/**)")) {
			t.Fatal(args)
		}
	}
}

func TestAllExternalReadsNeverWidenListedWrites(t *testing.T) {
	restore := SetLookPath(func(s string) (string, error) { return s, nil })
	defer restore()
	a := agents.MergeAccess(agents.Access{ExternalDirs: "list", Dirs: []string{"/reference"}, ExternalWrite: true}, agents.Access{ExternalDirs: "all"})
	cmd, err := Command(context.Background(), Spec{Agent: "claude", Access: &a})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "Read(//**)") || !strings.Contains(args, "Edit(//reference/**)") || strings.Contains(args, "Edit(//**)") {
		t.Fatal(args)
	}
}

func TestWindowsPermissionPaths(t *testing.T) {
	if got := permissionPath(`C:\Users\a\references`); got != "/c/Users/a/references" {
		t.Fatal(got)
	}
}

func TestOutsideWorkdirDoesNotGrantClaudeEdit(t *testing.T) {
	restore := SetLookPath(func(s string) (string, error) { return s, nil })
	defer restore()
	a := agents.Access{}
	cmd, err := Command(context.Background(), Spec{Agent: "claude", Root: "/project", Dir: "/tools", Access: &a})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.Join(cmd.Args, " "), "--disallowedTools")[0]
	if strings.Contains(args, "Edit(/**)") || !strings.Contains(args, "Edit(//project/**)") {
		t.Fatal(args)
	}
}
