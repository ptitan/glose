// Package edit turns a block of text rewritten by the author into
// CriticMarkup proposals: the smallest changes, word by word, written around
// the original text (SPEC F7, F8). Nothing outside the block changes, and the
// result is checked: rejecting every new proposal gives back the original
// block, accepting them gives the rewritten one.
package edit

import (
	"bytes"
	"errors"
	"regexp"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/grapheme"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/projection"
)

// Errors of Propose.
var (
	ErrMarked    = errors.New("ce passage contient déjà des annotations : décidez-les ou retirez-les d'abord")
	ErrUnchanged = errors.New("aucune modification")
	ErrTooLong   = errors.New("passage trop long pour être comparé mot à mot")
	ErrCheck     = errors.New("la vérification des propositions a échoué")
)

// maxCells bounds the comparison table (tokens of the old text × tokens of
// the new one).
const maxCells = 4_000_000

// Propose returns the edit that replaces src[block] with proposals turning it
// into text. The block must not contain marks.
func Propose(src []byte, block critic.Span, text string) (patch.Edit, error) {
	return proposeEdit(src, block, text, false)
}

// ProposeWhole is Propose, but the rewriting is one proposal (D-32), to be
// accepted or rejected as a whole: a substitution from the first changed
// word to the last (whole lines if it spans several), or an addition or a
// deletion. An old text that holds « ~> » cannot be the old text of a
// substitution: the proposal is then a deletion followed by an addition.
func ProposeWhole(src []byte, block critic.Span, text string) (patch.Edit, error) {
	return proposeEdit(src, block, text, true)
}

