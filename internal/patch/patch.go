// Package patch computes the edits that Glose makes to a text: annotate,
// propose, decide. Every operation is a pure function that returns one Edit:
// the replacement of a single byte range. Applying it leaves every byte outside
// that range untouched (D-04).
//
// Operations never write files: package store does.
package patch

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
)

// Edit replaces src[Start:End] with Insert.
type Edit struct {
	Start, End int
	Insert     []byte
}

// Cover returns the edit of src that gives out (src with e applied) and that
// covers at least the range [start, end) of src.
func (e Edit) Cover(src, out []byte, start, end int) Edit {
	start, end = min(e.Start, start), max(e.End, end)
	return Edit{Start: start, End: end, Insert: append([]byte(nil), out[start:len(out)-(len(src)-end)]...)}
}

// Apply returns a new slice holding src with the edit applied; src is not
// modified.
func (e Edit) Apply(src []byte) []byte {
	out := make([]byte, 0, len(src)-(e.End-e.Start)+len(e.Insert))
	out = append(out, src[:e.Start]...)
	out = append(out, e.Insert...)
	return append(out, src[e.End:]...)
}

// Errors returned by the operations.
var (
	ErrRange     = errors.New("plage invalide")
	ErrOverlap   = errors.New("la plage chevauche une marque existante")
	ErrAdjacent  = errors.New("la marque se collerait à un commentaire existant et changerait son fil")
	ErrDelimiter = errors.New("le texte contient un délimiteur CriticMarkup, qui ne peut pas être représenté")
	ErrEmpty     = errors.New("texte vide")
	ErrArrow     = errors.New("l'ancien texte d'un remplacement ne peut pas contenir « ~> »")
	ErrKind      = errors.New("opération impossible sur ce type d'annotation")
	ErrNested    = errors.New("l'annotation contient une marque imbriquée : à corriger dans le fichier")
	ErrNeighbour = errors.New("une marque mal formée du texte voisin absorberait la modification ; corrigez-la d'abord")
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// doc is the text with its marks, parsed once per operation.
type doc struct {
	src    []byte
	marks  []critic.Mark
	issues []critic.Issue
}

func parse(src []byte) doc {
	marks, issues := critic.Parse(src)
	return doc{src, marks, issues}
}

// checkOffset accepts an offset that is inside the text, on a character
// boundary, not inside the byte order mark (nor before it), and not between
// the '\r' and '\n' of a CRLF line ending.
func (d doc) checkOffset(i int) error {
	switch {
	case i < 0 || i > len(d.src):
		return fmt.Errorf("%w : octet %d hors du texte", ErrRange, i)
	case i < len(d.src) && !utf8.RuneStart(d.src[i]):
		return fmt.Errorf("%w : octet %d au milieu d'un caractère", ErrRange, i)
	case bytes.HasPrefix(d.src, bom) && i < len(bom):
		return fmt.Errorf("%w : octet %d avant la fin de la marque d'ordre des octets", ErrRange, i)
	case i > 0 && i < len(d.src) && d.src[i-1] == '\r' && d.src[i] == '\n':
		return fmt.Errorf("%w : octet %d entre \\r et \\n", ErrRange, i)
	}
	return nil
}

// checkSpan validates a range where a new mark will be placed: valid offsets,
// no intersection with an existing mark, and no accidental joining of an
// existing comment thread. newIsComment tells whether the new mark starts with
// a comment (which would attach to a mark ending at s.Start).
func (d doc) checkSpan(s critic.Span, newIsComment bool) error {
	if s.Start > s.End {
		return fmt.Errorf("%w : début après la fin", ErrRange)
	}
	if err := d.checkOffset(s.Start); err != nil {
		return err
	}
	if err := d.checkOffset(s.End); err != nil {
		return err
	}
	// The opener of a malformed mark ("{++" never closed, "{~~" without
	// arrow) must not be cut: the new mark would swallow or split it.
	for _, is := range d.issues {
		if is.Code != critic.Unclosed && is.Code != critic.MissingArrow {
			continue
		}
		o := critic.Span{Start: is.Offset, End: is.Offset + len(critic.Opener(critic.Addition))}
		if s.Start < o.End && s.End > o.Start || s.Start > o.Start && s.Start < o.End {
			return ErrNeighbour
		}
	}
	for _, m := range d.marks {
		inside := s.Start > m.Span.Start && s.Start < m.Span.End ||
			s.End > m.Span.Start && s.End < m.Span.End ||
			s.Start <= m.Span.Start && s.End >= m.Span.End && s.End > s.Start
		if inside {
			return ErrOverlap
		}
		if m.Kind == critic.Comment && m.Span.Start == s.End {
			return ErrAdjacent // the existing comment would join the new mark
		}
		if newIsComment && m.Span.End == s.Start {
			return ErrAdjacent // the new comment would join the existing mark
		}
	}
	return nil
}

func checkText(t []byte, allowEmpty bool) error {
	if !allowEmpty && len(t) == 0 {
		return ErrEmpty
	}
	if !utf8.Valid(t) {
		return fmt.Errorf("%w : texte qui n'est pas en UTF-8", ErrRange)
	}
	if critic.ContainsDelimiter(t) {
		return ErrDelimiter
	}
	return nil
}

func wrap(k critic.Kind, parts ...[]byte) []byte {
	b := []byte(critic.Opener(k))
	for _, p := range parts {
		b = append(b, p...)
	}
	return append(b, critic.Closer(k)...)
}

// key identifies a mark by its kind and its span.
type key struct {
	kind       critic.Kind
	start, end int
}

func keyOf(m critic.Mark, shift int) key {
	return key{m.Kind, m.Span.Start + shift, m.Span.End + shift}
}

// verify re-parses the result of e and checks that nothing changed outside
// the edit, at three levels:
//
//   - the marks: those of src before and after the edited range (shifted),
//     plus the marks written by the edit itself;
//   - the problems (malformed marks): those of src outside the edited range,
//     shifted; none may appear or move;
//   - the threads: every annotation of src outside the edited range keeps
//     exactly its comments. The marks written by the edit form their own
//     annotation, except for a reply (joinsAt >= 0), whose comment joins the
//     annotation that ends at offset joinsAt; and the annotation that starts at
//     offset cuts (if cuts >= 0) may lose the comments inside the edit.
//
// It catches malformed marks of the surrounding text (an unclosed "{++", for
// instance) that would capture the new mark, or that a removal would wake up.
func verify(src []byte, e Edit, joinsAt, cuts int) (Edit, error) {
	if e.Start < 0 || e.End > len(src) || e.Start > e.End {
		return Edit{}, ErrRange
	}
	before, beforeIssues := critic.Parse(src)
	inserted, insertedIssues := critic.Parse(e.Insert)
	if len(insertedIssues) > 0 {
		return Edit{}, ErrDelimiter
	}
	delta := len(e.Insert) - (e.End - e.Start)
	// where is an old mark: -1 before the edit, +1 after, 0 inside.
	where := func(m critic.Mark) (int, error) {
		switch {
		case m.Span.End <= e.Start:
			return -1, nil
		case m.Span.Start >= e.End:
			return +1, nil
		case m.Span.Start >= e.Start && m.Span.End <= e.End:
			return 0, nil
		}
		return 0, ErrOverlap
	}
	var want []key
	for _, m := range before {
		if w, err := where(m); err != nil {
			return Edit{}, err
		} else if w < 0 {
			want = append(want, keyOf(m, 0))
		}
	}
	for _, m := range inserted {
		want = append(want, keyOf(m, e.Start))
	}
	for _, m := range before {
		if w, _ := where(m); w > 0 {
			want = append(want, keyOf(m, delta))
		}
	}
	var wantIssues []critic.Issue
	for _, is := range beforeIssues {
		switch {
		case is.Offset < e.Start:
			wantIssues = append(wantIssues, is)
		case is.Offset >= e.End:
			wantIssues = append(wantIssues, critic.Issue{Offset: is.Offset + delta, Code: is.Code})
		}
	}

	out := e.Apply(src)
	after, afterIssues := critic.Parse(out)
	if len(after) != len(want) || len(afterIssues) != len(wantIssues) {
		return Edit{}, ErrNeighbour
	}
	for i, m := range after {
		if keyOf(m, 0) != want[i] {
			return Edit{}, ErrNeighbour
		}
	}
	for i, is := range afterIssues {
		if is != wantIssues[i] {
			return Edit{}, ErrNeighbour
		}
	}

	// Threads: expected membership of every annotation, in order.
	var groups [][]key
	joined := false
	for _, g := range critic.Group(before) {
		members := append([]int{g.Main}, g.Comments...)
		w0, _ := where(before[members[0]])
		var row []key
		for _, m := range members {
			w, _ := where(before[m])
			if w != w0 && g.Span.Start != cuts {
				return Edit{}, ErrOverlap // an annotation cut by the edit
			}
			switch {
			case w < 0:
				row = append(row, keyOf(before[m], 0))
			case w > 0:
				row = append(row, keyOf(before[m], delta))
			}
		}
		if joinsAt >= 0 && w0 < 0 && g.Span.End == joinsAt {
			for _, m := range inserted {
				row = append(row, keyOf(m, e.Start))
			}
			joined = true
		}
		if len(row) > 0 {
			groups = append(groups, row)
		}
	}
	if joinsAt >= 0 && !joined {
		return Edit{}, ErrNeighbour
	}
	if joinsAt < 0 {
		var fresh [][]key
		for _, g := range critic.Group(inserted) {
			row := []key{keyOf(inserted[g.Main], e.Start)}
			for _, c := range g.Comments {
				row = append(row, keyOf(inserted[c], e.Start))
			}
			fresh = append(fresh, row)
		}
		groups = insertSorted(groups, fresh)
	}
	afterGroups := critic.Group(after)
	if len(afterGroups) != len(groups) {
		return Edit{}, ErrNeighbour
	}
	for i, g := range afterGroups {
		members := append([]int{g.Main}, g.Comments...)
		if len(members) != len(groups[i]) {
			return Edit{}, ErrNeighbour
		}
		for j, m := range members {
			if keyOf(after[m], 0) != groups[i][j] {
				return Edit{}, ErrNeighbour
			}
		}
	}
	return e, nil
}

// insertSorted merges fresh annotations into groups, ordered by start.
func insertSorted(groups, fresh [][]key) [][]key {
	out := make([][]key, 0, len(groups)+len(fresh))
	i, j := 0, 0
	for i < len(groups) || j < len(fresh) {
		if j == len(fresh) || (i < len(groups) && groups[i][0].start < fresh[j][0].start) {
			out = append(out, groups[i])
			i++
		} else {
			out = append(out, fresh[j])
			j++
		}
	}
	return out
}

// Comment comments a passage: with an empty span, it inserts {>>note<<} at
// s.Start; otherwise it replaces the passage with {==passage==}{>>note<<}.
func Comment(src []byte, s critic.Span, note string) (Edit, error) {
	d := parse(src)
	if err := checkText([]byte(note), false); err != nil {
		return Edit{}, err
	}
	if err := d.checkSpan(s, s.Start == s.End); err != nil {
		return Edit{}, err
	}
	c := wrap(critic.Comment, []byte(note))
	if s.Start == s.End {
		return verify(src, Edit{s.Start, s.End, c}, -1, -1)
	}
	passage := src[s.Start:s.End]
	if err := checkText(passage, false); err != nil {
		return Edit{}, err
	}
	return verify(src, Edit{s.Start, s.End, append(wrap(critic.Highlight, passage), c...)}, -1, -1)
}

// ProposeAddition proposes to insert text at offset at.
func ProposeAddition(src []byte, at int, text string) (Edit, error) {
	d := parse(src)
	if err := checkText([]byte(text), false); err != nil {
		return Edit{}, err
	}
	if err := d.checkSpan(critic.Span{Start: at, End: at}, false); err != nil {
		return Edit{}, err
	}
	return verify(src, Edit{at, at, wrap(critic.Addition, []byte(text))}, -1, -1)
}

// ProposeDeletion proposes to delete the passage s.
func ProposeDeletion(src []byte, s critic.Span) (Edit, error) {
	d := parse(src)
	if err := d.checkSpan(s, false); err != nil {
		return Edit{}, err
	}
	old := src[s.Start:s.End]
	if err := checkText(old, false); err != nil {
		return Edit{}, err
	}
	return verify(src, Edit{s.Start, s.End, wrap(critic.Deletion, old)}, -1, -1)
}

// ProposeSubstitution proposes to replace the passage s with text. The old
// text is always taken from src.
func ProposeSubstitution(src []byte, s critic.Span, text string) (Edit, error) {
	d := parse(src)
	if err := d.checkSpan(s, false); err != nil {
		return Edit{}, err
	}
	old := src[s.Start:s.End]
	if err := checkText(old, false); err != nil {
		return Edit{}, err
	}
	if bytes.Contains(old, []byte(critic.Arrow)) {
		return Edit{}, ErrArrow
	}
	if err := checkText([]byte(text), false); err != nil {
		return Edit{}, err
	}
	return verify(src, Edit{s.Start, s.End, wrap(critic.Substitution, old, []byte(critic.Arrow), []byte(text))}, -1, -1)
}

// Find returns the annotation that starts at offset start, with the marks of
// src. Callers identify annotations by their position in the text they have
// just read; the identity scheme of D-19 will come on top of this.
func Find(src []byte, start int) (critic.Annotation, []critic.Mark, error) {
	marks, _ := critic.Parse(src)
	for _, a := range critic.Group(marks) {
		if a.Span.Start == start {
			return a, marks, nil
		}
	}
	return critic.Annotation{}, nil, fmt.Errorf("%w : aucune annotation à l'octet %d", ErrRange, start)
}

// checkAnnotation validates an annotation received from a caller: indices
// and spans consistent with marks and src, and no nested mark inside it (such
// a zone is read-only, SPEC N7).
func checkAnnotation(src []byte, marks []critic.Mark, a critic.Annotation) error {
	if a.Main < 0 || a.Main >= len(marks) {
		return fmt.Errorf("%w : annotation inconnue", ErrRange)
	}
	m := marks[a.Main]
	end := m.Span.End
	for _, c := range a.Comments {
		if c < 0 || c >= len(marks) || marks[c].Kind != critic.Comment || marks[c].Span.Start != end {
			return fmt.Errorf("%w : fil incohérent", ErrRange)
		}
		end = marks[c].Span.End
	}
	if m.Span.Start < 0 || a.Span.Start != m.Span.Start || a.Span.End != end || end > len(src) {
		return fmt.Errorf("%w : annotation hors du texte", ErrRange)
	}
	_, issues := critic.Parse(src)
	for _, is := range issues {
		if is.Code == critic.Nested && is.Offset >= a.Span.Start && is.Offset < a.Span.End {
			return ErrNested
		}
	}
	return nil
}

// Reply adds a comment at the end of the thread of annotation a.
func Reply(src []byte, marks []critic.Mark, a critic.Annotation, note string) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	if err := checkText([]byte(note), false); err != nil {
		return Edit{}, err
	}
	return verify(src, Edit{a.Span.End, a.Span.End, wrap(critic.Comment, []byte(note))}, a.Span.End, -1)
}

