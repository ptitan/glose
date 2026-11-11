package navette

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/notes"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// opWait is how long an operation waits for the lock of the project.
const opWait = 30 * time.Second

// Errors of the operations.
var (
	ErrConflict = errors.New("le fichier a changé sur le disque depuis sa lecture")
	ErrFile     = errors.New("fichier inconnu")
	ErrAgentNot = errors.New("ce n'est pas le tour de l'agent : l'auteur ne lui a pas remis la main (glose wait attend la remise)")
)

// Compute computes an edit of a file from its content; gen gives the new
// identifiers.
type Compute func(src []byte, gen *review.Gen) (patch.Edit, error)

// Result is the outcome of an operation.
type Result struct {
	Hash string     // hash of the file after the operation
	Edit patch.Edit // the edit made
}

// Do runs one operation of actor on file, under the lock of the project: it
// checks the turn (the author does not write while the agent has the hand,
// and the agent not while the author has it), reads the file and checks its
// hash (if hash is not empty), computes the edit, then records it in the
// journal and writes it. kind names the operation in the journal; decision
// what the closing of an annotation means.
func (n *Navette) Do(actor string, file int, hash, kind, decision string, compute Compute) (*Result, error) {
	return n.DoWith(actor, file, hash, kind, decision, func(src []byte, gen *review.Gen, _ *History) (patch.Edit, error) {
		return compute(src, gen)
	})
}

// ComputeWith is Compute, given also the history read under the lock (to
// know, for instance, what the last handover gave to the other party).
type ComputeWith func(src []byte, gen *review.Gen, h *History) (patch.Edit, error)

// DoWith is Do, with the history passed to compute.
func (n *Navette) DoWith(actor string, file int, hash, kind, decision string, compute ComputeWith) (*Result, error) {
	return n.Run(actor, file, hash, &Spec{Kind: kind, Decision: decision}, compute)
}

// Run is DoWith, the operation described by spec, which compute may complete
// (the direct changes it makes, those it undoes): it is read after compute.
func (n *Navette) Run(actor string, file int, hash string, spec *Spec, compute ComputeWith) (*Result, error) {
	if file < 0 || file >= len(n.P.Files) {
		return nil, ErrFile
	}
	lock, err := n.Lock(opWait)
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	if err := n.prepare(); err != nil {
		return nil, err
	}
	h, err := n.History()
	if err != nil {
		return nil, err
	}
	if hand := h.Hand(); hand != actor {
		if actor == review.Author {
			return nil, ErrAgentTurn
		}
		return nil, ErrAgentNot
	}
	d, err := store.Read(n.P.Path(file))
	if err != nil {
		return nil, err
	}
	if hash != "" && d.Hash != hash {
		return nil, ErrConflict
	}
	gen := n.Gen(n.ReadFiles(), h)
	e, err := compute(d.Bytes, gen, h)
	if err != nil {
		return nil, err
	}
	if actor == review.Agent {
		e, err = review.LabelTask(d.Bytes, e, h.ActiveTask())
		if err != nil {
			return nil, err
		}
	}
	// Only the author changes the text directly (D-14, D-57).
	if (len(spec.Direct) > 0 || len(spec.Reverted) > 0) && actor != review.Author {
		return nil, ErrDirectAgent
	}
	for i := range spec.Direct {
		spec.Direct[i].File = n.P.Files[file]
	}
	after := e.Apply(d.Bytes)
	if err := n.commit(actor, spec, "", []Change{{File: file, Before: d.Bytes, After: after}}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return &Result{Hash: store.Hash(after), Edit: e}, nil
}

// ErrNoProposal is returned when no proposal lies where a decision is asked.
var ErrNoProposal = errors.New("proposition introuvable")

// DecideRange accepts or rejects (decision) the proposal that starts at start,
// or, if end > start, every proposal inside [start, end). It is the author's
// only (D-14). It returns one edit of src.
func DecideRange(src []byte, start, end int, decision string) (patch.Edit, error) {
	return DecideVia(src, start, end, decision, "")
}

// DecideVia restricts the existing batch decision to one signature.
func DecideVia(src []byte, start, end int, decision, via string) (patch.Edit, error) {
	allowed := map[int]bool{}
	for _, a := range review.Read(src).Annotations {
		if via == "" || a.Via == via {
			allowed[a.Span.Start] = true
		}
	}
	op := map[string]func([]byte, []critic.Mark, critic.Annotation) (patch.Edit, error){
		Accept: patch.Accept, Reject: patch.Reject,
	}[decision]
	if op == nil {
		return patch.Edit{}, fmt.Errorf("décision inconnue : %q", decision)
	}
	marks, _ := critic.Parse(src)
	var targets []critic.Annotation
	for _, a := range critic.Group(marks) {
		if !allowed[a.Span.Start] || !isProposal(marks[a.Main].Kind) {
			continue
		}
		if (end > start && a.Span.Start >= start && a.Span.End <= end) || a.Span.Start == start {
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		return patch.Edit{}, ErrNoProposal
	}
	// A note is one proposal: its reference and its definition are decided
	// together (package notes).
	links := notes.Links(src)
	chosen := map[int]bool{}
	for _, a := range targets {
		chosen[a.Span.Start] = true
	}
	for _, a := range append([]critic.Annotation(nil), targets...) {
		for _, p := range links[a.Span.Start].Partners {
			if !chosen[p] && allowed[p] {
				chosen[p] = true
				for _, b := range critic.Group(marks) {
					if b.Span.Start == p {
						targets = append(targets, b)
					}
				}
			}
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Span.Start < targets[j].Span.Start })
	out := src
	for i := len(targets) - 1; i >= 0; i-- {
		cur, _ := critic.Parse(out)
		a, _, err := patch.Find(out, targets[i].Span.Start)
		if err != nil {
			return patch.Edit{}, ErrNoProposal
		}
		e, err := op(out, cur, a)
		if err != nil {
			return patch.Edit{}, err
		}
		out = e.Apply(out)
	}
	first, last := targets[0].Span.Start, targets[len(targets)-1].Span.End
	tail := len(src) - last
	return patch.Edit{Start: first, End: last, Insert: append([]byte(nil), out[first:len(out)-tail]...)}, nil
}

// ResolveAt closes the discussion (comment or highlighted passage) that starts
// at start: the passage stays, the marks and the thread go to the journal.
func ResolveAt(src []byte, start int) (patch.Edit, error) {
	a, marks, err := patch.Find(src, start)
	if err != nil {
		return patch.Edit{}, review.ErrNotFound
	}
	return patch.Resolve(src, marks, a)
}

// Count returns the number of proposals inside [start, end).
func Count(src []byte, start, end int) int {
	marks, _ := critic.Parse(src)
	n := 0
	for _, a := range critic.Group(marks) {
		if isProposal(marks[a.Main].Kind) && a.Span.Start >= start && a.Span.End <= end {
			n++
		}
	}
	return n
}
