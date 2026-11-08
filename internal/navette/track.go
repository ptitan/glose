package navette

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/edit"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/notes"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
)

// Tracking of the agent's changes (D-25, SPEC F18). When the agent hands its
// turn over, each file is compared with the copy kept when the author handed
// the turn over to it. The comparison works on units: words, runs of spaces,
// punctuation, and whole marks (a mark is never split). Each difference is:
//
//   - a change of free text: it becomes proposals, word by word, the way the
//     author's rewritings do (package edit);
//   - the addition of well-formed marks only (a comment, a reply in a thread,
//     a proposal written by hand, D-20), whose view « Current » is the text
//     they replace: kept as written;
//   - anything that touches an existing mark (changed, moved, deleted), or
//     mixes marks and free text, or adds a technical comment that Glose did not
//     write: an anomaly. Nothing is written, and the agent is told what to
//     put back (D-14: the agent never decides).
//
// The result is checked: its view « Current » is that of the copy (nothing
// changes until the author accepts), and its view « Proposed » is that of the
// agent's text (accepting everything gives what the agent wrote).

// Problem is an anomaly found in the agent's changes.
type Problem struct {
	File    string
	Offset  int // in the agent's text
	Line    int
	Message string
}

func (p Problem) String() string {
	return fmt.Sprintf("%s:%d : %s", p.File, p.Line, p.Message)
}

// TrackError gathers the anomalies of a handover.
type TrackError struct{ Problems []Problem }

func (e *TrackError) Error() string {
	var b strings.Builder
	b.WriteString("les modifications ne peuvent pas être converties en propositions ; rien n'a été écrit :")
	for _, p := range e.Problems {
		b.WriteString("\n  " + p.String())
	}
	return b.String()
}

// maxChanges bounds the number of units changed in one file (the comparison
// costs memory in the square of it).
const maxChanges = 6000

// ErrTooManyChanges is returned when a file changed too much to be compared
// word by word.
var ErrTooManyChanges = errors.New("trop de modifications dans ce fichier pour les comparer mot à mot : remettez votre tour plus souvent")

// unit is a token of a text: a mark, or a piece of free text.
type unit struct {
	b      []byte
	mark   bool
	offset int
}

func units(src []byte) []unit {
	marks, _ := critic.Parse(src)
	var out []unit
	text := func(s, e int) {
		off := s
		for _, t := range edit.Tokens(src[s:e]) {
			out = append(out, unit{b: t, offset: off})
			off += len(t)
		}
	}
	pos := 0
	for _, m := range marks {
		text(pos, m.Span.Start)
		out = append(out, unit{b: src[m.Span.Start:m.Span.End], mark: true, offset: m.Span.Start})
		pos = m.Span.End
	}
	text(pos, len(src))
	return out
}

// step of an edit script: equal units, or a change.
type step struct {
	equal  bool
	a0, a1 int // units of the old text
	b0, b1 int // units of the new text
}

// diff compares two sequences of units (Myers, O((N+M)·D) time, O(D²) memory).
func diff(a, b []unit) ([]step, error) {
	ids := map[string]int{}
	code := func(us []unit) []int {
		out := make([]int, len(us))
		for i, u := range us {
			k := string(u.b)
			if u.mark {
				k = "\x00" + k
			}
			id, ok := ids[k]
			if !ok {
				id = len(ids)
				ids[k] = id
			}
			out[i] = id
		}
		return out
	}
	x, y := code(a), code(b)
	// Common prefix and suffix first: most of a file is unchanged.
	p := 0
	for p < len(x) && p < len(y) && x[p] == y[p] {
		p++
	}
	s := 0
	for s < len(x)-p && s < len(y)-p && x[len(x)-1-s] == y[len(y)-1-s] {
		s++
	}
	mid, err := myers(x[p:len(x)-s], y[p:len(y)-s])
	if err != nil {
		return nil, err
	}
	var out []step
	add := func(st step) {
		if st.a0 == st.a1 && st.b0 == st.b1 {
			return
		}
		if n := len(out); n > 0 && out[n-1].equal == st.equal {
			out[n-1].a1, out[n-1].b1 = st.a1, st.b1
			return
		}
		out = append(out, st)
	}
	add(step{equal: true, a0: 0, a1: p, b0: 0, b1: p})
	for _, st := range mid {
		st.a0, st.a1, st.b0, st.b1 = st.a0+p, st.a1+p, st.b0+p, st.b1+p
		add(st)
	}
	add(step{equal: true, a0: len(x) - s, a1: len(x), b0: len(y) - s, b1: len(y)})
	return out, nil
}

