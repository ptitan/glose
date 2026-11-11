package navette

import (
	"errors"
	"sort"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
)

// Direct names, in the journal, a change that the author imposes on the text
// (D-57): the kind of the operation, and the decision of the copies it keeps.
// It is not a proposal: nothing is left in the file to decide, and the agent
// is never asked about it. Only the author makes one.
const Direct = "direct"

// Undo names, in the journal, the undoing of a decision of the author.
const Undo = "undo"

// Spec describes an operation for the journal: its kind, what the closing of
// an annotation means, and what the operation adds to the copies computed
// from the files.
type Spec struct {
	Kind     string
	Decision string
	// Direct holds the copies of the changes imposed by the author (D-57),
	// recorded with the closed annotations, decision « direct ». Their file
	// is filled by Run.
	Direct []journal.Closed
	// Reverted names the direct changes that the operation undoes.
	Reverted []string
}

// Errors of the direct changes.
var (
	ErrDirectAgent = errors.New("seul l'auteur modifie directement le texte (D-57)")
	ErrDirectEmpty = errors.New("aucune modification à appliquer")
	ErrDirectMarks = errors.New("la modification directe toucherait une annotation existante")
)

// Impose turns the proposals that proposed has and src has not (those just
// computed from a rewriting of the author) into text: each is accepted, so
// that no mark remains of them. It returns the text, and a copy of each change
// for the journal (decision Direct, line in the returned text). The other
// annotations of src must be left exactly as they are.
func Impose(src, proposed []byte) ([]byte, []journal.Closed, error) {
	created, _ := Compare("", src, proposed, "")
	isNew := map[string]bool{}
	for _, id := range created {
		isNew[id] = true
	}
	// The proposals created, from the last to the first: accepting one does
	// not move those before it.
	type target struct {
		start int
		ann   review.Annotation
	}
	var targets []target
	for _, a := range review.Read(proposed).Annotations {
		if !isNew[a.ID] {
			continue
		}
		if !isProposal(a.Kind) || len(a.Replies()) > 0 {
			// A message would ask the agent: not a direct change.
			return nil, nil, ErrDirectMarks
		}
		targets = append(targets, target{a.Span.Start, a})
	}
	if len(targets) == 0 {
		return nil, nil, ErrDirectEmpty
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].start > targets[j].start })
	type step struct{ start, delta int }
	var steps []step
	out := proposed
	done := map[string]bool{}
	for _, t := range targets {
		if done[t.ann.ID] {
			continue
		}
		a, ok := review.Read(out).Find(t.ann.ID)
		if !ok {
			// Decided with its partner (the two parts of a footnote).
			done[t.ann.ID] = true
			continue
		}
		e, err := DecideRange(out, a.Span.Start, a.Span.Start, Accept)
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, step{a.Span.Start, len(e.Insert) - (e.End - e.Start)})
		out = e.Apply(out)
		done[t.ann.ID] = true
	}
	// Nothing of the proposals is left, and the annotations of src are all
	// there, unchanged.
	after := review.Read(out)
	for id := range isNew {
		if _, ok := after.Find(id); ok {
			return nil, nil, ErrDirectMarks
		}
	}
	if !sameMarks(src, out) {
		return nil, nil, ErrDirectMarks
	}
	_, closed := Compare("", proposed, out, Direct)
	var copies []journal.Closed
	for _, c := range closed {
		if !isNew[c.ID] {
			return nil, nil, ErrDirectMarks
		}
		c.By = review.Author
		// Its place in the text that results.
		for _, t := range targets {
			if t.ann.ID != c.ID {
				continue
			}
			pos := t.start
			for _, s := range steps {
				if s.start < t.start {
					pos += s.delta
				}
			}
			c.Line, _ = render.LineCol(out, pos)
		}
		copies = append(copies, c)
	}
	sort.Slice(copies, func(i, j int) bool { return copies[i].Line < copies[j].Line })
	return out, copies, nil
}

// sameMarks reports whether a and b hold the same marks, with the same bytes,
// in the same order.
func sameMarks(a, b []byte) bool {
	ma, _ := critic.Parse(a)
	mb, _ := critic.Parse(b)
	if len(ma) != len(mb) {
		return false
	}
	for i := range ma {
		if string(a[ma[i].Span.Start:ma[i].Span.End]) != string(b[mb[i].Span.Start:mb[i].Span.End]) {
			return false
		}
	}
	return true
}

// Imposed returns the direct changes of the author's last part (the one in
// progress if the author has the hand, else the one the last handover
// ended), less those undone since: what the agent must take as written
// (D-57), and not discuss.
func (h *History) Imposed() []journal.Closed {
	out := []journal.Closed{}
	log := h.Log()
	i := len(log) - 1
	if h.Hand() != review.Author {
		i--
	}
	if i < 0 {
		return out
	}
	reverted := map[string]bool{}
	for _, op := range h.Ops {
		for _, id := range op.Reverted {
			reverted[id] = true
		}
	}
	for _, op := range log[i].Ops {
		if op.Actor != review.Author {
			continue
		}
		for _, c := range op.Closed {
			if c.Decision == Direct && !reverted[c.ID] {
				out = append(out, c)
			}
		}
	}
	return out
}
