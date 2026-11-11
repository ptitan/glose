// Package cli dispatches the glose subcommands.
//
// Messages shown to the user are in French (D-10); command names and JSON keys
// are in English (D-15).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/server"
	"github.com/ptitan/glose/internal/store"
)

// Version is the program version. Release builds override it with
// -ldflags "-X glose/internal/cli.Version=…".
var Version = "0.0.0-dev"

// Exit codes shared by every subcommand.
const (
	ExitOK      = 0 // success
	ExitProblem = 1 // the command ran and found problems (check), or failed
	ExitUsage   = 2 // unknown command or invalid arguments
)

const helpText = `Glose : la navette entre un auteur et son agent IA, dans les marges du texte.

Usage :
  glose <commande> [options] [dossier ou fichier]

Le projet est le dossier courant par défaut. Sans profil glose.json, Glose
prend les fichiers .md du dossier et de ses sous-dossiers (sauf les dossiers
cachés et les consignes d'agent : AGENTS.md, CLAUDE.md…), dans l'ordre d'une
table des matières ; avec un fichier, il prend ce seul fichier. Le profil
choisit les fichiers, dossiers ou motifs du texte, en écarte d'autres, et
fixe le titre, un filtre et une feuille de style : voir « glose help profile ».

Commandes de l'auteur :
  open      lance le serveur de relecture en arrière-plan et ouvre le navigateur ;
            un sous-dossier ou un fichier d'un projet (sous un glose.json)
            ouvre le projet entier, avec cette partie mise en avant
              --no-browser   n'ouvre pas le navigateur
  stop      arrête le serveur du projet
  set       ouvre les réglages des agents (tous les projets) : agents
            installés, noms courts (agent + modèle), agent par défaut
              --list         les affiche sans ouvrir le navigateur
              --no-browser   n'ouvre pas le navigateur
  home      ouvre la page de vos projets (ceux à profil ouverts par glose open
            ou glose init) : à qui est la main, annotations, échéance, serveur ;
            lit chaque projet sans rien y écrire ; un clic ouvre un projet.
            Réservé à l'auteur : refusé si GLOSE_AGENT est posé
              --list            les affiche sans ouvrir le navigateur
              --json            sortie JSON (clés stables, jamais de jeton)
              --no-browser      n'ouvre pas le navigateur
              --forget PROJET   retire un projet du registre (identifiant ou
                                chemin, même disparu) ; le projet n'est pas touché
  init      crée un profil glose.json s'il n'y en a pas (fichiers de la racine,
            puis un dossier par sous-dossier : à trier), et écrit la section
            « Navette Glose » dans AGENTS.md (ou CLAUDE.md s'il est seul)
              --to FICHIER   autre fichier d'instructions
              --print        affiche seulement la section
              --type NOM     recopie d'abord le gabarit NOM de votre configuration
                             (gabarits/NOM/ : arborescence, glose.json,
                             consignes) sans jamais écraser ; dit ce qu'il saute
  project   ce que vous dites du projet (glose.json, clé project) : l'affiche, ou
            change les valeurs données, sans toucher au reste du fichier
              --type TEXTE --status draft|active|review|done|archived
              --deadline AAAA-MM-JJ --next-action TEXTE  (valeur vide : effacer)
              --json         sortie JSON

Commandes de la navette (auteur et agent) :
  progress --text "…" [dossier]  annonce une étape (120 caractères au plus)
  status    à qui est la main, tour, annotations, anomalies
              --json         sortie JSON
  list      énumère les annotations des fichiers
              --json         sortie JSON (clés stables, en anglais ; context :
                             le paragraphe de l'annotation, tel qu'il se lit)
              --kind K       comment, highlight, addition, deletion, substitution
              --status S     new (de ce tour) ou open (remise, en attente)
              --by B         author ou agent
              --task ID      annotations ou réponses créées par cette consigne
              --mine         pendant un tour à plusieurs agents, celles qui ne
                             sont pas confiées aux agents lancés par Glose
              --to NOM       les annotations que doit traiter ce nom court
                             (glose set) ; sans destinataire, ou avec un
                             nom inconnu ici : le nom court par défaut
              --file F       un fichier, ou un dossier terminé par / (relatif
                             au projet ; par défaut, la partie donnée en chemin)
  wait      attend que la main soit au rôle demandé, et rend la main aussitôt
            si elle y est déjà
              --role R       agent (par défaut) ou author
              --after N      attend une remise postérieure à l'événement N
              --timeout D    délai (10m, 1h…) ; code 3 s'il s'écoule
              --json         sortie JSON
  log       l'histoire : tours, opérations, décisions archivées
              --turn N       un seul tour
              --json         sortie JSON

Lier une session d'agent à un Glose ouvert à la main :
  join      glose join CODE [--agent NOM] [--model M] : le code est donné par
            l'interface (menu de la navette, « Lier un agent ») ; il lie la
            session, puis attend que l'auteur lui remette la main et affiche
            alors les consignes du tour. Code 3 après --timeout (90s par
            défaut) : relancer la même commande, qui sert aussi à attendre
            chaque tour suivant (le code reste à cet agent jusqu'au prochain).
            --model : le modèle de l'agent, quand il ne se lit pas chez lui

Commandes de l'agent (pendant son tour) :
  done      remet le tour à l'auteur : les modifications libres des fichiers
            deviennent des propositions, les annotations reçoivent leur identité
              --note TEXTE   ce que l'agent a fait
  reply     répond dans le fil d'une annotation
              --id ID --text TEXTE
  propose   propose une modification précise, avec son explication
              --file F --find TEXTE --new TEXTE [--note TEXTE] [--whole]
                (--whole : une seule proposition pour toute la réécriture,
                à accepter ou rejeter en bloc ; sinon, mot à mot)
              --file F --after TEXTE --new TEXTE [--note TEXTE]   (ajout)
              --id ID --new TEXTE [--note TEXTE]   (passage commenté par l'auteur, ou correction d'une proposition)
  comment   commente un passage, ou un point après un texte
              --file F (--find TEXTE | --after TEXTE) --text TEXTE
  Pour wait, done, reply, propose et comment (et status, list s'il est
  donné) : --agent NOM, ou la variable GLOSE_AGENT, nomme l'agent ; son
  signe de vie (.glose/agent.json, non normatif) dit à l'auteur qui écoute,
  qui a la main et ce qu'il écrit.
  Pour reply, propose, comment et done : TEXTE vaut « - » pour le lire sur l'entrée
  standard (une seule option par commande ; dernier saut de ligne retiré),
  et --json donne une sortie JSON (identifiants créés ; pour done, le
  curseur seq de glose wait --after).

Contrôles :
  check     contrôle le profil, pandoc, les marques et le journal ; code 1
            s'il y a des anomalies
              --json         sortie JSON
  doctor    contrôle l'installation (pandoc, Git, navigateur) et le projet
            (profil, filtre, feuille de style, droits sur .glose, serveur) ;
            chaque problème vient avec sa correction ; code 1 s'il manque
            l'indispensable
              --json         sortie JSON
  render    écrit sur la sortie le HTML affiché par l'interface (--view proposed : vue Proposé)
  version   affiche la version
  help      affiche cette aide ; « glose help profile » : le profil glose.json
            et le filtre d'affichage, avec un exemple

L'agent ne peut ni accepter, ni rejeter, ni résoudre : l'auteur seul décide.
`

