// Package sourcemap holds, for one rendered file, the elements whose place in
// the source is known exactly, and validates the ranges that the browser asks
// to annotate: boundaries on known characters, a single block, and no cut
// through an inline element (bold, link, formula…).
//
// The browser converts its own positions (UTF-16) into source bytes with the
// data-g attributes of the rendered words; the server never trusts them
// without this check.
package sourcemap

import (
	"errors"

	"github.com/ptitan/glose/internal/grapheme"
	"github.com/ptitan/glose/internal/sourcepos"
)

// Kind of an item.
type Kind int

const (
	Word      Kind = iota // a word (pandoc Str)
	Space                 // a space between words
	Container             // an inline element that holds others (bold, link, note…)
	Block                 // a block (paragraph, title, cell, item…)
)

// Item is an element of the rendered file whose source range is exact.
type Item struct {
	Kind       Kind
	Start, End int  // byte range in the source file
	Certified  bool // words: the bytes are exactly the rendered text
	Parent     int  // index of the enclosing item, or -1
}

// Map is the source map of one version of one file.
type Map struct {
	Items  []Item
	scopes map[int]bool // blocks added by Scope
}

// Scope adds a block whose source range is not given by pandoc (a table cell,
// the content of a footnote): it separates its words from the enclosing block.
// Its range is computed from its words by Finish.
func (m *Map) Scope(parent int) int {
	i := m.Add(Item{Kind: Block, Start: 0, End: 0, Parent: parent})
	if m.scopes == nil {
		m.scopes = map[int]bool{}
	}
	m.scopes[i] = true
	return i
}

// Finish computes the range of each scope from its words.
func (m *Map) Finish() {
	seen := map[int]bool{}
	for _, i := range m.words() {
		it := m.Items[i]
		b := m.block(i)
		if b < 0 || !m.scopes[b] {
			continue
		}
		sc := &m.Items[b]
		if !seen[b] {
			sc.Start, sc.End, seen[b] = it.Start, it.End, true
		}
		sc.Start, sc.End = min(sc.Start, it.Start), max(sc.End, it.End)
	}
	// The inline elements of a scope belong to it, delimiters included
	// (« **bold** tail » in a note): a widened selection stays inside.
	for i, it := range m.Items {
		if it.Kind != Container {
			continue
		}
		if b := m.block(i); b >= 0 && m.scopes[b] && seen[b] {
			sc := &m.Items[b]
			sc.Start, sc.End = min(sc.Start, it.Start), max(sc.End, it.End)
		}
	}
}

// Add appends an item and returns its index.
func (m *Map) Add(it Item) int {
	m.Items = append(m.Items, it)
	return len(m.Items) - 1
}

// Errors of Check.
var (
	ErrBoundary = errors.New("une borne de la sélection ne correspond pas à un caractère connu du texte")
	ErrBlocks   = errors.New("la sélection s'étend sur plusieurs blocs (paragraphes, titres, cellules…)")
	ErrEmpty    = errors.New("sélection vide")
)

// Result of Check.
type Result struct {
	Start, End int  // the range to annotate
	Widened    bool // true if it had to be widened to whole inline elements
}

func (m *Map) words() []int {
	var w []int
	for i, it := range m.Items {
		if it.Kind == Word || it.Kind == Space {
			w = append(w, i)
		}
	}
	return w
}

// bounds returns the words, spaces and inline elements: the items whose
// edges are valid boundaries of a passage (an inline element counts as a
// whole, delimiters included, as in a widened selection).
func (m *Map) bounds() []int {
	var w []int
	for i, it := range m.Items {
		if it.Kind != Block {
			w = append(w, i)
		}
	}
	return w
}

// startWord returns the item whose range allows s as a start boundary.
func (m *Map) startWord(src []byte, s int) (int, bool) {
	for _, i := range m.bounds() {
		it := m.Items[i]
		if s == it.Start {
			return i, true
		}
		if it.Kind == Word && it.Certified && s > it.Start && s < it.End && s < len(src) && grapheme.Boundary(src, s) {
			return i, true
		}
	}
	return -1, false
}

// endWord returns the item whose range allows e as an end boundary.
func (m *Map) endWord(src []byte, e int) (int, bool) {
	for _, i := range m.bounds() {
		it := m.Items[i]
		if e == it.End {
			return i, true
		}
		if it.Kind == Word && it.Certified && e > it.Start && e < it.End && e < len(src) && grapheme.Boundary(src, e) {
			return i, true
		}
	}
	return -1, false
}

// block returns the innermost block that encloses item i, or -1.
func (m *Map) block(i int) int {
	for p := m.Items[i].Parent; p >= 0; p = m.Items[p].Parent {
		if m.Items[p].Kind == Block {
			return p
		}
	}
	return -1
}

// computeInner returns, for each container, the range covered by its words.
// It is computed at each Check and never stored: a map is shared by the
// concurrent requests of the server and must stay read-only once built.
func (m *Map) computeInner() map[int][2]int {
	inner := map[int][2]int{}
	for _, i := range m.words() {
		it := m.Items[i]
		for p := it.Parent; p >= 0; p = m.Items[p].Parent {
			if m.Items[p].Kind != Container {
				continue
			}
			r, ok := inner[p]
			if !ok {
				r = [2]int{it.Start, it.End}
			}
			r[0], r[1] = min(r[0], it.Start), max(r[1], it.End)
			inner[p] = r
		}
	}
	return inner
}

