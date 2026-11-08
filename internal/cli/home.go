package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/home"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/server"
	"github.com/ptitan/glose/internal/store"
)

// register records a project opened or prepared in the registry (D-50). A
// failure is said, and never stops anything: the registry is not normative.
func register(root string, stderr io.Writer) {
	if err := home.Register(root); err != nil {
		fmt.Fprintf(stderr, "glose : projet non inscrit à l'accueil (%v)\n", err)
	}
}

// refusedToAgent says that a command is the author's, under an agent launched
// by Glose (D-50): it has no business with the other projects.
func refusedToAgent(name string, stderr io.Writer) bool {
	if os.Getenv(agentEnv) == "" {
		return false
	}
	fmt.Fprintf(stderr, "glose : %s est réservé à l'auteur (%s est posé) : un agent lancé par Glose travaille sur son projet, sans voir les autres.\n", name, agentEnv)
	return true
}

// cmdHome shows the registry of the projects of the author.
func cmdHome(args []string, stdout, stderr io.Writer) int {
	var forget string
	var asJSON, list, noBrowser bool
	path, ok := parse("home", args, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&forget, "forget", "", "")
		fs.BoolVar(&asJSON, "json", false, "")
		fs.BoolVar(&list, "list", false, "")
		fs.BoolVar(&noBrowser, "no-browser", false, "")
	})
	if !ok {
		return ExitUsage
	}
	if refusedToAgent("glose home", stderr) {
		return ExitProblem
	}
	if forget != "" {
		e, err := home.Forget(forget)
		if errors.Is(err, home.ErrNotRegistered) {
			fmt.Fprintf(stderr, "glose : %v\n", err)
			return ExitProblem
		}
		if err != nil {
			return failure(stderr, err)
		}
		fmt.Fprintf(stdout, "Retiré du registre : %s (le projet n'est pas touché).\n", e.Root)
		return ExitOK
	}
	if !asJSON && !list {
		return openHome(path, noBrowser, stdout, stderr)
	}
	entries, err := home.Load()
	if err != nil {
		return failure(stderr, err)
	}
	projects := home.Summaries(entries, runningServer)
	totals := home.Compute(projects, time.Now())
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(map[string]any{"totals": totals, "projects": projects})
		return ExitOK
	}
	if len(projects) == 0 {
		fmt.Fprintln(stdout, "Aucun projet inscrit : glose open ou glose init les inscrit, s'ils ont un profil glose.json.")
		return ExitOK
	}
	fmt.Fprintf(stdout, "%d projet%s · %d annotation%s ouverte%s · %d en révision · %d à échéance sous 7 jours",
		totals.Projects, plural(totals.Projects), totals.Annotations, plural(totals.Annotations), plural(totals.Annotations), totals.InReview, totals.DueSoon)
	if totals.Overdue > 0 {
		fmt.Fprintf(stdout, " · %d échéance%s dépassée%s", totals.Overdue, plural(totals.Overdue), plural(totals.Overdue))
	}
	fmt.Fprintln(stdout)
	for _, s := range projects {
		printSummary(stdout, s)
	}
	return ExitOK
}

// openHome opens the page of the overview (P11.5): on the server of the project
// of path if one runs, else on a server of its own, which stops when the page
// is closed.
func openHome(path string, noBrowser bool, stdout, stderr io.Writer) int {
	open := func(url string) {
		fmt.Fprintf(stdout, "Vos projets : %s\n", url)
		if !noBrowser && os.Getenv("GLOSE_NO_BROWSER") == "" {
			if err := server.OpenBrowser(url); err != nil {
				fmt.Fprintf(stderr, "glose : navigateur non ouvert (%v) ; copiez l'adresse.\n", err)
			}
		}
	}
	if dir, err := project.StateOf(path); err == nil {
		if st, err := server.Running(dir); err == nil {
			open(st.Base() + "/home#t=" + st.Token)
			return ExitOK
		}
	}
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() { <-sig; close(stop) }()
	err := server.ServeSettings("/home", func(url string) {
		open(url)
		fmt.Fprintln(stdout, "Ce serveur s'arrête quelques minutes après la fermeture de la page (Ctrl+C pour l'arrêter tout de suite).")
	}, stop)
	if err != nil {
		return failure(stderr, err)
	}
	return ExitOK
}

// runningServer gives the address of the server of a project, never its token
// (D-14): the address with the token is the key of the author to decide.
func runningServer(stateDir string) (*home.Server, bool) {
	st, err := server.Running(stateDir)
	if err != nil {
		return nil, false
	}
	return &home.Server{URL: st.Base(), PID: st.PID}, true
}

var statusNames = map[string]string{project.StatusDraft: "brouillon", project.StatusActive: "en cours", project.StatusReview: "en révision",
	project.StatusDone: "terminé", project.StatusArchived: "archivé"}