// outcome is what an annotation becomes when it is closed.
type outcome int

const (
	keepNew outcome = iota // accept: the proposed text stays
	keepOld                // reject or withdraw: the original text stays
)

// closeAnnotation replaces the whole annotation (mark and thread) with the text it
// leaves behind.
func closeAnnotation(src []byte, marks []critic.Mark, a critic.Annotation, o outcome) Edit {
	m := marks[a.Main]
	var keep []byte
	switch {
	case m.Kind == critic.Addition && o == keepNew,
		m.Kind == critic.Deletion && o == keepOld,
		m.Kind == critic.Substitution && o == keepOld,
		m.Kind == critic.Highlight:
		keep = src[m.Text.Start:m.Text.End]
	case m.Kind == critic.Substitution && o == keepNew:
		keep = src[m.New.Start:m.New.End]
	}
	return Edit{a.Span.Start, a.Span.End, append([]byte(nil), keep...)}
}

func isProposal(k critic.Kind) bool {
	return k == critic.Addition || k == critic.Deletion || k == critic.Substitution
}

// Accept applies a proposal: the proposed text stays, the mark and its thread
// disappear.
func Accept(src []byte, marks []critic.Mark, a critic.Annotation) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	if !isProposal(marks[a.Main].Kind) {
		return Edit{}, ErrKind
	}
	return verify(src, closeAnnotation(src, marks, a, keepNew), -1, -1)
}