// Run executes the command line args (without the program name) and returns
// the process exit code. Output goes to stdout, diagnostics to stderr.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, helpText)
		return ExitOK
	}
	cmd, rest := args[0], args[1:]
	// « -h » demande l'aide, sauf s'il est la valeur d'une option (« --text
	// -h ») : la commande s'exécute alors normalement. Les arguments se lisent
	// dans l'ordre, la valeur d'une option étant sautée (« --note --file -h » :
	// « --file » est la note, « -h » demande l'aide).
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "-h" || a == "--help" || a == "-help" {
			fmt.Fprint(stdout, helpText)
			return ExitOK
		}
		if strings.HasPrefix(a, "-") && !strings.Contains(a, "=") && valueFlags[strings.TrimLeft(a, "-")] {
			i++ // sa valeur
		}
	}
	switch cmd {
	case "help", "-h", "--help":
		if len(rest) == 1 && rest[0] == "profile" {
			fmt.Fprint(stdout, profileHelpText)
			return ExitOK
		}
		if len(rest) > 0 {
			return usageError(stderr, "help : seul sujet connu, « glose help profile »")
		}
		fmt.Fprint(stdout, helpText)
		return ExitOK
	case "version", "--version":
		if len(rest) > 0 {
			return usageError(stderr, "la commande version ne prend pas d'argument")
		}
		fmt.Fprintf(stdout, "glose %s\n", Version)
		return ExitOK
	case "open":
		return cmdOpen(rest, stdout, stderr)
	case "stop":
		return cmdStop(rest, stdout, stderr)
	case "progress":
		return cmdProgress(rest, stdout, stderr)
	case "status":
		return cmdStatus(rest, stdout, stderr)
	case "list":
		return cmdList(rest, stdout, stderr)
	case "check":
		return cmdCheck(rest, stdout, stderr)
	case "doctor":
		return cmdDoctor(rest, stdout, stderr)
	case "render":
		return cmdRender(rest, stdout, stderr)
	case "done":
		return cmdDone(rest, stdout, stderr)
	case "wait":
		return cmdWait(rest, stdout, stderr)
	case "log":
		return cmdLog(rest, stdout, stderr)
	case "init":
		return cmdInit(rest, stdout, stderr)
	case "comment":
		return cmdComment(rest, stdout, stderr)
	case "propose":
		return cmdPropose(rest, stdout, stderr)
	case "reply":
		return cmdReply(rest, stdout, stderr)
	case "join":
		return cmdJoin(rest, stdout, stderr)
	case "set":
		return cmdSet(rest, stdout, stderr)
	case "home":
		return cmdHome(rest, stdout, stderr)
	case "project":
		return cmdProject(rest, stdout, stderr)
	case "serve":
		return cmdServe(rest, stderr)
	default:
		return usageError(stderr, fmt.Sprintf("commande inconnue : %q", cmd))
	}
}

