package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/server"
)

// ExitTimeout is the exit code of « glose wait » when its delay runs out.
const ExitTimeout = 3

var handNames = map[string]string{review.Author: "l'auteur", review.Agent: "l'agent"}

// when formats a time of the journal for the screen.
func when(t string) string {
	tt, err := time.Parse(time.RFC3339Nano, t)
	if err != nil {
		return t
	}
	return tt.Local().Format("02/01/2006 à 15:04")
}

// stateLine is the one-line summary of the exchange (SPEC F14).
func stateLine(st navette.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tour %d : la main est à %s", st.Round, handNames[st.Hand])
	if st.Since != "" {
		verb := "remise par"
		if st.Reclaimed {
			verb = "reprise à"
		}
		fmt.Fprintf(&b, " (%s %s le %s)", verb, handNames[st.LastActor], when(st.Since))
	}
	fmt.Fprintf(&b, " · %d annotation%s", st.Annotations, plural(st.Annotations))
	if st.Annotations > 0 {
		var parts []string
		if n := st.Statuses[navette.StatusNew]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d nouvelle%s", n, plural(n)))
		}
		if n := st.Statuses[navette.StatusOpen]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d en attente", n))
		}
		b.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	if st.Issues > 0 {
		fmt.Fprintf(&b, " · %d anomalie%s", st.Issues, plural(st.Issues))
	}
	// The note of the handover, in full on its own line: it may be the
	// instruction of the round (navette d'essai du 7 novembre 2026).
	if st.Note != "" {
		fmt.Fprintf(&b, "\nNote de %s : %s", handNames[st.LastActor], st.Note)
	}
	return b.String()
}

func cmdStatus(args []string, stdout, stderr io.Writer) int {
	var asJSON bool
	var agent string
	path, ok := parse("status", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&asJSON, "json", false, "")
		fs.StringVar(&agent, "agent", "", "")
	})
	if !ok {
		return ExitUsage
	}
	agentName, explicit, err := agentIdentity(agent)
	if err != nil {
		return usageError(stderr, "status : "+err.Error())
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	if explicit {
		touchAgent(p, agentName, navette.ActRead)
	}
	st, _, _, err := navette.New(p).State()
	if err != nil {
		return failure(stderr, err)
	}
	srv, srvErr := server.Running(p.State())
	// The address with its token is the author's key to decide: never shown
	// to an agent (D-14), who runs status under its name (P10.1).
	url := ""
	if srvErr == nil {
		url = srv.URL
		if explicit {
			url = srv.Base()
		}
	}
	if asJSON {
		out := map[string]any{"project": p.Root, "state": st, "server": nil, "agentsConfigured": agentsConfigured()}
		if srvErr == nil {
			out["server"] = map[string]any{"url": url, "pid": srv.PID}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return ExitOK
	}
	fmt.Fprintln(stdout, stateLine(st))
	if srvErr == nil {
		fmt.Fprintf(stdout, "Serveur : en marche, %s (processus %d)\n", url, srv.PID)
	} else {
		fmt.Fprintln(stdout, "Serveur : arrêté (glose open pour le lancer)")
	}
	if st.Annotations > 0 {
		var parts []string
		for _, k := range []string{"comment", "highlight", "addition", "deletion", "substitution"} {
			if n := st.Kinds[k]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, kindNames[k]))
			}
		}
		fmt.Fprintf(stdout, "Annotations : %s\n", strings.Join(parts, ", "))
	}
	if st.Issues > 0 {
		fmt.Fprintf(stdout, "Anomalies : %d (glose check)\n", st.Issues)
	}
	for _, pb := range st.Problems {
		fmt.Fprintf(stdout, "Attention : %s\n", pb)
	}
	if a := st.Agent; a != nil {
		fresh := "aucun signe depuis " + when(a.Seen)
		if a.Fresh {
			fresh = "dernier signe le " + when(a.Seen)
		}
		fmt.Fprintf(stdout, "Agent : %s (%s)\n", a.Name, fresh)
	}
	if !agentsConfigured() {
		fmt.Fprintln(stdout, noAgentLine)
	}
	return ExitOK
}

