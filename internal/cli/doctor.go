package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/server"
	"github.com/ptitan/glose/internal/store"
)

// doctorFinding is one point checked by « glose doctor ». Fix is the command
// or the action that corrects a problem (doctor reports, it never repairs).
type doctorFinding struct {
	Section string `json:"section"` // install or project
	Level   string `json:"level"`   // ok, warning, error
	Subject string `json:"subject"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// externalCSS finds what a project stylesheet would load from outside: the
// interface forbids it (D-07), so the browser ignores it.
var externalCSS = regexp.MustCompile(`(?i)(?:@import\s+(?:url\(\s*)?|url\(\s*)["']?\s*(?:https?:)?//`)

func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	var asJSON bool
	path, ok := parse("doctor", args, stderr, func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "") })
	if !ok {
		return ExitUsage
	}
	out := doctor(path)
	bad := false
	for _, f := range out {
		if f.Level == "error" {
			bad = true
		}
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]any{"ok": !bad, "findings": out})
	} else {
		section := ""
		for _, f := range out {
			if f.Section != section {
				section = f.Section
				fmt.Fprintln(stdout, map[string]string{"install": "Installation", "project": "Projet"}[section])
			}
			mark := map[string]string{"ok": "✓", "warning": "!", "error": "✗"}[f.Level]
			fmt.Fprintf(stdout, "  %s %s : %s\n", mark, f.Subject, f.Message)
			if f.Fix != "" {
				fmt.Fprintf(stdout, "      → %s\n", f.Fix)
			}
		}
		if bad {
			fmt.Fprintln(stdout, "Des points indispensables manquent : voir les corrections (→).")
		} else {
			fmt.Fprintln(stdout, "Tout l'indispensable est en place. Pour les marques et le journal : glose check.")
		}
	}
	if bad {
		return ExitProblem
	}
	return ExitOK
}