// valueFlags are the options that take a value: the word after them is that
// value, even if it starts with a dash.
var valueFlags = map[string]bool{"file": true, "find": true, "after": true, "id": true, "text": true, "new": true, "note": true,
	"kind": true, "status": true, "by": true, "role": true, "timeout": true, "turn": true, "to": true, "view": true, "agent": true, "model": true,
	"forget": true, "type": true, "deadline": true, "next-action": true}

// relPath normalises a path of the project typed by the user: forward
// slashes, without a leading « ./ » (or « .\ » under Windows).
func relPath(s string) string {
	s = filepath.ToSlash(s)
	for strings.HasPrefix(s, "./") {
		s = s[2:]
	}
	return s
}

func usageError(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "glose : %s\nVoir « glose help ».\n", msg)
	return ExitUsage
}

func failure(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "glose : %v\n", err)
	return ExitProblem
}

// parse reads the options of a subcommand and its optional project argument.
func parse(name string, args []string, stderr io.Writer, define func(*flag.FlagSet)) (string, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if define != nil {
		define(fs)
	}
	// Options may come after the path: move them first.
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			if !strings.Contains(a, "=") {
				if f := fs.Lookup(strings.TrimLeft(a, "-")); f != nil {
					if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
						if i+1 < len(args) {
							flags = append(flags, args[i+1])
							i++
						}
					}
				}
			}
			continue
		}
		pos = append(pos, a)
	}
	if err := fs.Parse(flags); err != nil {
		usageError(stderr, fmt.Sprintf("%s : %v", name, err))
		return "", false
	}
	if len(pos) > 1 {
		usageError(stderr, fmt.Sprintf("%s : un seul dossier ou fichier attendu", name))
		return "", false
	}
	if len(pos) == 0 {
		return ".", true
	}
	return pos[0], true
}