var kindNames = map[string]string{"comment": "commentaire(s)", "highlight": "passage(s) commenté(s)", "addition": "ajout(s)",
	"deletion": "suppression(s)", "substitution": "remplacement(s)"}

func cmdDone(args []string, stdout, stderr io.Writer) int {
	var note, agent string
	var asJSON bool
	path, ok := parse("done", args, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&note, "note", "", "")
		fs.BoolVar(&asJSON, "json", false, "")
		fs.StringVar(&agent, "agent", "", "")
	})
	if !ok {
		return ExitUsage
	}
	if err := fromStdin(map[string]*string{"note": &note}); err != nil {
		return usageError(stderr, "done : "+err.Error())
	}
	agentName, _, err := agentIdentity(agent)
	if err != nil {
		return usageError(stderr, "done : "+err.Error())
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	// A turn with several recipients (P10.6): done closes the pass of this
	// agent, and Glose hands the turn back after the last one.
	if h, herr := navette.New(p).History(); herr == nil && h.Hand() == review.Agent {
		pass := os.Getenv(navette.PassEnv)
		switch err := navette.ClosePass(p.State(), h.Turn(), pass, agentName, strings.TrimSpace(note)); {
		case err == nil:
			touchAgent(p, agentName, navette.ActDone)
			if asJSON {
				json.NewEncoder(stdout).Encode(map[string]any{"pass": true, "round": navette.Round(h.Turn())})
			} else {
				fmt.Fprintln(stdout, "Votre passage est clos. Glose remettra la main à l'auteur après les autres agents de ce tour ; arrêtez-vous ici.")
			}
			return ExitOK
		case !errors.Is(err, navette.ErrNoPass):
			return failure(stderr, err)
		case pass != "":
			return failure(stderr, fmt.Errorf("aucun passage %s en cours pour ce tour : la main n'est pas remise ; arrêtez-vous ici", pass))
		}
		// A bound session whose part is closed never hands over the whole
		// turn while the other agents work (Codex).
		if ps := navette.ReadPasses(p.State()); ps != nil && ps.Turn == h.Turn() && ps.Linked == agentName && ps.LinkedDone {
			return failure(stderr, errors.New("votre part de ce tour est déjà close : Glose remettra la main après les autres agents ; attendez le tour suivant"))
		}
	}
	ho, err := navette.New(p).Handoff(review.Agent, strings.TrimSpace(note))
	if err == nil {
		touchAgent(p, agentName, navette.ActDone)
	} else {
		touchAgent(p, agentName, navette.ActRead)
	}
	var te *navette.TrackError
	if errors.As(err, &te) {
		fmt.Fprintln(stderr, "glose : votre tour n'est pas remis. "+te.Error())
		fmt.Fprintln(stderr, "Corrigez ces points dans les fichiers, puis relancez « glose done ».")
		return ExitProblem
	}
	if err != nil {
		return failure(stderr, err)
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.Encode(map[string]any{"round": navette.Round(ho.Event.TurnEv.Number), "seq": ho.Event.Seq,
			"converted": ho.Converted, "identified": ho.Identified, "files": ho.Files})
		return ExitOK
	}
	fmt.Fprintf(stdout, "Réponse du tour %d remise à l'auteur.", navette.Round(ho.Event.TurnEv.Number))
	if ho.Converted > 0 {
		fmt.Fprintf(stdout, " %d proposition%s tirée%s de vos modifications.", ho.Converted, plural(ho.Converted), plural(ho.Converted))
	}
	if ho.Identified > 0 {
		fmt.Fprintf(stdout, " %d identifiant%s ajouté%s.", ho.Identified, plural(ho.Identified), plural(ho.Identified))
	}
	fmt.Fprintln(stdout)
	return ExitOK
}

