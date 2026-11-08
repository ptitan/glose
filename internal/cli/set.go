package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/server"
)

// noAgentLine is said by « glose open » and « glose status » until a
// default agent is recorded (P10.3, D-48).
const noAgentLine = "Aucun agent configuré : lancez « glose set »."

// agentsConfigured reports whether a default agent is recorded.
func agentsConfigured() bool {
	c, err := agents.Load()
	return err == nil && c.Configured()
}

// aliasDesc describes the agent and the model of an alias.
func aliasDesc(a agents.Alias) string {
	k, _ := agents.KindOf(a.Agent)
	if a.Model == "" {
		return k.Name + ", son modèle par défaut"
	}
	return k.Name + ", " + a.Model
}

// cmdSet opens the page of the settings of the agents (P10.3): on the
// server of the project if one runs, else on a settings server of its own,
// which stops when the page is closed. « --list » prints them instead.
func cmdSet(args []string, stdout, stderr io.Writer) int {
	var noBrowser, list bool
	path, ok := parse("set", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&noBrowser, "no-browser", false, "")
		fs.BoolVar(&list, "list", false, "")
	})
	if !ok {
		return ExitUsage
	}
	if list {
		return setList(stdout, stderr)
	}
	open := func(url string) {
		fmt.Fprintf(stdout, "Réglages des agents (tous les projets) : %s\n", url)
		if !noBrowser && os.Getenv("GLOSE_NO_BROWSER") == "" {
			if err := server.OpenBrowser(url); err != nil {
				fmt.Fprintf(stderr, "glose : navigateur non ouvert (%v) ; copiez l'adresse.\n", err)
			}
		}
	}
	// The server of the project, if one runs here: the page also shows what
	// the text asks (glose.json).
	if dir, err := project.StateOf(path); err == nil {
		if st, err := server.Running(dir); err == nil {
			open(st.Base() + "/set#t=" + st.Token)
			return ExitOK
		}
	}
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() { <-sig; close(stop) }()
	err := server.ServeSettings(func(url string) {
		open(url)
		fmt.Fprintln(stdout, "Le serveur des réglages s'arrête quelques minutes après la fermeture de la page (Ctrl+C pour l'arrêter tout de suite).")
	}, stop)
	if err != nil {
		return failure(stderr, err)
	}
	return ExitOK
}

// setList prints the agents found and the configuration.
func setList(stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d := agents.Detect(ctx)
	agents.SaveDetection(d)
	fmt.Fprintln(stdout, "Agents installés :")
	for _, a := range d.Agents {
		switch {
		case !a.Found:
			fmt.Fprintf(stdout, "  - %s : non trouvé\n", a.Name)
		case a.Windows:
			fmt.Fprintf(stdout, "  ! %s : installé côté Windows (%s), non lancé depuis WSL\n", a.Name, a.Path)
		case a.Error != "":
			fmt.Fprintf(stdout, "  ! %s : %s (%s)\n", a.Name, a.Error, a.Path)
		default:
			fmt.Fprintf(stdout, "  ✓ %s : %s (%s)\n", a.Name, a.Version, a.Path)
		}
	}
	c, err := agents.Load()
	if err != nil {
		return failure(stderr, err)
	}
	if !c.Configured() {
		fmt.Fprintln(stdout, noAgentLine)
		return ExitOK
	}
	fmt.Fprintln(stdout, "Noms courts :")
	for _, a := range c.Aliases {
		def := ""
		if a.Name == c.Default {
			def = " (par défaut)"
		}
		fmt.Fprintf(stdout, "  %s : %s%s\n", a.Name, aliasDesc(a), def)
	}
	return ExitOK
}
