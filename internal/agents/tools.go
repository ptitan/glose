package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Tools required by a text (P10.4): « mcp:NAME » or « skill:NAME ». The MCP
// servers and the skills do not come from Glose: the agent loads its own
// configuration; Glose only checks that it declares them, at best.

// ToolCheck is the result for one required tool.
type ToolCheck struct {
	Require string `json:"require"`
	Found   bool   `json:"found"`
	Detail  string `json:"detail"`
}

// mcpTimeout bounds « mcp list »: Claude Code checks each server.
var mcpTimeout = 60 * time.Second

// CheckTools checks the tools required for the agent id, in the project at
// root. An agent that cannot be asked gives an error; a tool not found is a
// check with Found false.
func CheckTools(ctx context.Context, id, root string, requires []string) ([]ToolCheck, error) {
	k, ok := KindOf(id)
	if !ok {
		return nil, fmt.Errorf("agent inconnu : %q", id)
	}
	out := []ToolCheck{}
	var mcpOut string
	var mcpErr error
	mcpRead := false
	for _, r := range requires {
		kind, name, _ := strings.Cut(r, ":")
		c := ToolCheck{Require: r}
		switch kind {
		case "mcp":
			if !mcpRead {
				mcpRead = true
				var names []string
				for _, q := range requires {
					if n, ok := strings.CutPrefix(q, "mcp:"); ok {
						names = append(names, n)
					}
				}
				mcpOut, mcpErr = listMCPReady(ctx, k, root, names)
			}
			if mcpErr != nil {
				c.Detail = "liste des serveurs MCP illisible : " + mcpErr.Error()
				break
			}
			if line, ok := findName(mcpOut, name); ok {
				c.Found, c.Detail = true, line
			} else {
				c.Detail = fmt.Sprintf("absent de « %s mcp list »", k.Bin)
			}
		case "skill":
			dirs, user := skillDirs(id, root)
			for _, d := range dirs {
				for _, cand := range candidates(d, name) {
					if _, err := os.Stat(filepath.Join(cand, "SKILL.md")); err == nil {
						c.Found, c.Detail = true, cand
						break
					}
				}
				if c.Found {
					break
				}
			}
			if !c.Found {
				c.Detail = fmt.Sprintf("introuvable aux emplacements connus de %s (ceux du projet, puis %s) ; un skill servi autrement (par un serveur MCP) n'est pas vu ici", k.Name, strings.Join(user, ", "))
			}
		default:
			c.Detail = "forme inconnue (mcp:NOM ou skill:NOM)"
		}
		out = append(out, c)
	}
	return out, nil
}

// mcpRetry spaces the readings of a list not ready yet.
var mcpRetry = 1500 * time.Millisecond

// notReady recognises a list of an agent whose servers are still starting:
// OpenCode, in a directory it has not seen yet, first answers « No MCP
// servers configured », then lists them « pending ».
var notReady = regexp.MustCompile(`(?i)no mcp servers configured|pending|connecting|starting`)

// listMCPReady reads the list of the MCP servers again, within mcpTimeout,
// while some names are missing and the list says it is not ready.
func listMCPReady(ctx context.Context, k Kind, dir string, names []string) (string, error) {
	deadline := time.Now().Add(mcpTimeout)
	for {
		out, err := listMCP(ctx, k, dir)
		if err != nil {
			return out, err
		}
		missing := false
		for _, n := range names {
			if _, ok := findName(out, n); !ok {
				missing = true
			}
		}
		if !missing || !notReady.MatchString(out) || time.Now().Add(mcpRetry).After(deadline) {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return out, nil
		case <-time.After(mcpRetry):
		}
	}
}

// listMCP runs « <agent> mcp list » in dir, the directory of the project:
// the servers of an agent may depend on it (project configuration).
func listMCP(ctx context.Context, k Kind, dir string) (string, error) {
	path, err := exec.LookPath(k.Bin)
	if err != nil {
		return "", fmt.Errorf("%s introuvable", k.Bin)
	}
	if onWindows(path) {
		return "", fmt.Errorf("%s est installé côté Windows", k.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, mcpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "mcp", "list")
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	b, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("pas de réponse en %s", mcpTimeout)
	}
	if err != nil && len(b) == 0 {
		return "", err
	}
	return string(b), nil
}

// findName finds name as a word in out (no format is assumed) and returns
// its line.
func findName(out, name string) (string, bool) {
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_-]|$)`)
	for _, l := range strings.Split(out, "\n") {
		if re.MatchString(l) {
			l = strings.TrimSpace(l)
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			return l, true
		}
	}
	return "", false
}

// candidates are the folders where a skill name may be in a skills
// directory d: d/name, and d/*/name for the skills synchronised by
// Claude Code (skills/synced/<id>/name).
func candidates(d, name string) []string {
	out := []string{filepath.Join(d, name)}
	for _, pat := range []string{filepath.Join(d, "*", name), filepath.Join(d, "*", "*", name)} {
		m, _ := filepath.Glob(pat)
		for _, c := range m {
			// Never a hidden folder: Claude Code keeps removed skills in
			// skills/.trash.
			rel, _ := filepath.Rel(d, c)
			if !strings.HasPrefix(rel, ".") && !strings.Contains(filepath.ToSlash(rel), "/.") {
				out = append(out, c)
			}
		}
	}
	return out
}

// skillDirs are the skills directories of an agent (documentation of each
// agent, 7 November 2026), project first: from root up to the repository;
// user are those of the user alone.
func skillDirs(id, root string) (all, user []string) {
	home, _ := os.UserHomeDir()
	var project []string
	for d := root; d != ""; {
		switch id {
		case "claude":
			project = append(project, filepath.Join(d, ".claude", "skills"))
		case "codex":
			project = append(project, filepath.Join(d, ".codex", "skills"), filepath.Join(d, ".agents", "skills"))
		case "opencode":
			project = append(project, filepath.Join(d, ".opencode", "skills"), filepath.Join(d, ".opencode", "skill"),
				filepath.Join(d, ".claude", "skills"), filepath.Join(d, ".agents", "skills"))
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if home != "" {
		switch id {
		case "claude":
			user = append(user, filepath.Join(home, ".claude", "skills"))
		case "codex":
			codexHome := os.Getenv("CODEX_HOME")
			if codexHome == "" {
				codexHome = filepath.Join(home, ".codex")
			}
			user = append(user, filepath.Join(codexHome, "skills"), filepath.Join(home, ".agents", "skills"))
		case "opencode":
			cfg := os.Getenv("XDG_CONFIG_HOME")
			if cfg == "" {
				cfg = filepath.Join(home, ".config")
			}
			user = append(user, filepath.Join(cfg, "opencode", "skills"), filepath.Join(cfg, "opencode", "skill"),
				filepath.Join(home, ".claude", "skills"), filepath.Join(home, ".agents", "skills"))
		}
	}
	return append(project, user...), user
}