func cmdWait(args []string, stdout, stderr io.Writer) int {
	var role, agent string
	var after int64
	var timeout time.Duration
	var asJSON bool
	path, ok := parse("wait", args, stderr, func(fs *flag.FlagSet) {
		fs.StringVar(&agent, "agent", "", "")
		fs.StringVar(&role, "role", review.Agent, "")
		fs.Int64Var(&after, "after", 0, "")
		fs.DurationVar(&timeout, "timeout", 0, "")
		fs.BoolVar(&asJSON, "json", false, "")
	})
	if !ok {
		return ExitUsage
	}
	if role != review.Agent && role != review.Author {
		return usageError(stderr, "wait : --role vaut agent ou author")
	}
	agentName, _, err := agentIdentity(agent)
	if err != nil {
		return usageError(stderr, "wait : "+err.Error())
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	n := navette.New(p)
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	// The agent who waits renews his sign of life (P10.1), at most every
	// WaitBeat: not at each reading of the journal.
	var beat time.Time
	for {
		h, err := n.History()
		if err != nil {
			return failure(stderr, err)
		}
		var lastSeq int64
		if l := h.Last(); l != nil {
			lastSeq = l.Seq
		}
		ready := h.Hand() == role && lastSeq >= after && (after == 0 || lastSeq > after)
		if role == review.Agent {
			if ready {
				navette.Touch(p.State(), agentName, navette.ActRead, h.Turn())
			} else if time.Since(beat) >= navette.WaitBeat {
				navette.Touch(p.State(), agentName, navette.ActWait, h.Turn())
				beat = time.Now()
			}
		}
		if ready {
			st, _, _, err := n.State()
			if err != nil {
				return failure(stderr, err)
			}
			// A turn with several recipients (P10.6): this session answers
			// only what the agents launched by Glose do not take.
			var shared string
			if ps := navette.ReadPasses(p.State()); role == review.Agent && ps != nil && ps.Turn == h.Turn() && ps.Linked == agentName {
				shared = fmt.Sprintf("Ce tour a plusieurs destinataires : d'autres agents répondent aux annotations qui leur sont adressées. "+
					"Ne traitez que les vôtres : glose list --json --mine --agent %q ; glose done clôt votre part, Glose rendra la main.", agentName)
			}
			if asJSON {
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "  ")
				out := map[string]any{"seq": lastSeq, "state": st}
				if shared != "" {
					out["shared"] = shared
				}
				enc.Encode(out)
			} else {
				fmt.Fprintln(stdout, stateLine(st))
				fmt.Fprintf(stdout, "Curseur : %d (glose wait --after %d attend la remise suivante)\n", lastSeq, lastSeq)
				if shared != "" {
					fmt.Fprintln(stdout, shared)
				}
			}
			return ExitOK
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprintf(stderr, "glose : délai écoulé, la main est toujours à %s.\n", handNames[h.Hand()])
			return ExitTimeout
		}
		time.Sleep(500 * time.Millisecond)
	}
}

var decisionNames = map[string]string{navette.Accept: "accepté", navette.Reject: "rejeté", navette.Resolve: "résolu", navette.Withdraw: "retiré"}