func cmdOpen(args []string, stdout, stderr io.Writer) int {
	var noBrowser bool
	path, ok := parse("open", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&noBrowser, "no-browser", false, "")
	})
	if !ok {
		return ExitUsage
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := render.CheckPandoc(ctx, p.Pandoc); err != nil {
		return failure(stderr, err)
	}
	st, started, err := server.Start(p)
	if err != nil {
		return failure(stderr, err)
	}
	if p.Profiled {
		register(p.Root, stderr)
	}
	fmt.Fprintf(stdout, "Glose — %s (%d fichier%s)\n", p.Title, len(p.Files), plural(len(p.Files)))
	if !started {
		fmt.Fprintln(stdout, "Le serveur tournait déjà.")
	}
	url := st.URL
	if p.Focus != "" {
		// %20 rather than « + »: the interface decodes with decodeURIComponent.
		url += "&f=" + strings.ReplaceAll(neturl.QueryEscape(p.Focus), "+", "%20")
		fmt.Fprintf(stdout, "Projet entier ouvert à %s, avec %s mis en avant.\n", p.Root, p.Focus)
	}
	fmt.Fprintf(stdout, "Ouvrir : %s\nArrêter : glose stop\n", url)
	if !agentsConfigured() {
		fmt.Fprintln(stdout, noAgentLine)
	}
	if !noBrowser && os.Getenv("GLOSE_NO_BROWSER") == "" {
		if err := server.OpenBrowser(url); err != nil {
			fmt.Fprintf(stderr, "glose : navigateur non ouvert (%v) ; copiez l'adresse.\n", err)
		}
	}
	return ExitOK
}

func cmdStop(args []string, stdout, stderr io.Writer) int {
	path, ok := parse("stop", args, stderr, nil)
	if !ok {
		return ExitUsage
	}
	state, err := project.StateOf(path)
	if err != nil {
		return failure(stderr, err)
	}
	if err := server.StopRunning(state); errors.Is(err, server.ErrNotRunning) {
		fmt.Fprintln(stdout, "Aucun serveur ne tournait pour ce projet.")
		return ExitOK
	} else if err != nil {
		return failure(stderr, err)
	}
	fmt.Fprintln(stdout, "Serveur arrêté.")
	return ExitOK
}

func cmdServe(args []string, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr, "serve : usage interne, « glose serve <dossier> »")
	}
	p, err := project.Load(args[0])
	if err != nil {
		return failure(stderr, err)
	}
	if err := server.Serve(p); err != nil {
		return failure(stderr, err)
	}
	return ExitOK
}

// fileMarks reads one file and returns its annotations and problems.
type fileMarks struct {
	Path        string
	Src         []byte
	Annotations []render.Annotation
	Issues      []render.Issue
	Err         error
}

func readMarks(p *project.Project) []fileMarks {
	var out []fileMarks
	for i, rel := range p.Files {
		fm := fileMarks{Path: rel}
		d, err := store.Read(p.Path(i))
		switch {
		case err != nil:
			fm.Err = err
		case !utf8.Valid(d.Bytes):
			fm.Err = errors.New("le fichier n'est pas en UTF-8 valide")
		default:
			fm.Src = d.Bytes
			fm.Annotations = render.Annotations(i, d.Bytes)
			fm.Issues = render.Issues(i, d.Bytes)
		}
		out = append(out, fm)
	}
	var all []render.Annotation
	for _, fm := range out {
		all = append(all, fm.Annotations...)
	}
	for _, is := range render.CrossDuplicates(all) {
		out[is.File].Issues = append(out[is.File].Issues, is)
	}
	return out
}

