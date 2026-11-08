package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// stdin is read when an option is given the value « - »; tests replace it.
var stdin io.Reader = os.Stdin

// fromStdin replaces by the standard input the one option among opts whose
// value is « - »: a long text, on several lines, with quotes or backticks,
// passes without shell quoting. The last line ending is removed (« echo »
// and here-documents add one). Two options at « - » are refused.
func fromStdin(opts map[string]*string) error {
	var name string
	for n, v := range opts {
		if *v == "-" {
			if name != "" {
				return errors.New("une seule option peut valoir « - » (lue sur l'entrée standard)")
			}
			name = n
		}
	}
	if name == "" {
		return nil
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	if !utf8.Valid(b) {
		return errors.New("--" + name + " - : l'entrée standard n'est pas en UTF-8 valide")
	}
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	*opts[name] = s
	return nil
}

// blankLine separates two paragraphs of the source.
var blankLine = regexp.MustCompile(`\n[ \t]*\r?\n`)

// contextMax bounds the context given with an annotation, in bytes.
const contextMax = 1200

// annotationContext returns the paragraph of src that holds the annotation
// starting at start and ending at end, as it reads now (view Current: without
// comments, proposals not applied), so that an agent knows where a comment
// stands without opening the file. A long paragraph is cut around the
// annotation, with « … ».
func annotationContext(src []byte, start, end int) string {
	ps, pe := 0, len(src)
	for _, m := range blankLine.FindAllIndex(src, -1) {
		if m[1] <= start {
			ps = m[1]
		} else if m[0] >= end {
			pe = m[0]
			break
		}
	}
	// A mark that crosses the edges of the paragraph (a comment with a
	// blank line) is taken whole: a mark is never cut.
	marks, _ := critic.Parse(src)
	for changed := true; changed; {
		changed = false
		for _, m := range marks {
			if m.Span.Start < pe && m.Span.End > ps && (m.Span.Start < ps || m.Span.End > pe) {
				ps, pe = min(ps, m.Span.Start), max(pe, m.Span.End)
				changed = true
			}
		}
	}
	if ps == 0 && bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) {
		ps = 3
	}
	text := projection.Project(src[ps:pe], projection.Current).Text
	if len(text) > contextMax && start >= ps {
		at := len(projection.Project(src[ps:start], projection.Current).Text)
		from := max(0, at-contextMax/3)
		to := min(len(text), from+contextMax)
		from = max(0, to-contextMax)
		for from > 0 && !utf8.RuneStart(text[from]) {
			from--
		}
		for to < len(text) && !utf8.RuneStart(text[to]) {
			to++
		}
		cut := string(text[from:to])
		if from > 0 {
			cut = "…" + cut
		}
		if to < len(text) {
			cut += "…"
		}
		return strings.TrimSpace(cut)
	}
	return strings.TrimSpace(string(text))
}

// agentEnv names the agent when --agent is not given (P10.1).
const agentEnv = "GLOSE_AGENT"

// agentIdentity returns the name of the agent: --agent, else GLOSE_AGENT,
// else « agent ». explicit tells whether a name was given.
func agentIdentity(flagValue string) (name string, explicit bool, err error) {
	name = flagValue
	if name == "" {
		name = os.Getenv(agentEnv)
	}
	if name == "" {
		return "agent", false, nil
	}
	if err := navette.CheckAgentName(name); err != nil {
		return "", true, err
	}
	return name, true, nil
}

// touchAgent records a sign of life of the agent (.glose/agent.json, not
// normative). A failure is ignored: it never stops a command.
func touchAgent(p *project.Project, name, activity string) {
	turn := 0
	if h, err := navette.New(p).History(); err == nil {
		turn = h.Turn()
	}
	navette.Touch(p.State(), name, activity, turn)
}

// joinTimeout is how long « glose join » waits for the hand before ending
// with « délai écoulé » (code 3): within the time that the agents allow a
// command (two minutes by default for some), the agent runs it again.
const joinTimeout = 90 * time.Second