// partial reports whether [s, e) cuts through container c: it overlaps it
// without containing it and without staying inside its words.
func (m *Map) partial(inner map[int][2]int, c, s, e int) bool {
	it := m.Items[c]
	if !(s < it.End && e > it.Start) || (s <= it.Start && e >= it.End) {
		return false
	}
	if r, ok := inner[c]; ok && s >= r[0] && e <= r[1] {
		return false
	}
	return true
}

// Check validates the range [s, e) of the source src for a passage comment.
// An unsafe range is widened to whole inline elements; the caller must show
// the widened passage and ask for confirmation (SPEC F5).
func (m *Map) Check(src []byte, s, e int) (Result, error) {
	if s >= e {
		return Result{}, ErrEmpty
	}
	// pandoc may give an emoji and its modifiers, or a flag, as separate
	// words: their edges are not all places where a text may be cut.
	if !grapheme.Boundary(src, s) || !grapheme.Boundary(src, e) {
		return Result{}, ErrBoundary
	}
	ws, ok1 := m.startWord(src, s)
	we, ok2 := m.endWord(src, e)
	if !ok1 || !ok2 {
		return Result{}, ErrBoundary
	}
	b := m.block(ws)
	if b != m.block(we) {
		return Result{}, ErrBlocks
	}
	inner := m.computeInner()
	r := Result{Start: s, End: e}
	for changed := true; changed; {
		changed = false
		for c, it := range m.Items {
			if it.Kind == Container && m.partial(inner, c, r.Start, r.End) {
				r.Start, r.End = min(r.Start, it.Start), max(r.End, it.End)
				r.Widened, changed = true, true
			}
		}
	}
	if r.Widened && b >= 0 && (r.Start < m.Items[b].Start || r.End > m.Items[b].End) {
		return Result{}, ErrBlocks
	}
	return r, nil
}

// CheckPoint validates an offset where a comment is inserted on its own: it
// must be the start or the end of a known word or space.
func (m *Map) CheckPoint(src []byte, at int) error {
	if at < 0 || at > len(src) || !grapheme.Boundary(src, at) {
		return ErrBoundary
	}
	for _, i := range m.words() {
		if it := m.Items[i]; at == it.Start || at == it.End {
			return nil
		}
	}
	return ErrBoundary
}

// BlockEnd returns the end of the last word of the block that contains the
// word or space boundary at, for a paragraph comment (SPEC F6).
func (m *Map) BlockEnd(at int) (int, error) {
	b := -2
	for _, i := range m.words() {
		if it := m.Items[i]; at >= it.Start && at <= it.End {
			b = m.block(i)
			break
		}
	}
	if b == -2 {
		return 0, ErrBoundary
	}
	end := -1
	for _, i := range m.words() {
		if it := m.Items[i]; it.Kind == Word && m.block(i) == b {
			end = max(end, it.End)
		}
	}
	if end < 0 {
		return 0, ErrBoundary
	}
	return end, nil
}

// statusOK reports whether a located word may enter the map.
func statusOK(s sourcepos.Status) bool {
	return s == sourcepos.Certified || s == sourcepos.Transformed
}

// Accepts reports whether a located word or space of the given status is kept
// in the map (certified or transformed: exact range).
func Accepts(s sourcepos.Status) bool { return statusOK(s) }

// BlockRange returns the source range of the innermost block that contains
// the word or space starting at at (or containing it), without the line
// ending and the spaces that end it: the text the author may rewrite.
func (m *Map) BlockRange(src []byte, at int) (int, int, error) {
	b := -2
	for _, i := range m.words() {
		if it := m.Items[i]; at >= it.Start && at < it.End || at == it.Start {
			b = m.block(i)
			break
		}
	}
	if b < 0 {
		return 0, 0, ErrBoundary
	}
	s, e := m.Items[b].Start, m.Items[b].End
	for e > s && (src[e-1] == '\n' || src[e-1] == '\r' || src[e-1] == ' ' || src[e-1] == '\t') {
		e--
	}
	return s, e, nil
}

// LinesRange returns the source range of the whole lines that hold the words
// from at (the start of a word or space) to to (the end of a word or space):
// from the start of the first line to the end of the last one, without its
// line ending. It serves to rewrite a structure (table, list, quotation,
// several paragraphs) in its source: pandoc does not give reliable ranges for
// these containers (a table without its header row, a list that overflows),
// but whole lines are always safe to rewrite.
func (m *Map) LinesRange(src []byte, at, to int) (int, int, error) {
	if at >= to || at < 0 || to > len(src) {
		return 0, 0, ErrEmpty
	}
	okAt, okTo := false, false
	for _, i := range m.words() {
		it := m.Items[i]
		okAt = okAt || it.Start == at
		okTo = okTo || it.End == to
	}
	if !okAt || !okTo {
		return 0, 0, ErrBoundary
	}
	s := at
	for s > 0 && src[s-1] != '\n' {
		s--
	}
	e := to
	for e < len(src) && src[e] != '\n' {
		e++
	}
	if e > s && e < len(src) && src[e-1] == '\r' {
		e--
	}
	// A byte order mark is not part of the first line.
	if s == 0 && len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		s = 3
	}
	return s, e, nil
}

// Append adds the items of o, a finished map of another part of the same
// file (a section of a big file, P6.12), after those of m.
func (m *Map) Append(o *Map) {
	shift := len(m.Items)
	for _, it := range o.Items {
		if it.Parent >= 0 {
			it.Parent += shift
		}
		m.Items = append(m.Items, it)
	}
}