func proposeEdit(src []byte, block critic.Span, text string, whole bool) (patch.Edit, error) {
	if block.Start < 0 || block.End > len(src) || block.Start > block.End {
		return patch.Edit{}, patch.ErrRange
	}
	marks, _ := critic.Parse(src)
	for _, m := range marks {
		if m.Span.Start < block.End && m.Span.End > block.Start {
			return patch.Edit{}, ErrMarked
		}
	}
	old := src[block.Start:block.End]
	neu := []byte(text)
	if !utf8.Valid(neu) {
		return patch.Edit{}, patch.ErrRange
	}
	// A text box gives "\n": keep the line endings of the original.
	if bytes.Contains(old, []byte("\r\n")) {
		neu = bytes.ReplaceAll(bytes.ReplaceAll(neu, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
	}
	if bytes.Equal(old, neu) {
		return patch.Edit{}, ErrUnchanged
	}
	if critic.ContainsDelimiter(neu) {
		return patch.Edit{}, patch.ErrDelimiter
	}
	a, b := Tokens(old), Tokens(neu)
	var out []byte
	switch {
	case whole:
		// The first form that reads back: a substitution that ends with
		// « { » or « ~ » would not (« {~ » then « ~> »), and the words kept
		// outside may close a code span with a sign of the proposal (D-24).
		for _, f := range []struct{ trim, split bool }{{true, false}, {true, true}, {false, false}, {false, true}} {
			out = wholeChange(old, neu, a, b, f.trim, f.split)
			if _, issues := critic.Parse(out); len(issues) == 0 &&
				bytes.Equal(projection.Project(out, projection.Current).Text, old) &&
				bytes.Equal(projection.Project(out, projection.Proposed).Text, neu) {
				break
			}
		}
	case len(a)*len(b) > maxCells:
		return patch.Edit{}, ErrTooLong
	default:
		for _, op := range slideLines(mergeLines(mergeSpaces(align(a, b)))) {
			switch {
			case op.del == nil && op.ins == nil:
				out = append(out, op.same...)
			case op.del == nil:
				out = append(out, wrap(critic.Addition, op.ins)...)
			case op.ins == nil:
				out = append(out, wrap(critic.Deletion, op.del)...)
			case bytes.Contains(op.del, []byte(critic.Arrow)):
				out = append(out, wrap(critic.Deletion, op.del)...)
				out = append(out, wrap(critic.Addition, op.ins)...)
			default:
				pre, d, n, suf := TrimSigns(op.del, op.ins)
				out = append(out, pre...)
				out = append(out, []byte(critic.Opener(critic.Substitution))...)
				out = append(out, d...)
				out = append(out, critic.Arrow...)
				out = append(out, n...)
				out = append(out, []byte(critic.Closer(critic.Substitution))...)
				out = append(out, suf...)
			}
		}
	}
	// The new block alone: well formed, Current = old, Proposed = new.
	if _, issues := critic.Parse(out); len(issues) > 0 ||
		!bytes.Equal(projection.Project(out, projection.Current).Text, old) ||
		!bytes.Equal(projection.Project(out, projection.Proposed).Text, neu) {
		return patch.Edit{}, ErrCheck
	}
	// In the file: nothing outside the block changes, no thread changes owner.
	return patch.Check(src, patch.Edit{Start: block.Start, End: block.End, Insert: out})
}

// wholeChange writes the rewriting of old into neu as one proposal: the words
// common to both ends stay outside it; a change that spans several lines
// takes whole lines (a line added or deleted starts its line, as with the
// proposals word by word).
func wholeChange(old, neu []byte, a, b [][]byte, trim, split bool) []byte {
	p, q := 0, 0
	for trim && p < len(a) && p < len(b) && bytes.Equal(a[p], b[p]) {
		p++
	}
	for trim && q < len(a)-p && q < len(b)-p && bytes.Equal(a[len(a)-1-q], b[len(b)-1-q]) {
		q++
	}
	P, Q := 0, 0
	for _, t := range a[:p] {
		P += len(t)
	}
	for _, t := range a[len(a)-q:] {
		Q += len(t)
	}
	if bytes.IndexByte(old[P:len(old)-Q], '\n') >= 0 || bytes.IndexByte(neu[P:len(neu)-Q], '\n') >= 0 {
		for P > 0 && old[P-1] != '\n' {
			P--
		}
		lineStart := func(t []byte, at int) bool { return at == 0 || t[at-1] == '\n' }
		for Q > 0 && !(lineStart(old, len(old)-Q) && lineStart(neu, len(neu)-Q)) {
			Q--
		}
	}
	o, n := old[P:len(old)-Q], neu[P:len(neu)-Q]
	out := append([]byte(nil), old[:P]...)
	switch {
	case len(o) == 0:
		out = append(out, wrap(critic.Addition, n)...)
	case len(n) == 0:
		out = append(out, wrap(critic.Deletion, o)...)
	case split || bytes.Contains(o, []byte(critic.Arrow)):
		out = append(out, wrap(critic.Deletion, o)...)
		out = append(out, wrap(critic.Addition, n)...)
	default:
		out = append(out, critic.Opener(critic.Substitution)...)
		out = append(out, o...)
		out = append(out, critic.Arrow...)
		out = append(out, n...)
		out = append(out, critic.Closer(critic.Substitution)...)
	}
	return append(out, old[len(old)-Q:]...)
}

func wrap(k critic.Kind, b []byte) []byte {
	out := append([]byte(critic.Opener(k)), b...)
	return append(out, critic.Closer(k)...)
}

var footnoteRe = regexp.MustCompile(`\[\^[^\]\s]+\]:?`)

// mathRe matches a formula between dollars, as pandoc reads it
// (tex_math_dollars): « $$…$$ », or « $…$ » whose content neither starts nor
// ends with a space. A closing « $ » followed by a digit is checked by
// formulas.
var mathRe = regexp.MustCompile(`\$\$(?:[^$]|\$[^$])+\$\$|\$[^\s$](?:[^$]*[^\s$\\])?\$`)

// formulas returns the formulas of b. A wrong guess only changes the grain of
// the proposals, never their result (Propose checks both views).
func formulas(b []byte) []critic.Span {
	var out []critic.Span
	for _, m := range mathRe.FindAllIndex(b, -1) {
		if m[1] < len(b) && b[m[1]] >= '0' && b[m[1]] <= '9' {
			continue
		}
		if m[0] > 0 && b[m[0]-1] == '\\' {
			continue
		}
		out = append(out, critic.Span{Start: m[0], End: m[1]})
	}
	return out
}

// Tokens splits b into words (letters, digits, combining marks), runs of
// spaces, and single punctuation or symbol characters (a grapheme cluster,
// such as an emoji with its modifiers, is never cut), so that a change is
// marked at the finest useful grain (« dort. » → « dort{++ bien++}. »). Code
// (a code span, a fenced block) is one token: a mark inside it would be
// literal (D-24), so a change of code replaces the whole of it.
func Tokens(b []byte) [][]byte {
	var out [][]byte
	code := critic.Scan(b).Code
	// A footnote label (« [^a] », « [^a]: ») is one token too: changing it
	// replaces it whole, so that the note it names is recognised (package
	// notes). So is a formula: a change in it replaces the whole formula,
	// which reads, and renders, as one.
	var atoms []critic.Span
	for _, m := range footnoteRe.FindAllIndex(b, -1) {
		atoms = append(atoms, critic.Span{Start: m[0], End: m[1]})
	}
	atoms = append(atoms, formulas(b)...)
	for _, m := range atoms {
		overlaps := false
		for _, c := range code {
			if m.Start < c.End && m.End > c.Start {
				overlaps = true
			}
		}
		if !overlaps {
			code = append(code, m)
		}
	}
	sort.Slice(code, func(i, j int) bool { return code[i].Start < code[j].Start })
	start, prev := 0, -1
	for i := 0; i < len(b); {
		if len(code) > 0 && i == code[0].Start {
			if start < i {
				out = append(out, b[start:i])
			}
			out = append(out, b[i:code[0].End])
			i, start, prev = code[0].End, code[0].End, -1
			code = code[1:]
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if i > start && !grapheme.Boundary(b, i) {
			// Inside a character as the reader sees it (accent, emoji with its
			// skin tone, sequence joined by U+200D, flag): never cut.
			i += size
			continue
		}
		kind := 2 // punctuation, symbol: one token per character
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			kind = 0
		case unicode.IsSpace(r):
			kind = 1
		}
		if prev >= 0 && (kind != prev || kind == 2) {
			out = append(out, b[start:i])
			start = i
		}
		prev = kind
		i += size
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return glueEmphasis(out)
}

// TrimSigns takes out of a substitution the emphasis signs common to its old
// and new texts: « **mot** » → « **terme** » is « **{~~mot~>terme~~}** ».
// Both texts keep at least one character.
func TrimSigns(old, neu []byte) (pre, o, n, suf []byte) {
	sign := func(c byte) bool { return c == '*' || c == '_' }
	p := 0
	for p < len(old)-1 && p < len(neu)-1 && sign(old[p]) && old[p] == neu[p] {
		p++
	}
	q := 0
	for q < len(old)-p-1 && q < len(neu)-p-1 && sign(old[len(old)-1-q]) && old[len(old)-1-q] == neu[len(neu)-1-q] {
		q++
	}
	return old[:p], old[p : len(old)-q], neu[p : len(neu)-q], old[len(old)-q:]
}

// glueEmphasis joins a run of emphasis signs (« ** », « _ ») to the word it
// touches: putting a word in bold is then one change, « outil » →
// « **outil** », and not two additions of « ** » that could be accepted apart.
func glueEmphasis(ts [][]byte) [][]byte {
	isSign := func(t []byte) bool { return len(t) == 1 && (t[0] == '*' || t[0] == '_') }
	isWord := func(t []byte) bool {
		r, _ := utf8.DecodeRune(t)
		return len(t) > 0 && (unicode.IsLetter(r) || unicode.IsDigit(r))
	}
	var out [][]byte
	for i := 0; i < len(ts); {
		if !isSign(ts[i]) {
			out = append(out, ts[i])
			i++
			continue
		}
		j := i
		var run []byte
		for j < len(ts) && isSign(ts[j]) && ts[j][0] == ts[i][0] {
			run = append(run, ts[j]...)
			j++
		}
		switch {
		case j < len(ts) && isWord(ts[j]): // « **mot »
			out = append(out, append(run, ts[j]...))
			j++
		case len(out) > 0 && isWord(lastRune(out[len(out)-1])): // « mot** »
			out[len(out)-1] = append(append([]byte(nil), out[len(out)-1]...), run...)
		default:
			for k := i; k < j; k++ {
				out = append(out, ts[k])
			}
		}
		i = j
	}
	return out
}

// lastRune returns the last character of t.
func lastRune(t []byte) []byte {
	_, n := utf8.DecodeLastRune(t)
	return t[len(t)-n:]
}

// op is one step of an alignment: equal text, or a change (deleted and/or
// inserted text).
type op struct {
	same, del, ins []byte
}

// align compares two token lists (longest common subsequence) and returns the
// steps, consecutive changes being merged.
func align(a, b [][]byte) []op {
	n, m := len(a), len(b)
	// lcs[i][j] = length of the LCS of a[i:] and b[j:], in one flat table.
	w := m + 1
	lcs := make([]int32, (n+1)*w)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if bytes.Equal(a[i], b[j]) {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}
	var ops []op
	var cur *op
	flush := func() {
		if cur != nil {
			ops = append(ops, *cur)
			cur = nil
		}
	}
	change := func() *op {
		if cur == nil || cur.same != nil {
			flush()
			cur = &op{}
		}
		return cur
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && bytes.Equal(a[i], b[j]):
			if cur == nil || cur.same == nil {
				flush()
				cur = &op{same: []byte{}}
			}
			cur.same = append(cur.same, a[i]...)
			i, j = i+1, j+1
		case j < m && (i == n || lcs[i*w+j+1] >= lcs[(i+1)*w+j]):
			c := change()
			c.ins = append(c.ins, b[j]...)
			j++
		default:
			c := change()
			c.del = append(c.del, a[i]...)
			i++
		}
	}
	flush()
	return ops
}

// mergeSpaces joins two changes separated only by spaces (no line break)
// into one: « {~~Le~>La~~} {~~site~>spécification~~} » becomes « {~~Le
// site~>La spécification~~} ». Accepting half of a rewording would leave a
// text that nobody wrote.
func mergeSpaces(ops []op) []op {
	var out []op
	for _, o := range ops {
		n := len(out)
		if o.same == nil && n >= 2 && out[n-2].same == nil && len(out[n-1].same) > 0 &&
			len(bytes.TrimSpace(out[n-1].same)) == 0 && !bytes.ContainsAny(out[n-1].same, "\r\n") {
			ws := out[n-1].same
			prev := out[n-2]
			m := op{}
			m.del = append(append(append([]byte{}, prev.del...), ws...), o.del...)
			m.ins = append(append(append([]byte{}, prev.ins...), ws...), o.ins...)
			out = append(out[:n-2], m)
			continue
		}
		out = append(out, o)
	}
	return out
}

// mergeLines joins two additions (or two deletions) separated only by spaces
// and line endings that the alignment kept as equal text: « {++A++}\n{++B\n++} »
// becomes « {++A\nB++}\n » when B ends with that text (or « \n{++A\nB++} »
// when A starts with it). A table, a list written in one go then stays one
// proposal: accepting half of it would leave a structure nobody wrote.
func mergeLines(ops []op) []op {
	for changed := true; changed; {
		changed = false
		for i := 0; i+2 < len(ops); i++ {
			x, sep, y := ops[i], ops[i+1], ops[i+2]
			if x.same != nil || y.same != nil || sep.same == nil || len(bytes.TrimSpace(sep.same)) > 0 || len(sep.same) == 0 {
				continue
			}
			var ax, ay []byte
			switch {
			case x.del == nil && y.del == nil && x.ins != nil && y.ins != nil:
				ax, ay = x.ins, y.ins
			case x.ins == nil && y.ins == nil && x.del != nil && y.del != nil:
				ax, ay = x.del, y.del
			default:
				continue
			}
			joined := append(append(append([]byte{}, ax...), sep.same...), ay...)
			var merged []op
			switch {
			case bytes.HasSuffix(ay, sep.same):
				merged = []op{{}, {same: sep.same}}
				merged[0] = withText(x, joined[:len(joined)-len(sep.same)])
			case bytes.HasPrefix(ax, sep.same):
				merged = []op{{same: sep.same}, {}}
				merged[1] = withText(x, joined[len(sep.same):])
			default:
				continue
			}
			ops = append(ops[:i], append(merged, ops[i+3:]...)...)
			changed = true
		}
	}
	// Equal texts that now follow each other are joined.
	var out []op
	for _, o := range ops {
		if n := len(out); n > 0 && o.same != nil && out[n-1].same != nil {
			out[n-1].same = append(append([]byte{}, out[n-1].same...), o.same...)
			continue
		}
		out = append(out, o)
	}
	return out
}

// withText returns an addition or a deletion like x, with text t.
func withText(x op, t []byte) op {
	if x.ins != nil {
		return op{ins: t}
	}
	return op{del: t}
}

// slideLines moves an addition or a deletion of whole lines (a list item, a
// table row, a paragraph) so that it starts at the beginning of a line and
// ends with a line break: « - b\n{++- x\n++}- c » rather than
// « - b\n- {++x\n- ++}c ». Both say the same thing (the text around is
// equal), but the first is where the author sees the change. Only shifts that
// keep the text identical are tried; without a better place, the change stays.
func slideLines(ops []op) []op {
	lineStart := func(b []byte) bool { return len(b) == 0 || b[len(b)-1] == '\n' }
	for i := 1; i+1 < len(ops); i++ {
		x := ops[i]
		if x.same != nil || (x.del == nil) == (x.ins == nil) || ops[i-1].same == nil || ops[i+1].same == nil {
			continue
		}
		text := x.ins
		if text == nil {
			text = x.del
		}
		before, after := ops[i-1].same, ops[i+1].same
		if !bytes.Contains(text, []byte("\n")) || (text[len(text)-1] == '\n' && lineStart(before)) {
			continue
		}
		var best []byte
		var nb, na []byte
		// Right: the text rotates with the start of what follows.
		for k := 1; k <= len(after) && k <= len(text) && after[k-1] == text[k-1]; k++ {
			t := append(append([]byte{}, text[k:]...), after[:k]...)
			if t[len(t)-1] == '\n' && text[k-1] == '\n' {
				best = t
				nb = append(append([]byte{}, before...), text[:k]...)
				na = after[k:]
				break
			}
		}
		// Left: the text rotates with the end of what precedes; kept if nearer.
		for k := 1; k <= len(before) && k <= len(text) && before[len(before)-k] == text[len(text)-k]; k++ {
			if best != nil && k >= len(nb)-len(before) {
				break
			}
			t := append(append([]byte{}, before[len(before)-k:]...), text[:len(text)-k]...)
			if t[len(t)-1] == '\n' && lineStart(before[:len(before)-k]) {
				best = t
				nb = before[:len(before)-k]
				na = append(append([]byte{}, text[len(text)-k:]...), after...)
				break
			}
		}
		if best == nil {
			continue
		}
		ops[i-1].same, ops[i+1].same = nb, na
		if x.ins != nil {
			ops[i].ins = best
		} else {
			ops[i].del = best
		}
	}
	return ops
}

// ProposeWithNote is Propose, followed by a comment (a question for the agent,
// or a justification for the author) at the end of the thread of the first new
// proposal. The result is one edit of the range, checked like the others.
func ProposeWithNote(src []byte, span critic.Span, text, note string) (patch.Edit, error) {
	return ProposeWithNoteWhole(src, span, text, note, false)
}

// ProposeWithNoteWhole is ProposeWithNote, with the rewriting as one proposal
// if whole is true (ProposeWhole).
func ProposeWithNoteWhole(src []byte, span critic.Span, text, note string, whole bool) (patch.Edit, error) {
	e, err := proposeEdit(src, span, text, whole)
	if err != nil || note == "" {
		return e, err
	}
	out := e.Apply(src)
	end := e.Start + len(e.Insert)
	marks, _ := critic.Parse(out)
	for _, a := range critic.Group(marks) {
		if a.Span.Start < e.Start || a.Span.End > end {
			continue
		}
		r, err := patch.Reply(out, marks, a, note)
		if err != nil {
			return patch.Edit{}, err
		}
		out = r.Apply(out)
		tail := len(src) - span.End
		return patch.Check(src, patch.Edit{Start: span.Start, End: span.End, Insert: append([]byte(nil), out[span.Start:len(out)-tail]...)})
	}
	return patch.Edit{}, ErrCheck
}
