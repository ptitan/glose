package agentrun

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ptitan/glose/internal/agents"
)

// AccessWarnings is always shown before a task launch. It describes controls
// that the verified installed CLI cannot enforce, including default denials.
func AccessWarnings(agent string, a agents.Access) []string {
	var out []string
	if a.ExternalDirs == "all" {
		out = append(out, "Tous les dossiers sont accessibles : accès déconseillé.")
	}
	switch agent {
	case "codex":
		out = append(out, "Codex : lecture externe, web, scripts, sous-agents et sélection MCP non applicables (contrôles fins non vérifiés sur 0.167.0). Le bac à sable permet la lecture externe et tout programme local ; le réseau et les outils dépendent aussi de sa configuration.")
		if a.ExternalDirs == "list" && !a.ExternalWrite {
			out = append(out, "Codex : liste de lecture seule non applicable ; aucun --add-dir accordé.")
		}
	case "claude":
		out = append(out, "Claude Code : l'isolement des lectures externes et la sélection exclusive des MCP ne sont pas garantis ; les réglages personnels restent chargés et peuvent élargir les permissions, y compris celles des scripts.")
	case "opencode":
		out = append(out, "OpenCode : les règles d'un sous-agent ou d'un agent personnalisé peuvent compléter les permissions ; sélection exclusive des MCP non applicable.")
	}
	if a.Scripts == "python" || a.Scripts == "shell" {
		out = append(out, "Les scripts autorisés s'exécutent avec les droits du système ; les cases dossiers et web ne les isolent pas.")
	}
	return out
}