func cmdLog(args []string, stdout, stderr io.Writer) int {
	var asJSON bool
	var turn int
	path, ok := parse("log", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&asJSON, "json", false, "")
		fs.IntVar(&turn, "turn", 0, "")
	})
	if !ok {
		return ExitUsage
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	h, err := navette.New(p).History()
	if err != nil {
		return failure(stderr, err)
	}
	turns := h.Log()
	if turn > 0 {
		var keep []navette.TurnLog
		for _, t := range turns {
			if t.Number == turn {
				keep = append(keep, t)
			}
		}
		turns = keep
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		problems := []journal.Problem{}
		problems = append(problems, h.Problems...)
		enc.Encode(map[string]any{"turns": turns, "problems": problems})
		return ExitOK
	}
	for _, t := range turns {
		if t.Actor == "" {
			fmt.Fprintf(stdout, "Tour %d — %s (en cours)\n", navette.Round(t.Number), partNames[h.Hand()])
		} else {
			// « Tour 1 — annotations de l'auteur, remises le … », « Tour 1 —
			// réponse de l'agent, remise le … » ; une main reprise par
			// l'auteur clôt la part de l'agent.
			line := partNames[t.Actor] + ", remises le "
			if t.Actor == "agent" {
				line = partNames["agent"] + ", remise le "
			}
			if t.Reclaimed {
				line = partNames["agent"] + ", main reprise par l'auteur le "
			}
			fmt.Fprintf(stdout, "Tour %d — %s%s", navette.Round(t.Number), line, when(t.Time))
			if t.Note != "" {
				fmt.Fprintf(stdout, " : « %s »", oneLine(t.Note, 100))
			}
			fmt.Fprintln(stdout)
		}
		for _, o := range t.Ops {
			created := ""
			if len(o.Created) > 0 {
				created = fmt.Sprintf(", %d identifiant%s créé%s", len(o.Created), plural(len(o.Created)), plural(len(o.Created)))
			}
			fmt.Fprintf(stdout, "  %s  %s (%s)%s\n", when(o.Time), opNames[o.Kind], actorNames[o.Actor], created)
			for _, c := range o.Closed {
				if c.Kind == "message" {
					fmt.Fprintf(stdout, "      message %s %s : %s\n", c.ID, decisionNames[c.Decision], oneLine(c.Text, 80))
					continue
				}
				what := "« " + oneLine(c.Text, 60) + " »"
				if c.Kind == "substitution" {
					what += " → « " + oneLine(c.New, 60) + " »"
				}
				id := c.ID
				if id == "" {
					id = "sans identifiant"
				}
				fmt.Fprintf(stdout, "      %s %s (%s) : %s\n", decisionNames[c.Decision], id, names[c.Kind], what)
			}
			for _, c := range o.Revised {
				what := "« " + oneLine(c.Text, 60) + " »"
				if c.Kind == "substitution" {
					what += " → « " + oneLine(c.New, 60) + " »"
				}
				done := "corrigée"
				if c.Kind == "message" {
					done = "modifié"
				}
				fmt.Fprintf(stdout, "      %s %s, version précédente (%s) : %s\n", c.ID, done, names[c.Kind], what)
			}
		}
	}
	for _, pb := range h.Problems {
		fmt.Fprintf(stdout, "Journal, ligne %d : %s\n", pb.Line, pb.Message)
	}
	return ExitOK
}

// partNames names the two parts of a round (a round: the author annotates,
// the agent answers).
var partNames = map[string]string{"author": "annotations de l'auteur", "agent": "réponse de l'agent"}

var opNames = map[string]string{"comment": "commentaire", "reply": "réponse", "propose": "proposition", "decide": "décision",
	"resolve": "fil résolu", "withdraw": "retrait", "identify": "identifiants ajoutés", "track": "modifications converties en propositions",
	"revise": "proposition corrigée", "amend": "message modifié", "undo": "décision annulée"}

var names = map[string]string{"comment": "commentaire", "highlight": "passage commenté", "addition": "ajout",
	"deletion": "suppression", "substitution": "remplacement", "message": "message"}

// ---------- Commands of the agent (P2.6 bis) ----------

// agentTarget reads the options that designate a place in a file.
type agentTarget struct {
	file, find, after string
	id                string
	text, new, note   string
	agent             string
	asJSON, whole     bool
}

func (t *agentTarget) flags(fs *flag.FlagSet) {
	fs.StringVar(&t.file, "file", "", "")
	fs.StringVar(&t.find, "find", "", "")
	fs.StringVar(&t.after, "after", "", "")
	fs.StringVar(&t.id, "id", "", "")
	fs.StringVar(&t.text, "text", "", "")
	fs.StringVar(&t.new, "new", "", "")
	fs.StringVar(&t.note, "note", "", "")
	fs.BoolVar(&t.asJSON, "json", false, "")
	fs.BoolVar(&t.whole, "whole", false, "")
	fs.StringVar(&t.agent, "agent", "", "")
}

