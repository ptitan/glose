// Package navette runs the exchange between the author and the agent (SPEC
// F13 to F18): whose turn it is, the operations on the files recorded in the
// journal, the handover of a turn, the status of each annotation, and the
// annotations that disappeared without a decision (D-14).
//
// Every change of a file goes through Apply or a handover, under the lock of
// the project, and is recorded in the journal by the two-step protocol of
// package journal. The present lives in the files, the history in the journal
// (D-13): the state computed here is never stored.
package navette

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// Navette is the exchange of one project.
type Navette struct {
	P *project.Project
	J *journal.Journal
}

// New returns the exchange of p.
func New(p *project.Project) *Navette {
	return &Navette{P: p, J: journal.Open(p.State())}
}

// Errors of the handover.
var (
	ErrNotYourTurn = errors.New("ce n'est pas votre tour")
	ErrAgentTurn   = errors.New("c'est le tour de l'agent : attendez qu'il remette le sien (glose done), ou reprenez la main")
)

// Other returns the other actor.
func Other(actor string) string {
	if actor == review.Agent {
		return review.Author
	}
	return review.Agent
}

// Lock takes the write lock of the project, waiting at most d.
func (n *Navette) Lock(d time.Duration) (*store.Lock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	l, err := store.WaitLock(ctx, n.P.State())
	if err != nil {
		return nil, fmt.Errorf("le projet est occupé par une autre écriture : %w", err)
	}
	return l, nil
}

// History is what the journal says, read once.
type History struct {
	Events   []journal.Event
	Problems []journal.Problem
	Turns    []journal.Event
	Ops      []journal.EffectiveOp
	Pending  []journal.Op
}

// History reads the journal.
func (n *Navette) History() (*History, error) {
	ev, pb, err := n.J.Read()
	if err != nil {
		return nil, err
	}
	return &History{Events: ev, Problems: pb, Turns: journal.Turns(ev), Ops: journal.Effective(ev), Pending: journal.Pending(ev)}, nil
}

// Hand returns who has the hand: the author at the start, then the other
// actor of the last handover.
func (h *History) Hand() string {
	if len(h.Turns) == 0 {
		return review.Author
	}
	return Other(h.Turns[len(h.Turns)-1].Actor)
}

// Turn returns the number of the current turn, from 1.
func (h *History) Turn() int { return len(h.Turns) + 1 }

// Last returns the last handover, or nil.
func (h *History) Last() *journal.Event {
	if len(h.Turns) == 0 {
		return nil
	}
	return &h.Turns[len(h.Turns)-1]
}

// Born returns, for every identifier created by an operation or present at a
// handover, the turn in which it first appeared.
func (h *History) Born() map[string]int {
	born := map[string]int{}
	turn := 1
	ti, oi := 0, 0
	// Walk turns and operations in the order of the journal.
	for ti < len(h.Turns) || oi < len(h.Ops) {
		if oi < len(h.Ops) && (ti == len(h.Turns) || h.Ops[oi].Seq < h.Turns[ti].Seq) {
			for _, id := range h.Ops[oi].Created {
				if _, ok := born[id]; !ok {
					born[id] = turn
				}
			}
			oi++
			continue
		}
		for _, id := range h.Turns[ti].TurnEv.IDs {
			if _, ok := born[id]; !ok {
				born[id] = turn
			}
		}
		turn++
		ti++
	}
	return born
}

// Closed returns the identifiers closed by an operation.
func (h *History) Closed() map[string]journal.Closed {
	out := map[string]journal.Closed{}
	for _, op := range h.Ops {
		// An identifier that comes back (a decision undone) is open again.
		for _, id := range op.Created {
			delete(out, id)
		}
		for _, c := range op.Closed {
			if c.ID != "" {
				out[c.ID] = c
			}
			// The messages of a closed thread are closed with it.
			for _, m := range c.Messages {
				if m.ID != "" && m.ID != c.ID {
					out[m.ID] = journal.Closed{ID: m.ID, File: c.File, Decision: c.Decision, Kind: "message", By: m.By, Via: m.Via, Model: m.Model, Text: m.Text}
				}
			}
		}
	}
	return out
}

// Files is the present: the content of every file of the project.
type Files struct {
	Docs  []store.Doc
	Errs  []error
	Annot [][]render.Annotation
}