// listItem is one annotation in the JSON output of « glose list ». The schema
// is documented in docs/SPEC.md (F15); keys are stable.
type listItem struct {
	File     string           `json:"file"`
	ID       string           `json:"id"`
	By       string           `json:"by"`
	Status   string           `json:"status"` // new or open
	Turn     int              `json:"turn"`
	Messages []render.Message `json:"messages"`
	Line     int              `json:"line"`
	Column   int              `json:"column"`
	Start    int              `json:"start"`
	End      int              `json:"end"`
	Kind     string           `json:"kind"`
	Text     string           `json:"text"`
	New      string           `json:"new,omitempty"`
	Comments []string         `json:"comments"`
	// Context is the paragraph that holds the annotation, as it reads now
	// (without comments, proposals not applied).
	Context string `json:"context"`
	// To is the short name of the agent that must answer (D-49), "" for
	// the default one; Via the agent that wrote a proposal of the agent.
	To    string `json:"to,omitempty"`
	Via   string `json:"via,omitempty"`
	Task  string `json:"task,omitempty"`
	Model string `json:"model,omitempty"` // model of that agent (P10.11)
}

// modelSuffix writes « , model » after the name of an agent.
func modelSuffix(m string) string {
	if m == "" {
		return ""
	}
	return ", " + m
}

type listIssue struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Offset  int    `json:"offset"`
	Message string `json:"message"`
}