// myers returns the steps that turn x into y.
func myers(x, y []int) ([]step, error) {
	n, m := len(x), len(y)
	if n == 0 && m == 0 {
		return nil, nil
	}
	max := n + m
	// trace[d] holds V for -d..d (index k+d).
	var trace [][]int
	v := []int{0, 0, 0} // V for k = -1..1 before the first step: V[1] = 0
	off := 1
	var dEnd int
	found := false
	for d := 0; d <= max; d++ {
		if d > maxChanges {
			return nil, ErrTooManyChanges
		}
		nv := make([]int, 2*d+1)
		get := func(k int) int { return v[k+off] }
		for k := -d; k <= d; k += 2 {
			var xx int
			if k == -d || (k != d && get(k-1) < get(k+1)) {
				xx = get(k + 1) // down: insertion
			} else {
				xx = get(k-1) + 1 // right: deletion
			}
			yy := xx - k
			for xx < n && yy < m && x[xx] == y[yy] {
				xx++
				yy++
			}
			nv[k+d] = xx
			if xx >= n && yy >= m {
				found = true
			}
		}
		trace = append(trace, nv)
		v, off = nv, d
		if found {
			dEnd = d
			break
		}
	}
	// Backtrack.
	var rev []step
	xx, yy := n, m
	for d := dEnd; d > 0; d-- {
		pv := trace[d-1]
		get := func(k int) int { return pv[k+d-1] }
		k := xx - yy
		var pk int
		if k == -d || (k != d && get(k-1) < get(k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := get(pk)
		py := px - pk
		// Diagonal (equal) from the end of the move to (xx, yy).
		var sx, sy int
		if pk == k+1 { // insertion: (px, py) -> (px, py+1)
			sx, sy = px, py+1
		} else { // deletion: (px, py) -> (px+1, py)
			sx, sy = px+1, py
		}
		if xx > sx {
			rev = append(rev, step{equal: true, a0: sx, a1: xx, b0: sy, b1: yy})
		}
		rev = append(rev, step{a0: px, a1: sx, b0: py, b1: sy})
		xx, yy = px, py
	}
	if xx > 0 {
		rev = append(rev, step{equal: true, a0: 0, a1: xx, b0: 0, b1: yy})
	}
	out := make([]step, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out, nil
}

// Track turns the agent's text cur, written over the copy base, into a text
// where its free changes are proposals. known holds the identifiers that
// Glose itself wrote since the copy (commands of the agent): only those may
// appear in new technical comments. It returns the new text and the number of
// proposals made, or the anomalies.
//
// The comparison has two levels: paragraphs first (a paragraph and the blank
// lines that follow it), the rewritten paragraphs being paired by similarity;
// then words, inside each pair. A paragraph deleted or added whole becomes
// one proposal, and no proposal crosses the limit of a paragraph.
func Track(path string, base, cur []byte, known map[string]bool, gen *review.Gen) ([]byte, int, []Problem) {
	if gen == nil {
		gen = review.NewGen(nil)
	}
	t := &tracker{path: path, cur: cur, known: known, gen: gen, fromBase: map[int]int{}}
	_, baseIssues := critic.Parse(base)
	if _, curIssues := critic.Parse(cur); len(curIssues) > len(baseIssues) {
		for _, is := range curIssues {
			t.report(is.Offset, "marque mal formée : "+is.Code.String())
		}
		return nil, 0, t.problems
	}
	a, b := chunks(base), chunks(cur)
	a = alignChunks(a, b)
	steps, err := diff(a, b)
	if err != nil {
		t.report(0, err.Error())
		return nil, 0, t.problems
	}
	t.base = base
	for _, st := range steps {
		if st.equal {
			for k := st.a0; k < st.a1; k++ {
				t.copyBase(a[k].offset, a[k].b)
			}
			continue
		}
		del, ins := a[st.a0:st.a1], b[st.b0:st.b1]
		pairs := pair(del, ins)
		i, j := 0, 0
		for _, p := range append(pairs, [2]int{len(del), len(ins)}) {
			for ; i < p[0]; i++ {
				t.words(del[i].b, del[i].offset, nil, insAt(ins, j, del, cur))
			}
			for ; j < p[1]; j++ {
				t.words(nil, 0, ins[j].b, ins[j].offset)
			}
			if i < len(del) && j < len(ins) {
				t.words(del[i].b, del[i].offset, ins[j].b, ins[j].offset)
				i, j = i+1, j+1
			}
		}
	}
	if len(t.problems) > 0 {
		return nil, 0, t.problems
	}
	out := t.out
	t.checkThreads(out)
	if len(t.problems) > 0 {
		return nil, 0, t.problems
	}
	// The result: Current = the copy, Proposed = the agent's text (in which
	// the proposals of the author that he rewrote are put back as they were:
	// his rewriting went to their thread as a correction), no new malformed
	// mark.
	want := cur
	for k := len(t.kept) - 1; k >= 0; k-- {
		km := t.kept[k]
		want = patch.Edit{Start: km.at, End: km.at + km.n, Insert: km.old}.Apply(want)
	}
	if !bytes.Equal(projection.Project(out, projection.Current).Text, projection.Project(base, projection.Current).Text) ||
		!bytes.Equal(projection.Project(out, projection.Proposed).Text, projection.Project(want, projection.Proposed).Text) {
		t.report(0, "la vérification des propositions a échoué (vues Actuel et Proposé) : aucune modification n'est écrite")
		return nil, 0, t.problems
	}
	if _, is := critic.Parse(out); len(is) > len(baseIssues) {
		t.report(0, "la conversion produirait une marque mal formée : aucune modification n'est écrite")
		return nil, 0, t.problems
	}
	return out, t.converted, nil
}

// insAt returns the offset in cur where text deleted before the j-th inserted
// chunk would be: for the reports.
func insAt(ins []unit, j int, del []unit, cur []byte) int {
	if j < len(ins) {
		return ins[j].offset
	}
	if len(ins) > 0 {
		return ins[len(ins)-1].offset + len(ins[len(ins)-1].b)
	}
	return 0
}

// tracker builds the result of Track.
type tracker struct {
	path      string
	base, cur []byte
	known     map[string]bool
	out       []byte
	converted int
	fromBase  map[int]int // start in out of a mark of the copy -> its start in the copy
	gen       *review.Gen
	problems  []Problem
	// kept lists the proposals of the author that the agent rewrote: they
	// stay as they were (the rewriting became a correction in their thread).
	kept []keptMark
}

// keptMark is a proposal of the author kept as it was: its place in the
// agent's text, the length of his rewriting there, and the original mark.
type keptMark struct {
	at, n int
	old   []byte
}

// copyBase appends to the result the bytes b of the copy, which start at
// offset at in it, and remembers where its marks go.
func (t *tracker) copyBase(at int, b []byte) {
	marks, _ := critic.Parse(b)
	if t.fromBase == nil {
		t.fromBase = map[int]int{}
	}
	for _, m := range marks {
		t.fromBase[len(t.out)+m.Span.Start] = at + m.Span.Start
	}
	t.out = append(t.out, b...)
}

// checkThreads verifies that every mark of the copy keeps its place in the
// threads: a main mark stays the head of its annotation, a comment stays in
// the thread of the same mark. A mark written next to an existing comment
// (by Glose or by the agent) would otherwise take it as its reply, and a
// decision on it would close the author's comment too.
func (t *tracker) checkThreads(out []byte) {
	bm, _ := critic.Parse(t.base)
	owner := map[int]int{} // start in the copy -> start of its head (-1: a head)
	for _, g := range critic.Group(bm) {
		owner[bm[g.Main].Span.Start] = -1
		for _, c := range g.Comments {
			owner[bm[c].Span.Start] = bm[g.Main].Span.Start
		}
	}
	om, _ := critic.Parse(out)
	for _, g := range critic.Group(om) {
		head, headOld := t.fromBase[om[g.Main].Span.Start]
		if headOld && owner[head] != -1 {
			t.report(0, "une réponse existante deviendrait une annotation à part : "+excerptBytes(out[om[g.Main].Span.Start:g.Span.End]))
			continue
		}
		for _, c := range g.Comments {
			old, ok := t.fromBase[om[c].Span.Start]
			if ok && (!headOld || owner[old] != head) {
				t.report(0, "une modification est collée à un commentaire existant, qui deviendrait une réponse à la proposition ; "+
					"laissez au moins un mot inchangé avant le commentaire, ou écrivez l'ajout après lui : "+
					excerptBytes(out[om[g.Main].Span.Start:g.Span.End]))
				break
			}
		}
	}
}

func (t *tracker) report(off int, msg string) {
	l, _ := render.LineCol(t.cur, off)
	t.problems = append(t.problems, Problem{File: t.path, Offset: off, Line: l, Message: msg})
}

// words compares an old and a new paragraph word by word, and appends the
// result. oldAt is the offset of the old paragraph in the copy, at that of the
// new paragraph in the agent's text.
func (t *tracker) words(old []byte, oldAt int, neu []byte, at int) {
	if old != nil && neu != nil && t.onlyAnnotated(old, oldAt, neu, at) {
		return
	}
	a, b := units(old), units(neu)
	for i := range a {
		a[i].offset += oldAt
	}
	for i := range b {
		b[i].offset += at
	}
	steps, err := diff(a, b)
	if err != nil {
		t.report(at, err.Error())
		return
	}
	steps = mergeAcrossSpaces(steps, a, b)
	join := func(us []unit) []byte {
		var out []byte
		for _, u := range us {
			out = append(out, u.b...)
		}
		return out
	}
	// An addition just before a comment would take it as its reply: it waits
	// until the thread is written (comments are invisible: the views are the
	// same).
	var pending [][]byte
	flush := func() {
		for _, p := range pending {
			t.out = append(t.out, p...)
			t.converted++
		}
		pending = nil
	}
	emit := func(b []byte, mark bool) {
		if !(mark && isComment(b)) {
			flush()
		}
		t.out = append(t.out, b...)
	}
	var rewritten bool // a highlighted passage rewritten by the agent: its thread follows
	var fix []byte     // a correction of a proposal of the author: it ends the thread that follows
	for _, st := range steps {
		if st.equal {
			olds := a[st.a0:st.a1]
			i := 0
			if rewritten {
				i = t.takeThread(olds)
				rewritten = false
			}
			if fix != nil {
				for i < len(olds) && olds[i].mark && isComment(olds[i].b) {
					t.copyBase(olds[i].offset, olds[i].b)
					i++
				}
				t.out = append(t.out, fix...)
				t.converted++
				fix = nil
			}
			for ; i < len(olds); i++ {
				if !(olds[i].mark && isComment(olds[i].b)) {
					flush()
				}
				t.copyBase(olds[i].offset, olds[i].b)
			}
			continue
		}
		ins := b[st.b0:st.b1]
		del := a[st.a0:st.a1]
		pos := at + len(neu)
		if st.b0 < len(b) {
			pos = b[st.b0].offset
		}
		delMarks, insMarks := false, false
		for _, u := range del {
			delMarks = delMarks || u.mark
		}
		for _, u := range ins {
			insMarks = insMarks || u.mark
		}
		oldText, newText := join(del), join(ins)
		switch {
		case len(del) == 1 && len(ins) == 1 && isHighlight(del[0]) && isHighlight(ins[0]):
			// The agent rewrote the passage that the author highlighted: a
			// proposal that takes over the thread (D-26).
			mark, msg := rewriteHighlight(del[0], ins[0])
			if msg != "" {
				t.report(pos, msg)
				continue
			}
			flush()
			t.fromBase[len(t.out)] = del[0].offset // the proposal stands for the highlight
			t.out = append(t.out, mark...)
			t.converted++
			rewritten = true
			continue
		case len(del) == 1 && len(ins) == 1 && isProposalUnit(del[0]) && isProposalUnit(ins[0]) &&
			bytes.Equal(projection.Project(del[0].b, projection.Current).Text, projection.Project(ins[0].b, projection.Current).Text):
			// The agent rewrote what a proposal proposes (its original text
			// unchanged). His own proposal: corrected in place. A proposal of
			// the author: unchanged, and its thread receives a correction that
			// the author may apply (Meta.Fix).
			proposed := projection.Project(ins[0].b, projection.Proposed).Text
			id, by := threadIdentity(a[st.a1:])
			switch {
			case id == "":
				t.report(pos, "cette proposition n'a pas encore d'identité : répondez dans son fil plutôt que de la réécrire")
			case by == review.Agent:
				flush()
				t.fromBase[len(t.out)] = del[0].offset
				t.out = append(t.out, ins[0].b...)
				t.converted++
			case critic.ContainsDelimiter(proposed):
				t.report(pos, "la correction contient une suite de signes réservée aux annotations ({++, <<}…)")
			case string(proposed) == review.FixDelete:
				t.report(pos, review.ErrFixSymbol.Error())
			default:
				t.copyBase(del[0].offset, del[0].b)
				t.kept = append(t.kept, keptMark{at: ins[0].offset, n: len(ins[0].b), old: del[0].b})
				body := string(proposed)
				if body == "" {
					body = review.FixDelete
				}
				nid, err := t.gen.New()
				if err != nil {
					t.report(pos, err.Error())
					continue
				}
				fix = append(wrapMark(critic.Comment, []byte(body)), review.Meta{ID: nid, Re: id, By: review.Agent, Fix: true}.Bytes()...)
			}
			continue
		case delMarks:
			t.report(pos, "une annotation existante a été modifiée, déplacée ou supprimée ; remettez-la telle qu'elle était : "+excerpt(del))
			continue
		case insMarks && len(del) == 0 && emptyMarks(ins):
			// Marks and text added at the same place: each on its own.
			if !t.knownMetas(ins) {
				continue
			}
			for k := 0; k < len(ins); {
				if ins[k].mark {
					emit(ins[k].b, true)
					k++
					continue
				}
				e := k
				for e < len(ins) && !ins[e].mark {
					e++
				}
				text := join(ins[k:e])
				switch {
				case critic.ContainsDelimiter(text):
					t.report(ins[k].offset, "le texte ajouté contient une suite de signes réservée aux annotations ({++, <<}…)")
				case e < len(ins) && isComment(ins[e].b):
					pending = append(pending, wrapMark(critic.Addition, text))
				default:
					flush()
					t.out = append(t.out, wrapMark(critic.Addition, text)...)
					t.converted++
				}
				k = e
			}
			continue
		case insMarks:
			// Marks written by the agent: allowed if they only annotate.
			if !bytes.Equal(projection.Project(newText, projection.Current).Text, oldText) {
				t.report(pos, "des marques et des modifications libres sont mêlées au même endroit ; écrivez l'un ou l'autre")
				continue
			}
			if t.knownMetas(ins) {
				for _, u := range ins {
					emit(u.b, u.mark)
				}
			}
			continue
		}
		if critic.ContainsDelimiter(newText) {
			t.report(pos, "le texte ajouté contient une suite de signes réservée aux annotations ({++, <<}…)")
			continue
		}
		var mark []byte
		switch {
		case len(oldText) == 0:
			mark = wrapMark(critic.Addition, newText)
			if st.a1 < len(a) && a[st.a1].mark && isComment(a[st.a1].b) {
				pending = append(pending, mark)
				continue
			}
		case len(newText) == 0:
			mark = wrapMark(critic.Deletion, oldText)
		case bytes.Contains(oldText, []byte(critic.Arrow)):
			mark = append(wrapMark(critic.Deletion, oldText), wrapMark(critic.Addition, newText)...)
		default:
			pre, o, n, suf := edit.TrimSigns(oldText, newText)
			mark = append([]byte(nil), pre...)
			mark = append(mark, critic.Opener(critic.Substitution)...)
			mark = append(mark, o...)
			mark = append(mark, critic.Arrow...)
			mark = append(mark, n...)
			mark = append(mark, critic.Closer(critic.Substitution)...)
			mark = append(mark, suf...)
		}
		flush()
		t.out = append(t.out, mark...)
		t.converted++
	}
	flush()
}

// onlyAnnotated handles a paragraph to which the agent only added marks (by
// glose propose, comment, reply): its view « Current » is unchanged, and every
// mark of the copy is still there, as it was and in order. The paragraph is
// then taken as it is, with no word alignment, which could pair the spaces
// otherwise and take a proposal for a change of text. It returns false, and
// writes nothing, if the paragraph is not of that kind.
func (t *tracker) onlyAnnotated(old []byte, oldAt int, neu []byte, at int) bool {
	if !bytes.Equal(projection.Project(old, projection.Current).Text, projection.Project(neu, projection.Current).Text) {
		return false
	}
	om, _ := critic.Parse(old)
	nm, _ := critic.Parse(neu)
	// The marks of the copy, matched in order among those of the new text.
	match := map[int]int{} // index in nm -> index in om
	k := 0
	for i, m := range nm {
		if k < len(om) && bytes.Equal(neu[m.Span.Start:m.Span.End], old[om[k].Span.Start:om[k].Span.End]) {
			match[i] = k
			k++
		}
	}
	if k < len(om) {
		return false
	}
	// Every mark of the copy stays at its place in the text: the text
	// between two marks of the copy (in view « Current ») is unchanged. Else
	// an annotation would move to another occurrence of its passage.
	matched := make([]int, 0, len(om)) // indexes in nm, in order
	for i := range nm {
		if _, ok := match[i]; ok {
			matched = append(matched, i)
		}
	}
	for j := 0; j <= len(om); j++ {
		ob, oe, nb, ne := 0, len(old), 0, len(neu)
		if j > 0 {
			ob, nb = om[j-1].Span.End, nm[matched[j-1]].Span.End
		}
		if j < len(om) {
			oe, ne = om[j].Span.Start, nm[matched[j]].Span.Start
		}
		if !bytes.Equal(projection.Project(old[ob:oe], projection.Current).Text, projection.Project(neu[nb:ne], projection.Current).Text) {
			return false
		}
	}
	// Every thread of the copy keeps exactly its members: an added mark
	// never becomes the head of an existing comment (it would take it as
	// its reply); added comments may only extend a thread.
	oldGroup := map[int]int{} // index in om -> index of its group's main
	for _, g := range critic.Group(om) {
		oldGroup[g.Main] = g.Main
		for _, c := range g.Comments {
			oldGroup[c] = g.Main
		}
	}
	for _, g := range critic.Group(nm) {
		head, headOld := match[g.Main]
		for _, c := range g.Comments {
			j, ok := match[c]
			if !ok {
				continue
			}
			if !headOld || oldGroup[j] != head {
				return false
			}
		}
		if headOld {
			n := 0
			for _, c := range g.Comments {
				if _, ok := match[c]; ok {
					n++
				}
			}
			want := 0
			for j, h := range oldGroup {
				if h == head && j != head {
					want++
				}
			}
			if n != want {
				return false
			}
		}
	}
	// The added marks only annotate (the view « Current » says so), and
	// their technical comments come from Glose.
	var added []unit
	for i, m := range nm {
		if _, ok := match[i]; !ok {
			added = append(added, unit{b: neu[m.Span.Start:m.Span.End], offset: at + m.Span.Start, mark: true})
		}
	}
	if !t.knownMetas(added) {
		return true // reported
	}
	pos := 0
	for i, m := range nm {
		t.out = append(t.out, neu[pos:m.Span.Start]...)
		if j, ok := match[i]; ok {
			t.copyBase(oldAt+om[j].Span.Start, old[om[j].Span.Start:om[j].Span.End])
		} else {
			t.out = append(t.out, neu[m.Span.Start:m.Span.End]...)
		}
		pos = m.Span.End
	}
	t.out = append(t.out, neu[pos:]...)
	return true
}

// isProposalUnit reports whether u is one proposal mark.
func isProposalUnit(u unit) bool {
	if !u.mark {
		return false
	}
	for _, k := range []critic.Kind{critic.Addition, critic.Deletion, critic.Substitution} {
		if bytes.HasPrefix(u.b, []byte(critic.Opener(k))) {
			return true
		}
	}
	return false
}

// threadIdentity reads, in the units that follow a mark (its thread), the
// identity of its annotation and who made it.
func threadIdentity(us []unit) (id, by string) {
	for _, u := range us {
		if !u.mark || !isComment(u.b) {
			return "", ""
		}
		if isMetaUnit(u) {
			if m, err := review.ParseMeta(u.b[3 : len(u.b)-3]); err == nil && m.Re == "" {
				return m.ID, m.By
			}
		}
	}
	return "", ""
}

func isComment(b []byte) bool { return bytes.HasPrefix(b, []byte(critic.Opener(critic.Comment))) }

func isHighlight(u unit) bool {
	return u.mark && bytes.HasPrefix(u.b, []byte(critic.Opener(critic.Highlight)))
}

func isMetaUnit(u unit) bool {
	return u.mark && isComment(u.b) && review.IsMeta(u.b[3:len(u.b)-3])
}

// emptyMarks reports whether every mark of us is invisible in the view
// « Current » (a comment, an addition): marks that replace no text.
func emptyMarks(us []unit) bool {
	for _, u := range us {
		if u.mark && len(projection.Project(u.b, projection.Current).Text) > 0 {
			return false
		}
	}
	return true
}

// knownMetas checks the technical comments among us: only those written by
// Glose since the copy are allowed. It reports the others.
func (t *tracker) knownMetas(us []unit) bool {
	ok := true
	for _, u := range us {
		if !isMetaUnit(u) {
			continue
		}
		m, err := review.ParseMeta(u.b[3 : len(u.b)-3])
		switch {
		case err != nil:
			t.report(u.offset, "commentaire technique « glose: » illisible : "+err.Error())
			ok = false
		case !t.known[m.ID]:
			t.report(u.offset, "commentaire technique « glose: » écrit à la main (identifiant "+m.ID+") : retirez-le, Glose écrit les identifiants")
			ok = false
		}
	}
	return ok
}

// rewriteHighlight turns the highlight old of the copy, rewritten new by the
// agent, into a proposal. It returns a message if it cannot.
func rewriteHighlight(old, neu unit) ([]byte, string) {
	p, n := old.b[3:len(old.b)-3], neu.b[3:len(neu.b)-3]
	if bytes.Contains(p, []byte(critic.Arrow)) || critic.ContainsDelimiter(p) || critic.ContainsDelimiter(n) {
		return nil, "le passage surligné ne peut pas devenir une proposition (il contient « ~> » ou un délimiteur) ; répondez dans le fil"
	}
	if len(n) == 0 {
		return wrapMark(critic.Deletion, p), ""
	}
	m := []byte(critic.Opener(critic.Substitution))
	m = append(m, p...)
	m = append(m, critic.Arrow...)
	m = append(m, n...)
	return append(m, critic.Closer(critic.Substitution)...), ""
}

// takeThread writes the thread of a rewritten highlight after its proposal,
// from the units olds of the copy that follow the highlight: the identity of
// the annotation goes to the proposal, made by the agent, and the author's
// remark becomes the first message of its thread, with an identity of its
// own and its author. It returns the number of units taken.
func (t *tracker) takeThread(olds []unit) int {
	k := 0
	var remark *unit
	if k < len(olds) && olds[k].mark && isComment(olds[k].b) && !isMetaUnit(olds[k]) {
		remark = &olds[k]
		k++
	}
	if k < len(olds) && isMetaUnit(olds[k]) {
		if m, err := review.ParseMeta(olds[k].b[3 : len(olds[k].b)-3]); err == nil && m.Re == "" {
			t.out = append(t.out, review.Meta{ID: m.ID, By: review.Agent}.Bytes()...)
			if remark != nil {
				t.copyBase(remark.offset, remark.b)
				if id, err := t.gen.New(); err == nil {
					t.out = append(t.out, review.Meta{ID: id, Re: m.ID, By: m.By, To: m.To}.Bytes()...)
				}
			}
			return k + 1
		}
	}
	// No identity yet: the thread stays as it is (identities come at the
	// handover).
	if remark != nil {
		t.copyBase(remark.offset, remark.b)
	}
	return k
}

// mergeAcrossSpaces joins two changes separated only by spaces, or by one
// short word, into one (« {~~Le~>La~~} {~~site~>spécification~~} » becomes
// « {~~Le site~>La spécification~~} »): accepting half of a rewording would
// leave a text that nobody wrote. Changes that involve marks are left apart.
func mergeAcrossSpaces(steps []step, a, b []unit) []step {
	hasMark := func(st step) bool {
		for _, u := range a[st.a0:st.a1] {
			if u.mark {
				return true
			}
		}
		for _, u := range b[st.b0:st.b1] {
			if u.mark {
				return true
			}
		}
		return false
	}
	// A gap that joins two changes: spaces, and at most one short word or
	// sign (« a », « de », « , »), on the same line.
	gap := func(st step) bool {
		words := 0
		for _, u := range a[st.a0:st.a1] {
			if u.mark || bytes.ContainsAny(u.b, "\r\n") {
				return false
			}
			if len(bytes.TrimSpace(u.b)) > 0 {
				words++
				if words > 1 || utf8.RuneCount(u.b) > 3 {
					return false
				}
			}
		}
		return st.a1 > st.a0
	}
	var out []step
	for _, st := range steps {
		n := len(out)
		if !st.equal && n >= 2 && out[n-1].equal && gap(out[n-1]) && !out[n-2].equal && !hasMark(out[n-2]) && !hasMark(st) {
			prev := out[n-2]
			out = append(out[:n-2], step{a0: prev.a0, a1: st.a1, b0: prev.b0, b1: st.b1})
			continue
		}
		out = append(out, st)
	}
	return out
}

// alignChunks merges the paragraphs of the copy that a mark of the agent's
// text covers whole (glose propose --whole on several paragraphs): chunks
// never splits a mark, so the agent's text has one paragraph where the copy
// has several. They are merged when their text is exactly the view
// « Current » of that paragraph; the comparison then sees one paragraph to
// which the agent only added marks.
func alignChunks(a, b []unit) []unit {
	views := map[string]bool{}
	for _, u := range b {
		v := projection.Project(u.b, projection.Current).Text
		if len(chunks(v)) > 1 {
			views[string(v)] = true
		}
	}
	if len(views) == 0 {
		return a
	}
	var out []unit
	for k := 0; k < len(a); {
		merged := false
		var acc []byte
		for m := k; m < len(a) && m-k < 64; m++ {
			acc = append(acc, projection.Project(a[m].b, projection.Current).Text...)
			if m > k && views[string(acc)] {
				out = append(out, unit{b: joinUnits(a[k : m+1]), offset: a[k].offset})
				k = m + 1
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, a[k])
			k++
		}
	}
	return out
}

// joinUnits returns the bytes of consecutive chunks (they are adjacent in
// their text).
func joinUnits(us []unit) []byte {
	var b []byte
	for _, u := range us {
		b = append(b, u.b...)
	}
	return b
}

// chunks splits src into paragraphs: each chunk is a run of non-blank lines
// followed by the blank lines after it. A mark is never split.
func chunks(src []byte) []unit {
	marks, _ := critic.Parse(src)
	inMark := func(i int) bool {
		for _, m := range marks {
			if i > m.Span.Start && i < m.Span.End {
				return true
			}
			if m.Span.Start >= i {
				break
			}
		}
		return false
	}
	var out []unit
	start := 0
	blankSeen := false
	for i := 0; i < len(src); {
		j := bytes.IndexByte(src[i:], '\n')
		end := len(src)
		if j >= 0 {
			end = i + j + 1
		}
		blank := len(bytes.TrimSpace(src[i:end])) == 0
		if !blank && blankSeen && !inMark(i) {
			out = append(out, unit{b: src[start:i], offset: start})
			start = i
		}
		if blank {
			blankSeen = true
		} else {
			blankSeen = false
		}
		i = end
	}
	if start < len(src) {
		out = append(out, unit{b: src[start:], offset: start})
	}
	return out
}

// similarity of two paragraphs, from 0 to 1: the share of their tokens in
// common (Dice coefficient on the multisets of words and punctuation, spaces
// left aside).
func similarity(a, b []byte) float64 {
	words := func(x []byte) [][]byte {
		var out [][]byte
		for _, t := range edit.Tokens(x) {
			if len(bytes.TrimSpace(t)) > 0 {
				out = append(out, t)
			}
		}
		return out
	}
	count := map[string]int{}
	wa, wb := words(a), words(b)
	for _, w := range wa {
		count[string(w)]++
	}
	common := 0
	for _, w := range wb {
		if count[string(w)] > 0 {
			count[string(w)]--
			common++
		}
	}
	if len(wa)+len(wb) == 0 {
		return 1
	}
	return 2 * float64(common) / float64(len(wa)+len(wb))
}

// pair matches deleted and inserted paragraphs that are rewritings of each
// other: in order, each pair similar enough, the total similarity as high as
// possible. It returns the pairs (index in del, index in ins).
func pair(del, ins []unit) [][2]int {
	const threshold = 0.3
	n, m := len(del), len(ins)
	if n == 0 || m == 0 || n*m > 1_000_000 {
		return nil
	}
	sim := make([]float64, n*m)
	// Two paragraphs with the same view « Current » are the same paragraph,
	// only annotated (the marks weigh in the raw similarity).
	insCur := make([][]byte, m)
	for j := range ins {
		insCur[j] = projection.Project(ins[j].b, projection.Current).Text
	}
	for i := range del {
		di := projection.Project(del[i].b, projection.Current).Text
		for j := range ins {
			if bytes.Equal(di, insCur[j]) {
				sim[i*m+j] = 1
				continue
			}
			sim[i*m+j] = similarity(del[i].b, ins[j].b)
		}
	}
	best := make([]float64, (n+1)*(m+1))
	w := m + 1
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			v := max(best[(i+1)*w+j], best[i*w+j+1])
			if s := sim[i*m+j]; s >= threshold {
				v = max(v, s+best[(i+1)*w+j+1])
			}
			best[i*w+j] = v
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		s := sim[i*m+j]
		switch {
		case s >= threshold && best[i*w+j] == s+best[(i+1)*w+j+1]:
			out = append(out, [2]int{i, j})
			i, j = i+1, j+1
		case best[i*w+j] == best[(i+1)*w+j]:
			i++
		default:
			j++
		}
	}
	return out
}

func wrapMark(k critic.Kind, b []byte) []byte {
	out := append([]byte(critic.Opener(k)), b...)
	return append(out, critic.Closer(k)...)
}

func excerpt(us []unit) string {
	var b []byte
	for _, u := range us {
		if u.mark {
			b = append(b, u.b...)
		}
	}
	return excerptBytes(b)
}

func excerptBytes(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > 100 {
		s = string(r[:99]) + "…"
	}
	return s
}

// track converts the agent's changes of every file (D-25), against the
// copies kept at the last handover of the author.
func (n *Navette) track(f *Files, h *History, gen *review.Gen) ([]Change, int, error) {
	last := h.Last()
	if last == nil || last.Actor != review.Author {
		return nil, 0, nil
	}
	base := map[string]string{}
	for _, fh := range last.TurnEv.Files {
		base[fh.Path] = fh.Hash
	}
	known := map[string]bool{}
	for _, op := range h.Ops {
		if op.Seq > last.Seq {
			for _, id := range op.Created {
				known[id] = true
			}
		}
	}
	var changes []Change
	var problems []Problem
	total := 0
	present := map[string]bool{}
	for _, p := range n.P.Files {
		present[p] = true
	}
	// A file handed over that is no longer in the project: deleted by the
	// agent, or taken out of the profile. Its text cannot become proposals:
	// never pass over it silently (D-14).
	for _, fh := range last.TurnEv.Files {
		if present[fh.Path] {
			continue
		}
		msg := "fichier remis à l'agent puis retiré du projet (profil modifié) : remettez-le dans le profil, ou reprenez la main sans convertir après vérification"
		if _, err := os.Stat(n.P.Abs(fh.Path)); errors.Is(err, os.ErrNotExist) {
			msg = "fichier supprimé pendant le tour de l'agent : sa suppression ne peut pas devenir une proposition ; restaurez-le (copie : " +
				filepath.ToSlash(filepath.Join(project.StateDir, journal.SnapshotDir, fh.Hash)) + "), puis proposez de vider son texte"
		}
		problems = append(problems, Problem{File: fh.Path, Line: 1, Message: msg})
	}
	for i, d := range f.Docs {
		hash, ok := base[n.P.Files[i]]
		if ok && hash == d.Hash {
			continue
		}
		var copyb []byte
		if !ok {
			existed := !n.createdSince(n.P.Files[i], last)
			if existed {
				// It existed at the handover but was not handed over (added
				// to the profile since, or a handover of an older Glose):
				// whether it changed cannot be known. Never assume.
				problems = append(problems, Problem{File: n.P.Files[i], Line: 1, Message: "fichier entré dans le projet pendant le tour de l'agent, sans copie de la dernière remise de l'auteur (profil modifié, ou remise d'une version antérieure de Glose) : ses modifications éventuelles ne peuvent pas être converties ; retirez-le du profil jusqu'à la prochaine remise de l'auteur, ou vérifiez-le (git diff) puis reprenez la main sans convertir"})
				continue
			}
			// Created during the agent's turn: its whole text is proposed.
		} else {
			var err error
			if copyb, err = journal.LoadSnapshot(n.P.State(), hash); err != nil {
				return nil, 0, fmt.Errorf("%s : %w", n.P.Files[i], err)
			}
		}
		out, conv, pbs := Track(n.P.Files[i], copyb, d.Bytes, known, gen)
		if len(pbs) > 0 {
			problems = append(problems, pbs...)
			continue
		}
		out, conv = orphanNotes(out, conv)
		if !bytes.Equal(out, d.Bytes) {
			changes = append(changes, Change{File: i, Before: d.Bytes, After: out})
			total += conv
		}
	}
	if len(problems) > 0 {
		return nil, 0, &TrackError{Problems: problems}
	}
	return changes, total, nil
}

// orphanNotes proposes the deletion of the definitions whose last reference
// the agent deleted: a note is deleted whole (package notes).
func orphanNotes(out []byte, conv int) ([]byte, int) {
	before := notes.Refs(projection.Project(out, projection.Current).Text)
	after := notes.Refs(projection.Project(out, projection.Proposed).Text)
	var labels []string
	for l := range before {
		if !after[l] {
			labels = append(labels, l)
		}
	}
	sort.Strings(labels)
	for _, l := range labels {
		if sp, ok := notes.Definition(out, l); ok {
			if e, err := patch.ProposeDeletion(out, sp); err == nil {
				out = e.Apply(out)
				conv++
			}
		}
	}
	return out, conv
}

// createdSince reports whether the file rel was certainly created after the
// handover last (D-38). Every doubt answers no, and the handover is then
// refused: taking an older text for a creation would let its rejection
// erase it. The file must be absent from the inventory, whatever the case of
// its name (Windows, macOS); its path must not go through a symbolic link
// (the inventory does not follow them); and it must have been modified after
// the handover.
func (n *Navette) createdSince(rel string, last *journal.Event) bool {
	if !last.TurnEv.Complete {
		return false
	}
	for _, e := range last.TurnEv.Existing {
		if strings.EqualFold(e, rel) {
			return false
		}
	}
	abs := n.P.Abs(rel)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false
	}
	root, err := filepath.EvalSymlinks(n.P.Root)
	if err != nil {
		return false
	}
	if r, err := filepath.Rel(root, real); err != nil || !strings.EqualFold(filepath.ToSlash(r), rel) {
		return false
	}
	handed, err := time.Parse(time.RFC3339Nano, last.Time)
	if err != nil {
		return false
	}
	fi, err := os.Stat(abs)
	return err == nil && fi.ModTime().After(handed.Add(-2*time.Second))
}