// ReadFiles reads every file of the project.
func (n *Navette) ReadFiles() *Files {
	f := &Files{}
	for i := range n.P.Files {
		d, err := store.Read(n.P.Path(i))
		if err == nil && !utf8.Valid(d.Bytes) {
			err = errors.New("le fichier n'est pas en UTF-8 valide")
		}
		f.Docs = append(f.Docs, d)
		f.Errs = append(f.Errs, err)
		var anns []render.Annotation
		if err == nil {
			anns = render.Annotations(i, d.Bytes)
		}
		f.Annot = append(f.Annot, anns)
	}
	return f
}

// IDs returns the identifiers present in the files.
func (f *Files) IDs() map[string]bool {
	out := map[string]bool{}
	for i, d := range f.Docs {
		if f.Errs[i] != nil {
			continue
		}
		for _, id := range review.Read(d.Bytes).IDs() {
			out[id] = true
		}
	}
	return out
}

// Gen returns a generator of identifiers that avoids those of the files and
// of the journal.
func (n *Navette) Gen(f *Files, h *History) *review.Gen {
	reserved := map[string]bool{}
	if h != nil {
		for id := range h.Born() {
			reserved[id] = true
		}
		for id := range h.Closed() {
			reserved[id] = true
		}
	}
	return review.NewGenLive(f.IDs(), reserved)
}

// Statuses of an annotation.
const (
	StatusNew  = "new"  // created during the current turn, not handed over yet
	StatusOpen = "open" // handed over, waiting for an answer or a decision
)

// HandedOver returns the identifiers (annotations and replies) that the last
// handover gave to the other party: those are no longer « new ».
func (h *History) HandedOver() map[string]bool {
	out := map[string]bool{}
	if l := h.Last(); l != nil {
		for _, id := range l.TurnEv.IDs {
			out[id] = true
		}
	}
	return out
}

// Annotate fills the status and the turn of birth of the annotations, and the
// status of each message of their threads.
func Annotate(anns []render.Annotation, h *History) {
	born := h.Born()
	handed := h.HandedOver()
	status := func(id string) string {
		if id != "" && handed[id] {
			return StatusOpen
		}
		return StatusNew
	}
	for i := range anns {
		a := &anns[i]
		a.Turn = h.Turn()
		if t, ok := born[a.ID]; ok && a.ID != "" {
			a.Turn = t
		}
		a.Status = status(a.ID)
		for k := range a.Messages {
			a.Messages[k].Status = status(a.Messages[k].ID)
		}
	}
}