// Reject discards a proposal: the original text stays, the mark and its
// thread disappear.
func Reject(src []byte, marks []critic.Mark, a critic.Annotation) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	if !isProposal(marks[a.Main].Kind) {
		return Edit{}, ErrKind
	}
	return verify(src, closeAnnotation(src, marks, a, keepOld), -1, -1)
}

// Resolve closes a discussion (a comment, or a highlighted passage and its
// comments): the passage stays, the marks disappear.
func Resolve(src []byte, marks []critic.Mark, a critic.Annotation) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	if isProposal(marks[a.Main].Kind) {
		return Edit{}, ErrKind
	}
	return verify(src, closeAnnotation(src, marks, a, keepOld), -1, -1)
}

// Withdraw removes an annotation as if it had never been made: the original
// text is restored, whatever the kind.
func Withdraw(src []byte, marks []critic.Mark, a critic.Annotation) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	return verify(src, closeAnnotation(src, marks, a, keepOld), -1, -1)
}

// RemoveComment removes one comment of a thread (for instance a reply made by
// mistake). The main mark of an annotation cannot be removed this way.
func RemoveComment(src []byte, marks []critic.Mark, a critic.Annotation, index int) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	for _, c := range a.Comments {
		if c == index {
			m := marks[c]
			return verify(src, Edit{m.Span.Start, m.Span.End, nil}, -1, a.Span.Start)
		}
	}
	return Edit{}, fmt.Errorf("%w : la marque %d n'est pas une réponse de cette annotation", ErrKind, index)
}