// cmdJoin binds a session of an agent to a Glose opened by hand (P10.2), and
// waits for the hand (P10.12): « glose join CODE [--agent NOM] [--model M]
// [--timeout D] [dossier] ». The same command, run again, waits for the next
// turn: the agent has only one command to remember.
func cmdJoin(args []string, stdout, stderr io.Writer) int {
	// The code is the first word that is neither an option nor its value.
	code, rest := "", []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			rest = append(rest, a)
			if !strings.Contains(a, "=") && valueFlags[strings.TrimLeft(a, "-")] && i+1 < len(args) {
				rest = append(rest, args[i+1])
				i++
			}
			continue
		}
		if code == "" {
			code = a
			continue
		}
		rest = append(rest, a)
	}
	var agent, model string
	timeout := joinTimeout
	path, ok := parse("join", rest, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&agent, "agent", "", "")
		fs.StringVar(&model, "model", "", "")
		fs.DurationVar(&timeout, "timeout", joinTimeout, "")
	})
	if !ok {
		return ExitUsage
	}
	// A path given first is not a code (a code has neither « / » nor « . »).
	if code == "" || strings.ContainsAny(code, `/\.`) {
		return usageError(stderr, "join : le code affiché par Glose est requis (glose join CODE --agent NOM)")
	}
	name, _, err := agentIdentity(agent)
	if err != nil {
		return usageError(stderr, "join : "+err.Error())
	}
	if model != "" && agents.CheckModel(model) != nil {
		return usageError(stderr, fmt.Sprintf("join : modèle invalide %q (lettres, chiffres, « . _ : / # @ + - », sans espace)", model))
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	again, err := navette.UseLink(p.State(), code, name, navette.SessionKey(), time.Now())
	if err != nil {
		return failure(stderr, err)
	}
	// One wait at a time for a code: a second session that copied the same
	// instruction does not wait beside the first (Codex audit).
	wl, err := store.TryLockNamed(p.State(), "join.lock")
	if errors.Is(err, store.ErrLocked) {
		return failure(stderr, errors.New("une autre commande glose join attend déjà la main pour cette liaison : une seule session à la fois"))
	}
	if err != nil {
		return failure(stderr, err)
	}
	defer wl.Unlock()
	q := strconv.Quote(name)
	command := fmt.Sprintf("glose join %s --agent %s", code, q)
	// The model (P10.11): read in the agent when it exposes it, else the
	// one declared by --model.
	source := navette.ModelDeclared
	if model == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if model = agents.SessionModel(ctx); model != "" {
			source = navette.ModelByAgent
		}
		cancel()
	}
	n := navette.New(p)
	turn := 0
	if h, err := n.History(); err == nil {
		turn = h.Turn()
	}
	// The binding and the model are written as soon as no agent launched
	// by Glose holds the presence (P10.8), and kept by the signs that follow.
	joined := navette.TouchJoined(p.State(), name, turn, code, model, source) == nil
	if !again {
		desc := ""
		if model != "" {
			desc = " (modèle " + model + ")"
		}
		fmt.Fprintf(stdout, "Lié à la navette Glose de %s, sous le nom %s%s.\n", p.Root, q, desc)
	}
	if l := navette.LaunchedAgent(p.State()); l != nil && l.Name != name {
		fmt.Fprintf(stdout, "%s, lancé par Glose, mène le tour en cours : vous prendrez la main à la remise suivante de l'auteur.\n", l.Name)
	}
	// Wait for the hand (P10.12). The turn is this session's if it owns it
	// (navette.Reserve, before the hand is announced): neither an agent
	// launched by Glose (reserved before the handover, P10.9) nor another
	// session takes it at the same time. A turn with passes (P10.6) is this
	// session's only if the plan names it.
	deadline := time.Now().Add(timeout)
	var beat time.Time
	for {
		if !navette.StillLinked(p.State(), code, name) {
			return failure(stderr, errors.New("un nouveau code de liaison a été créé dans Glose : cette liaison est terminée ; collez la nouvelle consigne"))
		}
		h, err := n.History()
		if err != nil {
			return failure(stderr, err)
		}
		if !joined {
			joined = navette.TouchJoined(p.State(), name, h.Turn(), code, model, source) == nil
		}
		// The turn is this session's only if it takes it, under the lock of
		// the project (navette.TakeTurn).
		if h.Hand() == review.Agent {
			ok, err := n.TakeTurn(name, code)
			if err != nil {
				return failure(stderr, err)
			}
			if ok {
				navette.TouchBound(p.State(), name, navette.ActRead, h.Turn(), code, model, source)
				return joinTurn(p, n, name, command, stdout, stderr)
			}
		}
		// Each sign carries the binding and the model: a presence replaced
		// meanwhile by an agent launched by Glose gets them back.
		if time.Since(beat) >= navette.WaitBeat {
			navette.TouchBound(p.State(), name, navette.ActWait, h.Turn(), code, model, source)
			beat = time.Now()
		}
		if time.Now().After(deadline) {
			who := handNames[h.Hand()]
			if h.Hand() == review.Agent {
				who = "un autre agent, pour ce tour"
			}
			fmt.Fprintf(stdout, "Délai écoulé : la main est toujours à %s. Relancez exactement la même commande pour continuer d'attendre :\n%s\n", who, command)
			return ExitTimeout
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// joinTurn tells a bound session that the hand is its own: the rules of the
// navette, the state, and what to do next.
func joinTurn(p *project.Project, n *navette.Navette, name, command string, stdout, stderr io.Writer) int {
	st, _, _, err := n.State()
	if err != nil {
		return failure(stderr, err)
	}
	q := strconv.Quote(name)
	fmt.Fprintf(stdout, "La main est à vous : menez votre tour dans %s, toutes les commandes avec --agent %s.\n\n", p.Root, q)
	fmt.Fprintln(stdout, strings.TrimSpace(NavetteSection))
	fmt.Fprintf(stdout, "\n### État\n\n%s\n", stateLine(st))
	if ps := navette.ReadPasses(p.State()); ps != nil && ps.Turn == st.Turn && ps.Linked == name {
		fmt.Fprintf(stdout, "\nCe tour a plusieurs destinataires : d'autres agents répondent aux annotations qui leur sont adressées. "+
			"Ne traitez que les vôtres : glose list --json --mine --agent %s.\n", q)
	}
	fmt.Fprintf(stdout, "\n### Marche à suivre\n\n")
	fmt.Fprintf(stdout, "1. glose list --json --agent %s donne les annotations ; répondez (glose reply), proposez (glose propose).\n", q)
	fmt.Fprintf(stdout, "2. glose done --agent %s --note \"ce que vous avez fait\" remet la main à l'auteur.\n", q)
	fmt.Fprintf(stdout, "3. Puis relancez %s : elle attend la remise suivante (relancez-la tant qu'elle se termine par « délai écoulé »).\n", command)
	return ExitOK
}

// viaName turns the name of the agent into the value of via= (D-49): the
// letters, digits, « . », « _ » and « - » of its name, spaces as « - », 32
// at most; nothing for the default name « agent ».
func viaName(name string) string {
	if name == "" || name == "agent" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r < 128 && (r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		}
		if b.Len() == 32 {
			break
		}
	}
	v := strings.Trim(b.String(), "-")
	if !review.ValidName(v) {
		return ""
	}
	return v
}

// agentModel is the model of the agent name (P10.11): GLOSE_MODEL, set by
// Glose for an agent it launches, else the model recorded at its glose join.
func agentModel(p *project.Project, name string) string {
	if m := os.Getenv("GLOSE_MODEL"); m != "" && agents.CheckModel(m) == nil {
		return m
	}
	if pr := navette.ReadPresence(p.State()); pr != nil && pr.Name == name {
		return pr.Model
	}
	return ""
}