// Disappeared returns the identifiers known to the journal, absent from the
// files, and closed by no decision (D-14): never taken for accepted.
func Disappeared(f *Files, h *History) []string {
	present := f.IDs()
	closed := h.Closed()
	var out []string
	for id := range h.Born() {
		if !present[id] {
			if _, ok := closed[id]; !ok {
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// State is the state of the exchange, for « glose status » and the banner of
// the interface (SPEC F14).
type State struct {
	Task        *journal.TaskInfo `json:"task,omitempty"`
	Turn        int               `json:"turn"`  // part in progress, from 1 (journal numbering: one per handover)
	Round       int               `json:"round"` // round shown to the author: her part and the agent's answer (Round)
	Part        string            `json:"part"`  // author or agent: whose part of the round is in progress
	Hand        string            `json:"hand"`  // author or agent
	Since       string            `json:"since,omitempty"`
	LastActor   string            `json:"lastActor,omitempty"`
	Note        string            `json:"note,omitempty"`
	Reclaimed   bool              `json:"reclaimed,omitempty"`
	Annotations int               `json:"annotations"`
	Kinds       map[string]int    `json:"kinds"`
	Statuses    map[string]int    `json:"statuses"`
	// ThisTurn counts what the party who has the hand wrote during this
	// turn, not handed over yet: "comments", "proposals", "replies" (the
	// handover button shows it, P9.3).
	ThisTurn map[string]int `json:"thisTurn"`
	Issues   int            `json:"issues"`
	Problems []string       `json:"problems"`
	// Agent is the presence of the agent (P10.1), or null if no agent ever
	// gave a sign; not normative (.glose/agent.json).
	Agent *AgentState `json:"agent"`
	// Imposed lists the direct changes of the author's last part (D-57):
	// written in the text, never to be discussed by the agent.
	Imposed []journal.Closed `json:"imposed"`
}

// Round gives the round of a turn as the author counts them: she annotates,
// the agent answers, that is one round. The journal numbers each part (one
// per handover, the author's parts odd, the agent's even, since the hand
// alternates); a round holds two of them.
func Round(turn int) int { return (turn + 1) / 2 }

// State computes the state of the exchange, without writing anything.
func (n *Navette) State() (State, *History, *Files, error) {
	h, err := n.History()
	if err != nil {
		return State{}, nil, nil, err
	}
	f := n.ReadFiles()
	st := State{Turn: h.Turn(), Round: Round(h.Turn()), Part: h.Hand(), Hand: h.Hand(), Kinds: map[string]int{}, Statuses: map[string]int{}, ThisTurn: map[string]int{}, Problems: []string{}}
	if l := h.Last(); l != nil {
		st.Since, st.LastActor, st.Note, st.Reclaimed = l.Time, l.Actor, l.TurnEv.Note, l.TurnEv.Reclaimed
	}
	if l := h.Last(); l != nil && l.Actor == review.Author && st.Hand == review.Agent {
		st.Task = l.TurnEv.Task
	}
	st.Agent = n.Agent(st.Hand, st.Since)
	st.Imposed = h.Imposed()
	var all []render.Annotation
	for i := range n.P.Files {
		if f.Errs[i] != nil {
			st.Issues++
			continue
		}
		st.Issues += len(render.Issues(i, f.Docs[i].Bytes))
		all = append(all, f.Annot[i]...)
	}
	st.Issues += len(render.CrossDuplicates(all))
	Annotate(all, h)
	// Written by the hand (or by hand, without identity yet: it will be
	// attributed to the hand at the handover, D-19).
	mine := func(by string) bool { return by == st.Hand || by == "" }
	for _, a := range all {
		st.Annotations++
		st.Kinds[a.Kind]++
		st.Statuses[a.Status]++
		// A footnote added or deleted is two linked proposals, one card.
		linked := len(a.Partners) > 0 && !a.Primary
		if a.Status == StatusNew && mine(a.By) && !linked {
			if a.Kind == "comment" || a.Kind == "highlight" {
				st.ThisTurn["comments"]++
			} else {
				st.ThisTurn["proposals"]++
			}
		}
		for _, m := range a.Messages {
			if m.Reply && !m.Fix && m.Status == StatusNew && mine(m.By) {
				st.ThisTurn["replies"]++
			}
		}
	}
	for _, p := range h.Problems {
		st.Problems = append(st.Problems, fmt.Sprintf("journal, ligne %d : %s", p.Line, p.Message))
	}
	for _, op := range h.Pending {
		st.Problems = append(st.Problems, fmt.Sprintf("opération %s (%s) préparée et jamais confirmée : elle sera réglée à la prochaine écriture", op.ID, op.Kind))
	}
	// An operation found half done at a restart, since the last handover:
	// its files may hold part of it (journal.Reconcile).
	var since int64
	if l := h.Last(); l != nil {
		since = l.Seq
	}
	for _, e := range journal.Partials(h.Events) {
		if e.Seq > since {
			st.Problems = append(st.Problems, fmt.Sprintf("%s ; fichiers : %s", e.Reason, strings.Join(e.Files, ", ")))
		}
	}
	// A file opened alone (glose open notes.md): the annotations of the other
	// files of the project are not displayed. Said, never hidden (D-38); and
	// disappearances are judged on the whole project, not on this file.
	whole := f
	if n.P.Source != n.P.Root {
		if sc, err := n.Scope(h); err == nil {
			w := New(sc)
			whole = w.ReadFiles()
			mine := map[string]bool{}
			for _, p := range n.P.Files {
				mine[p] = true
			}
			count, where := 0, []string{}
			for i, p := range sc.Files {
				if mine[p] || whole.Errs[i] != nil || len(whole.Annot[i]) == 0 {
					continue
				}
				count += len(whole.Annot[i])
				where = append(where, p)
			}
			if count > 0 {
				st.Problems = append(st.Problems, fmt.Sprintf("%d annotation(s) dans d'autres fichiers du projet (%s), invisibles quand ce fichier est ouvert seul : ouvrez le projet entier (glose open dans %s)",
					count, strings.Join(where, ", "), n.P.Root))
			}
		}
	}
	for _, id := range Disappeared(whole, h) {
		st.Problems = append(st.Problems, fmt.Sprintf("annotation %s disparue sans décision (D-14) : elle n'est pas tenue pour acceptée", id))
	}
	return st, h, f, nil
}

// prepare settles the operations left pending by an interruption. It is
// called under the lock before any write.
func (n *Navette) prepare() error {
	_, _, err := n.J.Reconcile(func(rel string) string { return n.P.Abs(rel) })
	return err
}

// Change is the new content of one file of the project.
type Change struct {
	File   int
	Before []byte
	After  []byte
}

// Commit records an operation and writes its files, under the lock held by
// the caller: the created and closed identifiers are computed from the
// contents before and after. decision names what the closing of an
// annotation means (accept, reject, resolve, withdraw).
func (n *Navette) Commit(actor, kind, decision, note string, changes []Change) error {
	return n.commit(actor, &Spec{Kind: kind, Decision: decision}, note, changes)
}

// commit is Commit, for the operation that spec describes.
func (n *Navette) commit(actor string, spec *Spec, note string, changes []Change) error {
	if err := n.prepare(); err != nil {
		return err
	}
	kind, decision := spec.Kind, spec.Decision
	op := journal.Op{Kind: kind, Note: note, Reverted: spec.Reverted}
	var jc []journal.Change
	for _, c := range changes {
		path := n.P.Files[c.File]
		// Checked when the project was loaded, and again here, under the
		// lock: a file replaced since by a link to the outside is not written.
		if _, _, err := n.P.Inside(path); err != nil {
			return err
		}
		created, closed := Compare(path, c.Before, c.After, decision)
		op.Created = append(op.Created, created...)
		op.Closed = append(op.Closed, closed...)
		op.Revised = append(op.Revised, Revisions(path, c.Before, c.After)...)
		jc = append(jc, journal.Change{Path: path, Abs: n.P.Path(c.File), Before: c.Before, After: c.After})
	}
	op.Closed = append(op.Closed, spec.Direct...)
	_, err := n.J.Commit(actor, op, jc)
	return err
}

// Compare returns the identifiers that appear in after, and a copy of the
// annotations and messages of before that are absent from after.
func Compare(path string, before, after []byte, decision string) ([]string, []journal.Closed) {
	b, a := review.Read(before), review.Read(after)
	now := map[string]bool{}
	for _, id := range a.IDs() {
		now[id] = true
	}
	was := map[string]bool{}
	for _, id := range b.IDs() {
		was[id] = true
	}
	var created []string
	for _, id := range a.IDs() {
		if !was[id] {
			created = append(created, id)
		}
	}
	var closed []journal.Closed
	// Annotations of before that are gone (by identity, or by position for
	// those without identity).
	for _, ann := range b.Annotations {
		gone := false
		if ann.ID != "" {
			gone = !now[ann.ID]
		} else {
			_, revised := revisedIn(b, ann, a)
			gone = !sameAnnotationIn(before, ann, a) && !revised
		}
		if gone {
			closed = append(closed, copyOf(path, before, ann, decision))
			continue
		}
		for _, m := range ann.Replies() {
			if m.ID != "" && !now[m.ID] {
				closed = append(closed, journal.Closed{ID: m.ID, File: path, Decision: Withdraw, Kind: "message", By: m.By,
					Via: m.Via, Model: m.Model, Text: string(before[m.Mark.Text.Start:m.Mark.Text.End])})
			}
		}
	}
	return created, closed
}

// Revisions returns a copy of the proposals of before whose identity is still
// in after, but whose mark has changed (a correction, P3.2), and of the
// messages whose text has changed (an amended message, P9.1): their previous
// version, kept by the journal.
func Revisions(path string, before, after []byte) []journal.Closed {
	b, a := review.Read(before), review.Read(after)
	var out []journal.Closed
	for _, old := range b.Annotations {
		out = append(out, amendedMessages(path, b, old, a)...)
		if !isProposal(old.Kind) {
			continue
		}
		var now review.Annotation
		ok := false
		if old.ID != "" {
			now, ok = a.Find(old.ID)
		} else if !sameAnnotationIn(before, old, a) {
			now, ok = revisedIn(b, old, a)
		}
		if !ok || bytes.Equal(before[old.Mark.Span.Start:old.Mark.Span.End], after[now.Mark.Span.Start:now.Mark.Span.End]) {
			continue
		}
		c := copyOf(path, before, old, Revise)
		c.ID = now.ID // a proposal written by hand receives its identity with the correction
		out = append(out, c)
	}
	return out
}

// amendedMessages returns a copy of the messages of the annotation old (in
// the text b) that keep their identity in a but whose text has changed (an
// amended message, P9.1): their previous version, kept by the journal. The
// head of a comment or of a highlighted passage shares the identity of the
// annotation; replies have their own. Messages without identity are not
// followed: they receive one when they are amended, and appear then.
func amendedMessages(path string, b *review.Doc, old review.Annotation, a *review.Doc) []journal.Closed {
	if old.ID == "" {
		return nil
	}
	now, ok := a.Find(old.ID)
	if !ok {
		return nil
	}
	text := func(d *review.Doc, m review.Message) string { return string(d.Src[m.Mark.Text.Start:m.Mark.Text.End]) }
	var out []journal.Closed
	add := func(m review.Message, was string) {
		out = append(out, journal.Closed{ID: m.ID, File: path, Decision: Revise, Kind: "message", By: m.By, Via: m.Via, Model: m.Model, Text: was})
	}
	oh, nh := len(old.Messages)-len(old.Replies()), len(now.Messages)-len(now.Replies())
	for k := 0; k < oh && k < nh; k++ {
		if was := text(b, old.Messages[k]); was != text(a, now.Messages[k]) {
			add(old.Messages[k], was)
		}
	}
	replies := map[string]review.Message{}
	for _, m := range now.Replies() {
		if m.ID != "" {
			replies[m.ID] = m
		}
	}
	for _, m := range old.Replies() {
		if n, ok := replies[m.ID]; ok && m.ID != "" && text(b, m) != text(a, n) {
			add(m, text(b, m))
		}
	}
	return out
}

// revisedIn finds, for a proposal without identity, its corrected version in
// a: a proposal that starts at the same place, receives a new identity there,
// and keeps the old text (a correction changes only what is proposed).
func revisedIn(b *review.Doc, ann review.Annotation, a *review.Doc) (review.Annotation, bool) {
	if !isProposal(ann.Kind) {
		return review.Annotation{}, false
	}
	known := map[string]bool{}
	for _, id := range b.IDs() {
		known[id] = true
	}
	old := b.Src[ann.Mark.Text.Start:ann.Mark.Text.End]
	for _, x := range a.Annotations {
		// The identity must be new: a neighbour already known is another proposal.
		if x.Span.Start != ann.Span.Start || x.ID == "" || known[x.ID] || !isProposal(x.Kind) {
			continue
		}
		if ann.Kind == critic.Addition && x.Kind == critic.Addition ||
			ann.Kind != critic.Addition && x.Kind != critic.Addition && bytes.Equal(a.Src[x.Mark.Text.Start:x.Mark.Text.End], old) {
			return x, true
		}
	}
	return review.Annotation{}, false
}

// sameAnnotationIn tells whether an annotation without identity is still in
// the text a: same mark, same content.
func sameAnnotationIn(src []byte, ann review.Annotation, a *review.Doc) bool {
	want := string(src[ann.Mark.Span.Start:ann.Mark.Span.End])
	for _, x := range a.Annotations {
		if string(a.Src[x.Mark.Span.Start:x.Mark.Span.End]) == want {
			return true
		}
	}
	return false
}

func copyOf(path string, src []byte, a review.Annotation, decision string) journal.Closed {
	m := a.Mark
	c := journal.Closed{ID: a.ID, File: path, Decision: decision, Kind: render.KindName(m.Kind), By: a.By, Task: a.Task,
		Via: a.Via, Model: a.Model, Text: string(src[m.Text.Start:m.Text.End]), New: string(src[m.New.Start:m.New.End])}
	for _, msg := range a.Messages {
		c.Messages = append(c.Messages, journal.Message{ID: msg.ID, By: msg.By, Via: msg.Via, Model: msg.Model, Task: msg.Task,
			Text: string(src[msg.Mark.Text.Start:msg.Mark.Text.End])})
	}
	return c
}

// Decisions, as recorded in the journal.
const (
	Accept   = "accept"
	Reject   = "reject"
	Resolve  = "resolve"
	Withdraw = "withdraw"
	// Revise is not a decision: it names, in the journal, the previous
	// version of a corrected proposal (journal.Op.Revised).
	Revise = "revise"
)

// isProposal reports whether a kind is a proposal.
func isProposal(k critic.Kind) bool {
	return k == critic.Addition || k == critic.Deletion || k == critic.Substitution
}

// ActiveTask is fixed by the author’s handover, never by the process environment.
func (h *History) ActiveTask() string {
	if h.Hand() == review.Agent {
		if l := h.Last(); l != nil && l.TurnEv.Task != nil {
			return l.TurnEv.Task.ID
		}
	}
	return ""
}