func cmdList(args []string, stdout, stderr io.Writer) int {
	var asJSON bool
	var kind, file, status, by, agent, to, task string
	var mine bool
	path, ok := parse("list", args, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&task, "task", "", "")
		fs.StringVar(&to, "to", "", "")
		fs.BoolVar(&mine, "mine", false, "")
		fs.BoolVar(&asJSON, "json", false, "")
		fs.StringVar(&agent, "agent", "", "")
		fs.StringVar(&kind, "kind", "", "")
		fs.StringVar(&file, "file", "", "")
		fs.StringVar(&status, "status", "", "")
		fs.StringVar(&by, "by", "", "")
	})
	if !ok {
		return ExitUsage
	}
	kinds := map[string]bool{"": true, "comment": true, "highlight": true, "addition": true, "deletion": true, "substitution": true}
	if !kinds[kind] {
		return usageError(stderr, fmt.Sprintf("list : type inconnu %q", kind))
	}
	if task != "" && !review.ValidName(task) {
		return usageError(stderr, "list : identifiant de mission invalide")
	}
	if by != "" && by != "author" && by != "agent" {
		return usageError(stderr, fmt.Sprintf("list : auteur inconnu %q (author ou agent)", by))
	}
	if status != "" && status != navette.StatusNew && status != navette.StatusOpen {
		return usageError(stderr, fmt.Sprintf("list : statut inconnu %q (new ou open)", status))
	}
	agentName, explicit, err := agentIdentity(agent)
	if err != nil {
		return usageError(stderr, "list : "+err.Error())
	}
	if to != "" && !review.ValidName(to) {
		return usageError(stderr, fmt.Sprintf("list : nom court invalide %q", to))
	}
	if mine && to != "" {
		return usageError(stderr, "list : --mine ou --to, pas les deux")
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	if explicit {
		touchAgent(p, agentName, navette.ActRead)
	}
	// --to: the annotations that this agent must answer (D-49). Without a
	// recipient, or with one unknown on this machine, it is the agent of
	// the text (glose.json), else the default one.
	cfg, _ := agents.Load()
	def := cfg.Default
	if al, ok := cfg.Alias(p.Agent.Alias); ok {
		def = al.Name
	}
	recipient := func(a render.Annotation) string {
		if a.To != "" {
			if al, ok := cfg.Alias(a.To); ok {
				return al.Name
			}
		}
		return def
	}
	// During a turn with several recipients (P10.6), the routing fixed in
	// the plan at the handover: --to gives the annotations of a pass, --mine
	// (a session bound to Glose) what no pass takes.
	var plan *navette.Passes
	launched := map[string]bool{}
	if hh, err := navette.New(p).History(); err == nil && hh.Hand() == review.Agent {
		if ps := navette.ReadPasses(p.State()); ps != nil && ps.Turn == hh.Turn() {
			plan = ps
			for _, x := range ps.Passes {
				launched[strings.ToLower(x.Alias)] = true
			}
		}
	}
	wanted := func(a render.Annotation) bool {
		if mine {
			if a.ID != "" {
				_, taken := plan.Assigned(a.ID)
				return !taken
			}
			return !launched[strings.ToLower(recipient(a))]
		}
		if to == "" {
			return true
		}
		if pass, ok := plan.Find(to); ok && a.ID != "" {
			got, taken := plan.Assigned(a.ID)
			return taken && got.Alias == pass.Alias
		}
		return strings.EqualFold(recipient(a), to) || (def == "" && strings.EqualFold(a.To, to))
	}
	h, err := navette.New(p).History()
	if err != nil {
		return failure(stderr, err)
	}
	items := []listItem{}
	problems := []listIssue{}
	// A file, or a directory (« livre-1/ »); by default the part asked for
	// by the path given (D-35).
	want := relPath(file)
	if want == "" {
		want = p.Focus
	}
	inPart := func(path string) bool {
		return want == "" || path == want || (strings.HasSuffix(want, "/") && strings.HasPrefix(path, want))
	}
	found := want == ""
	for _, fm := range readMarks(p) {
		if !inPart(fm.Path) {
			continue
		}
		found = true
		if fm.Err != nil {
			problems = append(problems, listIssue{File: fm.Path, Message: fm.Err.Error()})
			continue
		}
		navette.Annotate(fm.Annotations, h)
		for _, a := range fm.Annotations {
			if (kind == "" || a.Kind == kind) && (status == "" || a.Status == status) && (by == "" || a.By == by) && wanted(a) && (task == "" || a.Task == task || messageTask(a.Messages, task)) {
				items = append(items, listItem{File: fm.Path, ID: a.ID, By: a.By, Status: a.Status, Turn: a.Turn, Messages: a.Messages, Line: a.Line, Column: a.Column, Start: a.Start, End: a.End,
					Kind: a.Kind, Text: a.Text, New: a.New, Comments: a.Comments, Context: annotationContext(fm.Src, a.Start, a.End), To: a.To, Via: a.Via, Model: a.Model, Task: a.Task})
			}
		}
		for _, is := range fm.Issues {
			problems = append(problems, listIssue{File: fm.Path, Line: is.Line, Column: is.Column, Offset: is.Offset, Message: is.Message})
		}
	}
	if !found {
		return usageError(stderr, fmt.Sprintf("list : %q n'est ni un fichier ni un dossier du projet", want))
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		imposed := []journal.Closed{}
		for _, c := range h.Imposed() {
			if inPart(c.File) {
				imposed = append(imposed, c)
			}
		}
		enc.Encode(map[string]any{"project": p.Root, "annotations": items, "issues": problems, "imposed": imposed})
		return ExitOK
	}
	names := map[string]string{"comment": "commentaire", "highlight": "passage commenté", "addition": "ajout",
		"deletion": "suppression", "substitution": "remplacement"}
	for _, it := range items {
		var what string
		switch it.Kind {
		case "substitution":
			what = fmt.Sprintf("« %s » → « %s »", oneLine(it.Text, 60), oneLine(it.New, 60))
		case "comment":
			what = oneLine(it.Text, 120)
		default:
			what = "« " + oneLine(it.Text, 80) + " »"
		}
		id := it.ID
		if id == "" {
			id = "sans identifiant"
		}
		dest := ""
		if it.To != "" {
			dest = "  → " + it.To
		}
		if it.Via != "" {
			dest += "  (par " + it.Via + modelSuffix(it.Model) + ")"
		}
		fmt.Fprintf(stdout, "%s:%d:%d  [%s%s]  %s  %s%s\n", it.File, it.Line, it.Column, id, actorSuffix(it.By), names[it.Kind], what, dest)
		for _, m := range it.Messages {
			if it.Kind == "comment" && !m.Reply {
				continue
			}
			who := actorPrefix(m.By)
			if m.Via != "" {
				who = "(" + m.Via + modelSuffix(m.Model) + ") "
			}
			fmt.Fprintf(stdout, "    ↳ %s%s\n", who, oneLine(m.Text, 120))
		}
	}
	for _, pb := range problems {
		fmt.Fprintf(stdout, "%s:%d:%d  anomalie  %s\n", pb.File, pb.Line, pb.Column, pb.Message)
	}
	imposed := 0
	for _, c := range h.Imposed() {
		if inPart(c.File) {
			fmt.Fprintf(stdout, "%s:%d  [imposé par l'auteur]  %s\n", c.File, c.Line, imposedText(c))
			imposed++
		}
	}
	if len(items) == 0 && len(problems) == 0 && imposed == 0 {
		fmt.Fprintln(stdout, "Aucune annotation.")
	}
	return ExitOK
}