// doctor checks the installation, then the project at path.
func doctor(path string) []doctorFinding {
	var out []doctorFinding
	add := func(section, level, subject, msg, fix string) {
		out = append(out, doctorFinding{section, level, subject, msg, fix})
	}
	inst := func(level, subject, msg, fix string) { add("install", level, subject, msg, fix) }
	proj := func(level, subject, msg, fix string) { add("project", level, subject, msg, fix) }

	// Installation.
	self, _ := os.Executable()
	inst("ok", "glose", fmt.Sprintf("version %s (%s)", Version, self), "")

	p, perr := project.Load(path)
	pandoc := "pandoc"
	if perr == nil {
		pandoc = p.Pandoc
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	v, err := render.CheckPandoc(ctx, pandoc)
	cancel()
	pandocOK := err == nil
	switch {
	case err == nil:
		inst("ok", "pandoc", v, "")
	case v != "": // found, too old
		inst("error", "pandoc", err.Error(), pandocFix(pandoc))
	default:
		inst("error", "pandoc", fmt.Sprintf("introuvable (%s) : indispensable pour afficher le texte", pandoc), pandocFix(pandoc))
	}

	if g, err := exec.LookPath("git"); err == nil {
		inst("ok", "git", g, "")
	} else {
		inst("warning", "git", "introuvable : utile pour suivre les fichiers à côté de Glose, pas indispensable", gitFix())
	}

	if name, where, err := server.BrowserOpener(); err == nil {
		inst("ok", "navigateur", fmt.Sprintf("ouvert par %s (%s)", name, where), "")
	} else {
		fix := "installez xdg-utils (sudo apt install xdg-utils), ou ouvrez vous-même l'adresse affichée par glose open"
		if server.UnderWSL() {
			fix = "activez l'interopérabilité de WSL ([interop] enabled=true et appendWindowsPath=true dans /etc/wsl.conf, puis wsl --shutdown), ou installez wslu (sudo apt install wslu) ; à défaut, ouvrez vous-même l'adresse affichée par glose open"
		}
		inst("warning", "navigateur", err.Error()+" : glose open affichera l'adresse sans ouvrir le navigateur", fix)
	}

	// Agents (P10.3): a warning, Glose works without them.
	if c, err := agents.Load(); err != nil {
		inst("warning", "agents", err.Error(), "corrigez ou supprimez ce fichier, puis lancez glose set")
	} else if !c.Configured() {
		inst("warning", "agents", "aucun agent configuré : Glose ne sait pas quel agent lancer ni nommer", "lancez glose set")
	} else {
		a, _ := c.Alias(c.Default)
		inst("ok", "agents", fmt.Sprintf("par défaut : %s (%s)", a.Name, aliasDesc(a)), "")
	}

	// Project.
	if perr != nil {
		fix := "corrigez " + project.ProfileName + ", ou lancez glose doctor sur le dossier du texte (glose doctor DOSSIER)"
		if strings.Contains(perr.Error(), "aucun fichier Markdown") {
			fix = "lancez glose doctor sur le dossier ou le fichier .md à relire (glose doctor DOSSIER)"
		}
		proj("error", "projet", perr.Error(), fix)
		return out
	}
	if p.Profiled {
		proj("ok", "profil", fmt.Sprintf("%s lu : %s, %d fichier%s", project.ProfileName, p.Root, len(p.Files), plural(len(p.Files))), "")
	} else {
		proj("ok", "profil", fmt.Sprintf("pas de %s : %d fichier%s .md de %s", project.ProfileName, len(p.Files), plural(len(p.Files)), p.Root),
			"")
	}
	for _, w := range p.MetaWarnings {
		proj("warning", "métadonnées", w, "corrigez la clé project de "+project.ProfileName+" (glose help profile), ou passez par glose project")
	}

	if p.CSS != "" {
		rel, _ := filepath.Rel(p.Root, p.CSS)
		b, err := os.ReadFile(p.CSS)
		switch {
		case err != nil:
			proj("error", "feuille de style", err.Error(), "vérifiez le champ css de "+project.ProfileName)
		case !utf8.Valid(b):
			proj("warning", "feuille de style", rel+" n'est pas en UTF-8 valide", "réenregistrez "+rel+" en UTF-8")
		case externalCSS.Match(b):
			proj("warning", "feuille de style", rel+" charge une ressource extérieure (@import ou url() vers un autre site) : l'interface la refuse (D-07)",
				"copiez la ressource dans le projet et donnez un chemin relatif")
		default:
			proj("ok", "feuille de style", rel, "")
		}
	}

	// The filter and the files: a real rendering, as the interface does it.
	if pandocOK {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		d, rerr := render.New(p).Render(ctx)
		cancel()
		filter := "sans filtre"
		if p.Filter != "" {
			rel, _ := filepath.Rel(p.Root, p.Filter)
			filter = "avec le filtre " + rel
		}
		if rerr != nil {
			fix := "corrigez le texte qui fait échouer pandoc"
			if p.Filter != "" {
				null := "/dev/null"
				if runtime.GOOS == "windows" {
					null = "NUL"
				}
				fix = "corrigez le filtre (essai : " + gloseCommand("glose render", p.Source) + " > " + null + "), ou retirez-le de " + project.ProfileName
			}
			proj("error", "rendu", fmt.Sprintf("échec (%s) : %v", filter, rerr), fix)
		} else {
			failed := 0
			for _, f := range d.Files {
				if f.Error != "" {
					failed++
					fix := "corrigez le fichier"
					if strings.Contains(f.Error, "UTF-8") {
						fix = "réenregistrez " + f.Path + " en UTF-8"
					}
					proj("error", f.Path, "ne peut pas être affiché : "+f.Error, fix)
				}
			}
			if failed == 0 {
				proj("ok", "rendu", fmt.Sprintf("%d fichier%s rendu%s %s en %d ms", len(d.Files), plural(len(d.Files)), plural(len(d.Files)), filter, d.Millis), "")
			}
		}
	}

	state := p.State()
	if err := store.Writable(state); err != nil {
		fix := "donnez-vous le droit d'écrire dans le dossier du projet"
		if runtime.GOOS != "windows" {
			fix = "chmod u+w " + quoteArg(nearestExisting(state))
		}
		proj("error", project.StateDir, "Glose ne peut pas y écrire (journal, serveur) : "+err.Error(), fix)
	} else if _, err := os.Stat(state); err == nil {
		proj("ok", project.StateDir, "accessible en écriture", "")
	} else {
		proj("ok", project.StateDir, "sera créé au premier usage (le dossier du projet est accessible en écriture)", "")
	}

	// P6.9 : un dossier Windows vu depuis WSL (/mnt/c) sans l'option
	// « metadata » ne garde pas les droits : l'état de Glose y est lisible
	// par tous les comptes de la machine.
	if !store.KeepsPermissions(state) {
		proj("warning", "droits", "ce système de fichiers ne garde pas les droits des fichiers (dossier Windows vu depuis WSL ?) : journal et brouillons de .glose y sont lisibles par tous les comptes de la machine",
			"placez le projet dans le système de fichiers de WSL, ou montez C: avec l'option metadata ([automount] options = \"metadata\" dans /etc/wsl.conf, puis wsl --shutdown)")
	}

	if st, err := server.Running(state); err == nil {
		switch {
		case st.Build != server.Build():
			proj("warning", "serveur", fmt.Sprintf("en cours (%s), mais lancé par une autre version de glose", st.Base()),
				gloseCommand("glose open", p.Source)+" le relance avec cette version")
		case st.Source != p.Source:
			proj("ok", "serveur", fmt.Sprintf("en cours pour %s : %s", st.Source, st.Base()), "")
		default:
			proj("ok", "serveur", "en cours : "+st.Base(), "")
		}
	} else {
		proj("ok", "serveur", "arrêté", "")
	}
	if !inGit(p.Root) {
		proj("warning", "git", "le projet n'est pas suivi par Git : conseillé, pour revenir à une version antérieure hors de Glose",
			"git init (dans "+p.Root+")")
	} else {
		proj("ok", "git", "projet suivi par Git", "")
	}
	return out
}

func pandocFix(path string) string {
	if path != "pandoc" {
		return fmt.Sprintf("installez pandoc 3.1 ou plus récent, ou corrigez le champ pandoc de %s (%s)", project.ProfileName, path)
	}
	switch runtime.GOOS {
	case "windows":
		return "winget install --id JohnMacFarlane.Pandoc"
	case "darwin":
		return "brew install pandoc"
	default:
		return "installez pandoc 3.1 ou plus récent depuis https://github.com/jgm/pandoc/releases (paquet .deb : sudo apt install ./pandoc-*.deb) ; celui des dépôts peut être trop ancien"
	}
}

func gitFix() string {
	switch runtime.GOOS {
	case "windows":
		return "winget install --id Git.Git"
	case "darwin":
		return "xcode-select --install"
	default:
		return "sudo apt install git"
	}
}

// inGit tells whether dir or one of its parents holds a .git directory or
// file (worktree), without running git.
func inGit(dir string) bool {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func nearestExisting(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// gloseCommand gives a glose command on path, to be copied into a terminal.
// Under Windows, no quoting protects a path from both cmd.exe (« %VAR% ») and
// PowerShell (« $VAR », « ` »): such a path is not written in the command,
// which is then run from the directory (every glose command takes the
// current directory by default). A single file stays named, alone.
func gloseCommand(cmd, path string) string {
	if runtime.GOOS == "windows" && strings.ContainsAny(path, "%$`") {
		dir, arg := path, ""
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			dir, arg = filepath.Dir(path), " "+quoteArg(filepath.Base(path))
			if strings.ContainsAny(arg, "%$`") {
				arg = ""
			}
		}
		return cmd + arg + " (lancée dans le dossier « " + dir + " »)"
	}
	return cmd + " " + quoteArg(path)
}

// quoteArg quotes a path for the shell of the user, only when needed:
// single quotes for a POSIX shell (nothing is expanded inside), double quotes
// under Windows (a path cannot hold one).
func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t\n'\"$&;|<>()`\\*?[]{}~#!%") {
		return s
	}
	if runtime.GOOS == "windows" {
		// A backslash just before the closing quote would escape it.
		return `"` + s + strings.Repeat(`\`, len(s)-len(strings.TrimRight(s, `\`))) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