// RemoveRange removes the comments of the thread of a that lie exactly in
// span (a reply and its technical comment, for instance). The main mark
// cannot be removed this way.
func RemoveRange(src []byte, marks []critic.Mark, a critic.Annotation, span critic.Span) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	startOK, endOK := false, false
	for _, c := range a.Comments {
		startOK = startOK || marks[c].Span.Start == span.Start
		endOK = endOK || marks[c].Span.End == span.End
	}
	if !startOK || !endOK || span.Start >= span.End {
		return Edit{}, fmt.Errorf("%w : la plage ne correspond pas à des réponses de cette annotation", ErrKind)
	}
	return verify(src, Edit{span.Start, span.End, nil}, -1, a.Span.Start)
}

// Revise corrects a proposal still pending: its main mark is rewritten so
// that it proposes text instead (SPEC F7, P3.2), and nothing else changes,
// its thread and its technical comment (its identity) included. The old text
// of the proposal stays as it was, taken from src:
//
//   - an addition proposes text instead ({++text++});
//   - a deletion becomes a substitution of its text by text, or stays a
//     deletion if text is empty;
//   - a substitution proposes text instead, or becomes a deletion if text is
//     empty.
//
// A correction that would propose nothing (an empty addition, a substitution
// by its own old text) is refused: the proposal is rejected instead.
func Revise(src []byte, marks []critic.Mark, a critic.Annotation, text string) (Edit, error) {
	if err := checkAnnotation(src, marks, a); err != nil {
		return Edit{}, err
	}
	m := marks[a.Main]
	if !isProposal(m.Kind) {
		return Edit{}, ErrKind
	}
	if err := checkText([]byte(text), true); err != nil {
		return Edit{}, err
	}
	old := src[m.Text.Start:m.Text.End]
	var mark []byte
	switch {
	case m.Kind == critic.Addition:
		if text == "" {
			return Edit{}, ErrEmpty
		}
		mark = wrap(critic.Addition, []byte(text))
	case text == "":
		mark = wrap(critic.Deletion, old)
	case bytes.Equal(old, []byte(text)):
		return Edit{}, ErrSame
	case bytes.Contains(old, []byte(critic.Arrow)):
		return Edit{}, ErrArrow
	default:
		mark = wrap(critic.Substitution, old, []byte(critic.Arrow), []byte(text))
	}
	if bytes.Equal(mark, src[m.Span.Start:m.Span.End]) {
		return Edit{}, ErrSame
	}
	return verifyRevise(src, Edit{Start: m.Span.Start, End: m.Span.End, Insert: mark})
}