// fileIndex returns the index of a file of the project.
func fileIndex(p *project.Project, rel string) (int, error) {
	rel = relPath(rel)
	for i, f := range p.Files {
		if f == rel {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%q n'est pas un fichier du projet (voir glose list)", rel)
}

// locate returns the place of text in src: its only occurrence outside any
// mark. The agent quotes the text as it is written in the file.
func locate(src []byte, text string) (critic.Span, error) {
	if text == "" {
		return critic.Span{}, errors.New("texte à chercher vide")
	}
	marks, _ := critic.Parse(src)
	var found []critic.Span
	for i := 0; ; {
		j := bytes.Index(src[i:], []byte(text))
		if j < 0 {
			break
		}
		s := critic.Span{Start: i + j, End: i + j + len(text)}
		inside := false
		for _, m := range marks {
			if s.Start < m.Span.End && s.End > m.Span.Start {
				inside = true
				break
			}
		}
		if !inside {
			found = append(found, s)
		}
		i += j + 1
	}
	switch len(found) {
	case 0:
		return critic.Span{}, fmt.Errorf("« %s » est introuvable hors des annotations ; citez le texte exactement comme il est écrit dans le fichier", oneLine(text, 60))
	case 1:
		return found[0], nil
	}
	return critic.Span{}, fmt.Errorf("« %s » figure %d fois : citez un passage plus long, qui ne figure qu'une fois", oneLine(text, 60), len(found))
}

// agentWrite runs one write of the agent and reports it.
func agentWrite(args []string, name, kind string, stdout, stderr io.Writer,
	check func(t *agentTarget) string,
	compute func(t *agentTarget, src []byte, gen *review.Gen) (patch.Edit, error)) int {
	var t agentTarget
	path, ok := parse(name, args, stderr, t.flags)
	if !ok {
		return ExitUsage
	}
	if err := fromStdin(map[string]*string{"text": &t.text, "new": &t.new, "note": &t.note, "find": &t.find, "after": &t.after}); err != nil {
		return usageError(stderr, name+" : "+err.Error())
	}
	if msg := check(&t); msg != "" {
		return usageError(stderr, name+" : "+msg)
	}
	agentName, _, err := agentIdentity(t.agent)
	if err != nil {
		return usageError(stderr, name+" : "+err.Error())
	}
	p, err := project.Load(path)
	if err != nil {
		return failure(stderr, err)
	}
	touchAgent(p, agentName, navette.ActWrite)
	n := navette.New(p)
	file := -1
	if t.file != "" {
		if file, err = fileIndex(p, t.file); err != nil {
			return failure(stderr, err)
		}
	} else if t.id != "" {
		// A reply: the file is the one that holds the annotation.
		for i := range p.Files {
			if b, err := os.ReadFile(p.Path(i)); err == nil {
				if _, ok := review.Read(b).Find(t.id); ok {
					file = i
					break
				}
			}
		}
		if file < 0 {
			return failure(stderr, fmt.Errorf("aucune annotation %s dans le projet (voir glose list)", t.id))
		}
	}
	before, _ := os.ReadFile(p.Path(file))
	via, model := viaName(agentName), agentModel(p, agentName)
	res, err := n.Do(review.Agent, file, "", kind, "", func(src []byte, gen *review.Gen) (patch.Edit, error) {
		e, err := compute(&t, src, gen)
		if err != nil || (via == "" && model == "") {
			return e, err
		}
		// The agent that wrote it, and its model when known, on each
		// message and proposal (D-49, P10.11).
		return review.SignModel(src, e, via, model)
	})
	if err != nil {
		return failure(stderr, agentMessage(err))
	}
	after, _ := os.ReadFile(p.Path(file))
	created, _ := navette.Compare(p.Files[file], before, after, "")
	line, _ := render.LineCol(after, res.Edit.Start)
	if t.asJSON {
		enc := json.NewEncoder(stdout)
		enc.Encode(map[string]any{"file": p.Files[file], "line": line, "created": created})
		return ExitOK
	}
	sort.Strings(created)
	ids := ""
	if len(created) > 0 {
		ids = fmt.Sprintf(" (identifiant%s %s)", plural(len(created)), strings.Join(created, ", "))
	}
	fmt.Fprintf(stdout, "%s : %s, ligne %d%s.\n", opNames[kind], p.Files[file], line, ids)
	return ExitOK
}

func agentMessage(err error) error {
	switch {
	case errors.Is(err, patch.ErrOverlap):
		return errors.New("le passage touche une annotation existante")
	case errors.Is(err, patch.ErrAdjacent):
		return errors.New("une annotation existante est collée à cet endroit : répondez dans son fil (glose reply)")
	case errors.Is(err, patch.ErrDelimiter):
		return errors.New("le texte contient une suite de signes réservée aux annotations ({++, {>>, <<}…)")
	case errors.Is(err, patch.ErrNeighbour):
		return errors.New("une annotation mal formée se trouve dans le voisinage (glose check)")
	}
	return err
}

func cmdComment(args []string, stdout, stderr io.Writer) int {
	return agentWrite(args, "comment", "comment", stdout, stderr,
		func(t *agentTarget) string {
			switch {
			case t.file == "":
				return "--file est requis"
			case strings.TrimSpace(t.text) == "":
				return "--text est requis"
			case (t.find == "") == (t.after == ""):
				return "indiquez le passage commenté (--find) ou le point après lequel commenter (--after)"
			}
			return ""
		},
		func(t *agentTarget, src []byte, gen *review.Gen) (patch.Edit, error) {
			s, err := locate(src, t.find+t.after)
			if err != nil {
				return patch.Edit{}, err
			}
			if t.after != "" {
				s.Start = s.End
			}
			return review.Comment(src, s, strings.TrimSpace(t.text), review.Agent, gen)
		})
}

func cmdPropose(args []string, stdout, stderr io.Writer) int {
	return agentWrite(args, "propose", "propose", stdout, stderr,
		func(t *agentTarget) string {
			switch {
			case t.whole && t.find == "":
				return "--whole s'emploie avec --find (une réécriture du passage, proposée d'un tenant)"
			case t.id != "":
				if t.find != "" || t.after != "" {
					return "--id désigne déjà le passage : sans --find ni --after"
				}
				return ""
			case t.file == "":
				return "--file est requis (ou --id pour un passage commenté)"
			case (t.find == "") == (t.after == ""):
				return "indiquez le passage à remplacer ou supprimer (--find), le point après lequel ajouter (--after), ou le passage commenté (--id)"
			case t.after != "" && t.new == "":
				return "--new est requis pour un ajout"
			}
			return ""
		},
		func(t *agentTarget, src []byte, gen *review.Gen) (patch.Edit, error) {
			if t.id != "" {
				if a, ok := review.Read(src).Find(t.id); ok && a.Kind != critic.Highlight && a.Kind != critic.Comment {
					return fixProposal(src, t.id, t.new, strings.TrimSpace(t.note), gen)
				}
				return rewriteHighlight(src, t.id, t.new, strings.TrimSpace(t.note), gen)
			}
			s, err := locate(src, t.find+t.after)
			if err != nil {
				return patch.Edit{}, err
			}
			if t.after != "" {
				s.Start = s.End
				// Just before a comment, an addition would take its thread:
				// it goes after the comment and its thread (a comment is
				// invisible: the views are the same). Before a highlighted
				// passage or a proposal, it stays where it was asked.
				if a, ok := review.Read(src).At(s.Start); ok && a.Kind == critic.Comment {
					s = critic.Span{Start: a.Span.End, End: a.Span.End}
				}
			}
			propose := review.Propose
			if t.whole {
				propose = review.ProposeWhole
			}
			e, _, err := propose(src, s, t.new, strings.TrimSpace(t.note), review.Agent, gen)
			return e, err
		})
}

// fixProposal adds to the thread of a proposal of the author a correction:
// what the proposal should propose instead, which the author may apply in the
// interface (« Accepter cette version »). An optional note goes before it.
//
// A proposal of the agent itself is corrected in place (D-31): same mark,
// same identity, same thread; the note goes at the end of its thread.
func fixProposal(src []byte, id, text, note string, gen *review.Gen) (patch.Edit, error) {
	if a, ok := review.Read(src).Find(id); ok && a.By == review.Agent {
		e, _, err := review.Revise(src, 0, id, text, review.Agent, gen)
		if err != nil {
			return patch.Edit{}, err
		}
		out := e.Apply(src)
		if note != "" {
			r, _, err := review.Reply(out, 0, id, note, review.Agent, gen)
			if err != nil {
				return patch.Edit{}, err
			}
			out = r.Apply(out)
		}
		return review.Diff(src, out), nil
	}
	out := src
	if note != "" {
		r, _, err := review.Reply(src, 0, id, note, review.Agent, gen)
		if err != nil {
			return patch.Edit{}, err
		}
		out = r.Apply(src)
	}
	f, _, err := review.Fix(out, id, text, review.Agent, gen)
	if err != nil {
		return patch.Edit{}, err
	}
	return review.Diff(src, f.Apply(out)), nil
}

// rewriteHighlight rewrites the passage of a highlight annotation (D-26): the
// file holds the new passage inside the highlight until « glose done », which
// turns it into a proposal that takes over the thread.
func rewriteHighlight(src []byte, id, text, note string, gen *review.Gen) (patch.Edit, error) {
	d := review.Read(src)
	a, ok := d.Find(id)
	if !ok {
		return patch.Edit{}, review.ErrNotFound
	}
	if strings.TrimSpace(text) == "" {
		return patch.Edit{}, errors.New("--new vide : un passage commenté ne se supprime pas ainsi ; répondez dans le fil, ou proposez un texte de remplacement")
	}
	if a.Kind != critic.Highlight {
		return patch.Edit{}, fmt.Errorf("l'annotation %s n'est pas un passage commenté : utilisez --file et --find", id)
	}
	if critic.ContainsDelimiter([]byte(text)) || strings.Contains(text, critic.Arrow) || strings.Contains(string(src[a.Mark.Text.Start:a.Mark.Text.End]), critic.Arrow) {
		return patch.Edit{}, patch.ErrDelimiter
	}
	e := patch.Edit{Start: a.Mark.Text.Start, End: a.Mark.Text.End, Insert: []byte(text)}
	out := e.Apply(src)
	if after, ok := review.Read(out).Find(id); !ok || after.Kind != critic.Highlight || len(review.Read(out).Annotations) != len(d.Annotations) {
		return patch.Edit{}, patch.ErrNeighbour
	}
	if note == "" {
		return e, nil
	}
	r, _, err := review.Reply(out, 0, id, note, review.Agent, gen)
	if err != nil {
		return patch.Edit{}, err
	}
	return review.Diff(src, r.Apply(out)), nil
}

func cmdReply(args []string, stdout, stderr io.Writer) int {
	return agentWrite(args, "reply", "reply", stdout, stderr,
		func(t *agentTarget) string {
			switch {
			case t.id == "":
				return "--id est requis (identifiant de l'annotation, voir glose list)"
			case strings.TrimSpace(t.text) == "":
				return "--text est requis"
			}
			return ""
		},
		func(t *agentTarget, src []byte, gen *review.Gen) (patch.Edit, error) {
			e, _, err := review.Reply(src, 0, t.id, strings.TrimSpace(t.text), review.Agent, gen)
			return e, err
		})
}
