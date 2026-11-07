// Package projection computes, in memory, the views of a text that holds
// CriticMarkup marks, and keeps the provenance of every projected byte:
//
//   - Current: the text as it stands if every proposal is rejected;
//   - Proposed: the text as it would be if every proposal were accepted;
//   - Review: both, side by side (additions, deletions, old and new texts).
//
// Comments never appear in a view: they are shown in the margin. Pandoc does
// not know CriticMarkup, so it is given a view,
// never the raw source. A view is only computed, never written (SPEC F3).
package projection

import (
	"sort"

	"github.com/ptitan/glose/internal/critic"
)

// View selects what a projection keeps.
type View int

const (
	Current View = iota
	Proposed
	Review
)

func (v View) String() string { return [...]string{"actuel", "proposé", "relecture"}[v] }

// Role tells where a projected segment comes from.
type Role int

const (
	Plain       Role = iota // text outside any mark
	Highlighted             // content of {==…==}
	Added                   // content of {++…++}
	Deleted                 // content of {--…--}
	Old                     // old text of {~~…~>…~~}
	New                     // new text of {~~…~>…~~}
)

func (r Role) String() string {
	return [...]string{"texte", "surligné", "ajouté", "supprimé", "ancien", "nouveau"}[r]
}

// Segment is a run of bytes copied verbatim from the source into the view.
type Segment struct {
	Out  critic.Span // range in the view
	Src  critic.Span // range in the source, of the same length
	Role Role
	Mark int // index of the mark it belongs to, or -1 for Plain
}

// Projection is a view and its provenance.
type Projection struct {
	View     View
	Text     []byte
	Segments []Segment     // contiguous, in order, covering Text
	Marks    []critic.Mark // the marks of the source
	// Places gives, for every mark, the range of the view where its visible
	// content lies; it is empty (Start == End) for a mark with nothing visible
	// in this view, such as a comment: that is where the margin note attaches.
	Places []critic.Span
	Issues []critic.Issue // malformed marks, kept as plain text
}

// Project computes view v of src.
func Project(src []byte, v View) Projection {
	marks, issues := critic.Parse(src)
	p := Projection{View: v, Marks: marks, Issues: issues, Places: make([]critic.Span, len(marks))}
	copySeg := func(s critic.Span, r Role, mark int) {
		if s.Len() == 0 {
			return
		}
		out := critic.Span{Start: len(p.Text), End: len(p.Text) + s.Len()}
		p.Text = append(p.Text, src[s.Start:s.End]...)
		p.Segments = append(p.Segments, Segment{Out: out, Src: s, Role: r, Mark: mark})
	}
	pos := 0
	for i, m := range marks {
		copySeg(critic.Span{Start: pos, End: m.Span.Start}, Plain, -1)
		start := len(p.Text)
		switch m.Kind {
		case critic.Highlight:
			copySeg(m.Text, Highlighted, i)
		case critic.Addition:
			if v != Current {
				copySeg(m.Text, Added, i)
			}
		case critic.Deletion:
			if v != Proposed {
				copySeg(m.Text, Deleted, i)
			}
		case critic.Substitution:
			if v != Proposed {
				copySeg(m.Text, Old, i)
			}
			if v != Current {
				copySeg(m.New, New, i)
			}
		case critic.Comment:
			// never in the text
		}
		p.Places[i] = critic.Span{Start: start, End: len(p.Text)}
		pos = m.Span.End
	}
	copySeg(critic.Span{Start: pos, End: len(src)}, Plain, -1)
	return p
}

// segmentAt returns the index of the segment that contains view offset off,
// or the number of segments if off is at the end of the view.
func (p Projection) segmentAt(off int) int {
	return sort.Search(len(p.Segments), func(i int) bool { return p.Segments[i].Out.End > off })
}

// SourceStart maps a view offset used as the start of a range to a source
// offset: at a boundary between two segments, it is the start of the
// following segment. ok is false outside the view.
func (p Projection) SourceStart(off int) (src int, seg Segment, ok bool) {
	if off < 0 || off >= len(p.Text) {
		return 0, Segment{}, false
	}
	s := p.Segments[p.segmentAt(off)]
	return s.Src.Start + off - s.Out.Start, s, true
}

// SourceEnd maps a view offset used as the (exclusive) end of a range to a
// source offset: at a boundary between two segments, it is the end of the
// preceding segment. ok is false outside the view.
func (p Projection) SourceEnd(off int) (src int, seg Segment, ok bool) {
	if off <= 0 || off > len(p.Text) {
		return 0, Segment{}, false
	}
	s := p.Segments[p.segmentAt(off-1)]
	return s.Src.Start + off - s.Out.Start, s, true
}