// ErrSame is returned by Revise when the correction proposes nothing new.
var ErrSame = errors.New("la correction ne change rien à la proposition")

// verifyRevise checks the correction of a main mark (the range of e): after the edit, the
// text holds the same marks (the main one replaced by the new one, the others
// shifted), the same problems, and the same threads, the corrected annotation
// keeping all its comments.
func verifyRevise(src []byte, e Edit) (Edit, error) {
	before, beforeIssues := critic.Parse(src)
	inserted, insertedIssues := critic.Parse(e.Insert)
	if len(insertedIssues) > 0 || len(inserted) != 1 || inserted[0].Span.Len() != len(e.Insert) {
		return Edit{}, ErrDelimiter
	}
	delta := len(e.Insert) - (e.End - e.Start)
	shift := func(m critic.Mark) key {
		switch {
		case m.Span.End <= e.Start:
			return keyOf(m, 0)
		case m.Span.Start == e.Start && m.Span.End == e.End:
			return keyOf(inserted[0], e.Start)
		}
		return keyOf(m, delta)
	}
	out := e.Apply(src)
	after, afterIssues := critic.Parse(out)
	if len(after) != len(before) || len(afterIssues) != len(beforeIssues) {
		return Edit{}, ErrNeighbour
	}
	for i, m := range before {
		if m.Span.Start < e.End && m.Span.End > e.Start && !(m.Span.Start == e.Start && m.Span.End == e.End) {
			return Edit{}, ErrOverlap
		}
		if keyOf(after[i], 0) != shift(m) {
			return Edit{}, ErrNeighbour
		}
	}
	for i, is := range beforeIssues {
		want := is
		if is.Offset >= e.End {
			want.Offset += delta
		} else if is.Offset >= e.Start {
			return Edit{}, ErrNeighbour
		}
		if afterIssues[i] != want {
			return Edit{}, ErrNeighbour
		}
	}
	bg, ag := critic.Group(before), critic.Group(after)
	if len(bg) != len(ag) {
		return Edit{}, ErrNeighbour
	}
	for i := range bg {
		bm := append([]int{bg[i].Main}, bg[i].Comments...)
		am := append([]int{ag[i].Main}, ag[i].Comments...)
		if len(bm) != len(am) {
			return Edit{}, ErrNeighbour
		}
		for j := range bm {
			if keyOf(after[am[j]], 0) != shift(before[bm[j]]) {
				return Edit{}, ErrNeighbour
			}
		}
	}
	return e, nil
}

// Check verifies an edit built outside this package (a rewritten block, for
// instance) with the same rules as the operations: nothing changes outside
// the edited range, no malformed mark appears or moves, no thread changes
// owner.
func Check(src []byte, e Edit) (Edit, error) {
	return verify(src, e, -1, -1)
}