// actorNames are the French names of the actors.
var actorNames = map[string]string{"author": "auteur", "agent": "agent", "unknown": "inconnu"}

func actorSuffix(by string) string {
	if n, ok := actorNames[by]; ok {
		return ", " + n
	}
	return ""
}

func actorPrefix(by string) string {
	if n, ok := actorNames[by]; ok {
		return "(" + n + ") "
	}
	return ""
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > n {
		r := []rune(s)
		return string(r[:n-1]) + "…"
	}
	return s
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

func cmdCheck(args []string, stdout, stderr io.Writer) int {
	var asJSON bool
	path, ok := parse("check", args, stderr, func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "") })
	if !ok {
		return ExitUsage
	}
	type finding struct {
		Level   string `json:"level"` // ok, warning, error
		Subject string `json:"subject"`
		Message string `json:"message"`
	}
	var out []finding
	add := func(level, subject, msg string) { out = append(out, finding{level, subject, msg}) }
	if _, err := agents.Load(); err != nil {
		add("error", "consignes globales", err.Error())
	}
	p, err := project.Load(path)
	if err != nil {
		add("error", "profil", err.Error())
	} else {
		if p.Profiled {
			add("ok", "profil", fmt.Sprintf("%s lu : %d fichier%s", project.ProfileName, len(p.Files), plural(len(p.Files))))
		} else {
			add("ok", "profil", fmt.Sprintf("pas de %s : %d fichier%s .md du dossier", project.ProfileName, len(p.Files), plural(len(p.Files))))
		}
		for _, w := range p.MetaWarnings {
			add("warning", "métadonnées", w)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		v, err := render.CheckPandoc(ctx, p.Pandoc)
		cancel()
		if err != nil {
			add("error", "pandoc", err.Error())
		} else {
			add("ok", "pandoc", v)
		}
		for _, fm := range readMarks(p) {
			if fm.Err != nil {
				add("error", fm.Path, fm.Err.Error())
				continue
			}
			for _, is := range fm.Issues {
				add("error", fmt.Sprintf("%s:%d:%d", fm.Path, is.Line, is.Column), is.Message)
			}
			// Safety net of D-24: a mark left literal that pandoc does not
			// read as code would be hidden from the review.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if sus, err := render.New(p).LiteralOutsideCode(ctx, fm.Src); err == nil {
				for _, off := range sus {
					l, c := render.LineCol(fm.Src, off)
					add("error", fmt.Sprintf("%s:%d:%d", fm.Path, l, c),
						"cette marque est prise pour du code par Glose, mais pandoc ne la lit pas comme du code : elle est absente de la relecture ; signalez ce cas")
				}
			}
			cancel()
			if lit := critic.Scan(fm.Src).Literal; len(lit) > 0 {
				l, c := render.LineCol(fm.Src, lit[0])
				msg := "une marque écrite dans du code est laissée telle quelle : ce n'est pas une annotation (D-24)"
				if len(lit) > 1 {
					msg = fmt.Sprintf("%d marques écrites dans du code sont laissées telles quelles, la première ici : ce ne sont pas des annotations (D-24)", len(lit))
				}
				add("warning", fmt.Sprintf("%s:%d:%d", fm.Path, l, c), msg)
			}
		}
		// The agent asked by the text (P10.3): its short name must exist on
		// this machine, or the default one is taken.
		if a := p.Agent.Alias; a != "" {
			if c, err := agents.Load(); err == nil {
				if _, ok := c.Alias(a); !ok {
					add("warning", "agent", fmt.Sprintf("%s demande le nom court %q, inconnu sur cette machine : le nom court par défaut sera pris (glose set)", project.ProfileName, a))
				}
			}
		}
		// The journal and its agreement with the files (SPEC F19).
		if h, err := navette.New(p).History(); err != nil {
			add("error", "journal", err.Error())
		} else {
			for _, pb := range h.Problems {
				add("error", fmt.Sprintf("journal:%d", pb.Line), pb.Message)
			}
			for _, op := range h.Pending {
				add("warning", "journal", fmt.Sprintf("opération %s (%s) préparée et jamais confirmée : elle sera réglée à la prochaine écriture", op.ID, op.Kind))
			}
			for _, e := range journal.Partials(h.Events) {
				add("warning", fmt.Sprintf("journal:%d", e.Seq), fmt.Sprintf("%s ; fichiers : %s", e.Reason, strings.Join(e.Files, ", ")))
			}
			// During the agent's turn: what « glose done » would refuse.
			if h.Hand() == "agent" {
				if pbs, err := navette.New(p).Preview(); err != nil {
					// Never silent: what prevents the preview prevents the handover.
					add("error", "navette", "glose done refusera : "+err.Error())
				} else {
					for _, pb := range pbs {
						add("error", fmt.Sprintf("%s:%d", pb.File, pb.Line), "glose done refusera : "+pb.Message)
					}
				}
			}
			for _, id := range navette.Disappeared(navette.New(p).ReadFiles(), h) {
				add("warning", "journal", fmt.Sprintf("annotation %s disparue sans décision (D-14) : elle n'est pas tenue pour acceptée", id))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == "ok" && out[j].Level != "ok" })
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
		for _, f := range out {
			mark := map[string]string{"ok": "✓", "warning": "!", "error": "✗"}[f.Level]
			fmt.Fprintf(stdout, "%s %s : %s\n", mark, f.Subject, f.Message)
		}
	}
	if bad {
		return ExitProblem
	}
	return ExitOK
}

func cmdRender(args []string, stdout, stderr io.Writer) int {
	var viewName string
	path, ok := parse("render", args, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&viewName, "view", "current", "vue : current (actuel) ou proposed (proposé)")
	})
	if !ok {
		return ExitUsage
	}
	view, known := map[string]projection.View{"current": projection.Current, "proposed": projection.Proposed}[viewName]
	if !known {
		usageError(stderr, "render : vue inconnue « "+viewName+" » (current ou proposed)")
		return ExitUsage
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	d, err := render.New(p).RenderView(ctx, view)
	if err != nil {
		return failure(stderr, err)
	}
	io.WriteString(stdout, d.HTML)
	for _, f := range d.Files {
		if f.Error != "" {
			fmt.Fprintf(stderr, "glose : %s : %s\n", f.Path, f.Error)
		}
	}
	return ExitOK
}

func messageTask(messages []render.Message, task string) bool {
	for _, m := range messages {
		if m.Task == task {
			return true
		}
	}
	return false
}