// applyAccess only runs for an explicit task; handovers without a task retain
// their P10 command. A dry run retains read-only controls and never runs the task.
func applyAccess(s Spec, args, env []string) ([]string, []string, error) {
	if s.Access == nil {
		return args, env, nil
	}
	outsideWorkdir := false
	if s.Root != "" {
		rel, err := filepath.Rel(s.Root, s.Dir)
		outsideWorkdir = err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	a := *s.Access
	if err := a.Check(false); err != nil {
		return nil, nil, err
	}
	for _, d := range a.Dirs {
		if strings.ContainsAny(d, "*,?()\n\r") {
			return nil, nil, fmt.Errorf("dossier incompatible avec les règles de permission : %q", d)
		}
	}
	switch s.Agent {
	case "codex":
		if (a.ExternalWrite || (s.Root != "" && s.Root != s.Dir)) && !s.DryRun {
			// Insert before stdin sentinel.
			args = args[:len(args)-1]
			if a.ExternalWrite {
				for _, d := range a.Dirs {
					args = append(args, "--add-dir", d)
				}
			}
			if s.Root != "" && s.Root != s.Dir {
				args = append(args, "--add-dir", s.Root)
			}
			args = append(args, "-")
		}
	case "claude":
		// Permission specifiers with // are absolute (single / anchors in CWD).
		allowed := []string{"Bash(glose *)", "Read(/**)", "Edit(/**)"}
		if outsideWorkdir {
			allowed = []string{"Bash(glose *)", "Read(/**)"}
		}
		denied := []string{"Read(/.glose/**)", "Edit(/.glose/**)"}
		if s.DryRun {
			allowed = []string{"Bash(glose status*)", "Bash(glose list*)", "Read(/**)"}
			denied = append(denied, "Edit", "Agent", "Bash(python *)", "Bash(python3 *)")
		}
		if !s.DryRun {
			if a.Scripts == "shell" {
				allowed = append(allowed, "Bash")
			} else if a.Scripts == "python" {
				allowed = append(allowed, "Bash(python *)", "Bash(python3 *)")
			}
			if a.Subagents {
				allowed = append(allowed, "Agent")
			} else {
				denied = append(denied, "Agent")
			}
		}
		if a.Web {
			allowed = append(allowed, "WebSearch", "WebFetch")
		} else {
			denied = append(denied, "WebSearch", "WebFetch")
		}
		if s.Root != "" && s.Root != s.Dir {
			pattern := "/" + strings.TrimSuffix(permissionPath(s.Root), "/") + "/**"
			allowed = append(allowed, "Read("+pattern+")")
			if !s.DryRun {
				allowed = append(allowed, "Edit("+pattern+")")
			}
		}
		if a.ExternalDirs == "all" {
			allowed = append(allowed, "Read(//**)")
		}
		for _, d := range a.Dirs {
			pattern := "/" + permissionPath(d)
			if !strings.HasSuffix(pattern, "/") {
				pattern += "/"
			}
			pattern += "**"
			allowed = append(allowed, "Read("+pattern+")")
			if a.ExternalWrite && !s.DryRun {
				allowed = append(allowed, "Edit("+pattern+")")
			}
		}
		for _, m := range a.MCP {
			allowed = append(allowed, "mcp__"+m, "mcp__claude_ai_"+m)
		}
		if s.Root != "" {
			pattern := "/" + strings.TrimSuffix(permissionPath(s.Root), "/") + "/.glose/**"
			denied = append(denied, "Read("+pattern+")", "Edit("+pattern+")")
		}
		for i := range args {
			if args[i] == "--allowedTools" {
				args[i+1] = strings.Join(allowed, ",")
			}
			if args[i] == "--disallowedTools" {
				args[i+1] = strings.Join(denied, ",")
			}
		}
	case "opencode":
		rule := func(action, resource, effect string) map[string]string {
			return map[string]string{"action": action, "resource": resource, "effect": effect}
		}
		// Begin with the tested P10 defaults, then widen the chosen cases.
		var conf struct {
			Permissions []map[string]string `json:"permissions"`
		}
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_CONTENT="); ok {
				if err := json.Unmarshal([]byte(v), &conf); err != nil {
					return nil, nil, err
				}
			}
		}
		add := func(act, res, eff string) { conf.Permissions = append(conf.Permissions, rule(act, res, eff)) }
		effect := func(b bool) string {
			if b {
				return "allow"
			}
			return "deny"
		}
		add("webfetch", "*", effect(a.Web))
		add("websearch", "*", effect(a.Web))
		add("subagent", "*", effect(a.Subagents && !s.DryRun))
		if !s.DryRun {
			if a.Scripts == "shell" {
				add("shell", "*", "allow")
			} else if a.Scripts == "python" {
				add("shell", "python *", "allow")
				add("shell", "python3 *", "allow")
			}
		}
		if outsideWorkdir {
			add("edit", "*", "deny")
			if rel, err := filepath.Rel(s.Dir, s.Root); err == nil {
				add("edit", filepath.ToSlash(rel)+"/*", "allow")
			}
		}
		// External edits default to deny; project paths are location-relative.
		add("edit", "/*", "deny")
		add("edit", "?:/*", "deny")
		if a.ExternalDirs == "all" {
			add("external_directory", "*", "allow")
			add("read", "/*", "allow")
		}
		for _, d := range a.Dirs {
			pattern := strings.TrimSuffix(filepath.ToSlash(d), "/") + "/*"
			add("external_directory", pattern, "allow")
			add("read", pattern, "allow")
			if !s.DryRun {
				add("edit", pattern, effect(a.ExternalWrite))
			}
		}
		for _, m := range a.MCP {
			add(m+"_*", "*", "allow")
		}
		if s.Root != "" && s.Root != s.Dir {
			p := strings.TrimSuffix(filepath.ToSlash(s.Root), "/") + "/*"
			add("external_directory", p, "allow")
			add("read", p, "allow")
			add("edit", p, "allow")
		}
		if s.DryRun {
			add("edit", "*", "deny")
		}
		// Keep the decision token protected even after broad grants.
		add("read", ".glose/*", "deny")
		add("edit", ".glose/*", "deny")
		if s.Root != "" {
			p := strings.TrimSuffix(filepath.ToSlash(s.Root), "/") + "/.glose/*"
			add("read", p, "deny")
			add("edit", p, "deny")
		}
		b, _ := json.Marshal(conf)
		env = []string{"OPENCODE_CONFIG_CONTENT=" + string(b)}
	}
	return args, env, nil
}

// Claude normalizes Windows drive paths to /c/... before matching.
func permissionPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if len(p) > 2 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}