// printSummary writes one project on a few lines.
func printSummary(w io.Writer, s home.Summary) {
	fmt.Fprintf(w, "\n%s  [%s]\n", s.Title, s.ID)
	var parts []string
	if s.Meta.Type != "" {
		parts = append(parts, s.Meta.Type)
	}
	if n := statusNames[s.Meta.Status]; n != "" {
		parts = append(parts, n)
	}
	switch s.Hand {
	case review.Author:
		parts = append(parts, fmt.Sprintf("tour %d, à vous", s.Round))
	case review.Agent:
		who := "l'agent"
		if s.Agent != nil {
			who = s.Agent.Name
		}
		parts = append(parts, fmt.Sprintf("tour %d, à %s", s.Round, who))
	}
	if s.State == home.StateOK || s.Hand != "" {
		parts = append(parts, fmt.Sprintf("%d annotation%s", s.Annotations, plural(s.Annotations)))
	}
	if s.Issues > 0 {
		parts = append(parts, fmt.Sprintf("%d anomalie%s", s.Issues, plural(s.Issues)))
	}
	if pr := s.Progress; pr != nil {
		text := fmt.Sprintf("%d %% relu", 100*pr.Read/pr.Total)
		if pr.FilesSeen < pr.Files {
			text = fmt.Sprintf("%d/%d sections relues (%d fichier%s affiché%s sur %d)", pr.Read, pr.Total, pr.FilesSeen, plural(pr.FilesSeen), plural(pr.FilesSeen), pr.Files)
		}
		parts = append(parts, text)
	}
	if s.Meta.Deadline != "" {
		parts = append(parts, "échéance "+s.Meta.Deadline)
	}
	if s.Server != nil {
		parts = append(parts, "serveur en marche")
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "  %s\n", strings.Join(parts, " · "))
	}
	if s.Meta.NextAction != "" {
		fmt.Fprintf(w, "  Prochaine action : %s\n", s.Meta.NextAction)
	}
	if s.State != home.StateOK {
		fmt.Fprintf(w, "  ! %s\n", s.Message)
	}
	for _, m := range s.Warnings {
		fmt.Fprintf(w, "  ! %s\n", m)
	}
	for _, pb := range s.Problems {
		fmt.Fprintf(w, "  ! %s\n", pb)
	}
	fmt.Fprintf(w, "  %s\n", s.Root)
}

// optionalValue is a string option that knows whether it was given: an empty
// value clears a key.
type optionalValue struct {
	value string
	given bool
}

func (o *optionalValue) String() string { return o.value }
func (o *optionalValue) Set(v string) error {
	o.value, o.given = v, true
	return nil
}

// cmdProject shows or changes the key « project » of the profile (P11.6).
func cmdProject(args []string, stdout, stderr io.Writer) int {
	var kind, status, deadline, next optionalValue
	var asJSON bool
	path, ok := parse("project", args, stderr, func(fs *flag.FlagSet) {
		fs.Var(&kind, "type", "")
		fs.Var(&status, "status", "")
		fs.Var(&deadline, "deadline", "")
		fs.Var(&next, "next-action", "")
		fs.BoolVar(&asJSON, "json", false, "")
	})
	if !ok {
		return ExitUsage
	}
	var changes []home.MetaChange
	for _, c := range []struct {
		key string
		opt *optionalValue
	}{{project.MetaType, &kind}, {project.MetaStatus, &status}, {project.MetaDeadline, &deadline}, {project.MetaNextAction, &next}} {
		if c.opt.given {
			c.opt.value = strings.TrimSpace(c.opt.value)
			if err := project.CheckMeta(c.key, c.opt.value); err != nil {
				return usageError(stderr, "project : "+err.Error())
			}
			changes = append(changes, home.MetaChange{Key: c.key, Value: c.opt.value})
		}
	}
	if len(changes) > 0 && refusedToAgent("glose project --status, --deadline, --type et --next-action", stderr) {
		return ExitProblem
	}
	root, err := profileRoot(path)
	if err != nil {
		return failure(stderr, err)
	}
	if len(changes) > 0 {
		_, wrote, err := home.WriteMeta(root, "", changes)
		if errors.Is(err, store.ErrConflict) {
			fmt.Fprintf(stderr, "glose : %s a changé pendant l'écriture : rien n'est écrit ; relancez la commande.\n", project.ProfileName)
			return ExitProblem
		}
		if err != nil {
			return failure(stderr, err)
		}
		if !wrote && !asJSON {
			fmt.Fprintln(stdout, "Rien à changer.")
		}
	}
	p, err := project.Load(root)
	if err != nil {
		// The metadata is written; what prevents opening the text is said.
		if len(changes) == 0 {
			return failure(stderr, err)
		}
		fmt.Fprintf(stderr, "glose : %v\n", err)
		return ExitOK
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		warnings := p.MetaWarnings
		if warnings == nil {
			warnings = []string{}
		}
		enc.Encode(map[string]any{"root": p.Root, "title": p.Title, "meta": p.Meta, "warnings": warnings})
		return ExitOK
	}
	value := func(v string) string {
		if v == "" {
			return "—"
		}
		return v
	}
	st := value(p.Meta.Status)
	if n := statusNames[p.Meta.Status]; n != "" {
		st = n + " (" + p.Meta.Status + ")"
	}
	fmt.Fprintf(stdout, "%s\n  type : %s\n  statut : %s\n  échéance : %s\n  prochaine action : %s\n",
		p.Title, value(p.Meta.Type), st, value(p.Meta.Deadline), value(p.Meta.NextAction))
	for _, w := range p.MetaWarnings {
		fmt.Fprintf(stdout, "  ! %s\n", w)
	}
	return ExitOK
}

// profileRoot returns the directory of the profile of the project at path.
func profileRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
		abs = filepath.Dir(abs)
	}
	root, ok := project.FindRoot(abs)
	if !ok {
		return "", fmt.Errorf("pas de %s pour %s : lancez d'abord glose init", project.ProfileName, path)
	}
	return root, nil
}
