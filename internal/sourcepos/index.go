package sourcepos

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
)

// tabStop is the tab width pandoc uses when it computes columns.
const tabStop = 4

var bom = []byte{0xEF, 0xBB, 0xBF}

// ErrNotUTF8 is returned for input that is not valid UTF-8: pandoc then
// re-reads the whole input as Latin-1, and no position can be trusted.
var ErrNotUTF8 = errors.New("le texte n'est pas en UTF-8 valide")

// Index converts pandoc positions into byte offsets of one exact text: the
// bytes that were given to pandoc.
//
// Rules observed with pandoc 3.1.3:
//   - lines end at '\n' only; every '\r' is removed by pandoc, so it counts
//     neither as a line break nor as a column;
//   - one column per Unicode code point, tabs advance to the next tab stop of 4;
//   - a byte order mark at the start of the text is ignored;
//   - pandoc normalises the text (NFC), which can merge characters (é written
//     e + combining accent, a Hangul syllable written in jamo) or split them
//     (U+0958 becomes two code points). Glose reproduces it (nfc.go, D-29):
//     a line is cut into pieces that NFC treats apart; the columns of a piece
//     are those of its NFC form. Inside a piece that NFC changes, there is no
//     exact position; before and after it, positions are exact. After a
//     character unknown to the tables, the rest of the line is inexact.
type Index struct {
	src    []byte
	starts []int // starts[i] is the byte offset of line i+1
	// marks are piece boundaries reached by Offset, every markEvery
	// columns at least, line by line and in increasing columns (P6.11): a
	// paragraph is often one long line, and starting again from the start of
	// the line for every word made the rendering quadratic in the length of
	// the lines. An Index is not safe for concurrent use.
	marks map[int][]mark
}

// mark is a piece boundary of a line: column, byte offset, and whether an
// inexact character came before.
type mark struct {
	col, off int
	doubt    bool
}

// markEvery spaces the marks of a line.
const markEvery = 32

// NewIndex builds the index of src. It fails if src is not valid UTF-8.
func NewIndex(src []byte) (*Index, error) {
	if !utf8.Valid(src) {
		return nil, ErrNotUTF8
	}
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &Index{src: src, starts: starts}, nil
}

// Bytes returns the indexed text. It must not be modified.
func (x *Index) Bytes() []byte { return x.src }

// Offset returns the byte offset of p in the text. exact is false when the
// computation went through a character that pandoc may have composed with its
// neighbour, or when p falls inside the expansion of a tab.
func (x *Index) Offset(p Pos) (offset int, exact bool, err error) {
	if p.Line < 1 || p.Col < 1 {
		return 0, false, fmt.Errorf("position %v invalide", p)
	}
	if p.Line > len(x.starts) {
		return 0, false, fmt.Errorf("ligne %d hors du texte (%d lignes)", p.Line, len(x.starts))
	}
	off := x.starts[p.Line-1]
	end := len(x.src)
	if p.Line < len(x.starts) {
		end = x.starts[p.Line] - 1 // position of the '\n'
	}
	if p.Line == 1 && bytes.HasPrefix(x.src, bom) {
		off += len(bom)
	}
	col, doubt := 1, false
	// Resume at the last mark of the line before p (strictly before: at the
	// same column, the position before a \r that pandoc ignores is wanted).
	marks := x.marks[p.Line]
	if k := sort.Search(len(marks), func(k int) bool { return marks[k].col >= p.Col }) - 1; k >= 0 {
		col, off, doubt = marks[k].col, marks[k].off, marks[k].doubt
	}
	for {
		if n := len(marks); col > 1 && (n == 0 && col > markEvery || n > 0 && col >= marks[n-1].col+markEvery) {
			if x.marks == nil {
				x.marks = map[int][]mark{}
			}
			marks = append(marks, mark{col, off, doubt})
			x.marks[p.Line] = marks
		}
		if col == p.Col {
			return off, !doubt, nil
		}
		if off >= end {
			return 0, false, fmt.Errorf("colonne %d hors de la ligne %d", p.Col, p.Line)
		}
		r, size := utf8.DecodeRune(x.src[off:])
		switch r {
		case '\r':
			// removed by pandoc: no column
			off += size
			continue
		case '\t':
			next := ((col-1)/tabStop+1)*tabStop + 1
			if p.Col > col && p.Col < next {
				return off, false, nil // inside the tab expansion
			}
			col = next
			off += size
			continue
		}
		// Fast path: a Latin character (below U+0300) followed by another
		// boundary is a piece of its own that NFC leaves as it is.
		if r < 0x300 {
			if off+size >= end {
				col++
				off += size
				continue
			}
			if r2, _ := utf8.DecodeRune(x.src[off+size:]); r2 < 0x300 || r2 == '\r' || r2 == '\t' || nfcBoundary(r2) {
				col++
				off += size
				continue
			}
		}
		// A piece: this character and those that NFC may join to it.
		pend := off + size
		for pend < end {
			r2, s2 := utf8.DecodeRune(x.src[pend:])
			if r2 == '\r' || r2 == '\t' || nfcBoundary(r2) {
				break
			}
			pend += s2
		}
		piece := []rune(string(x.src[off:pend]))
		for _, q := range piece {
			if !assigned(q) {
				doubt = true // unknown to the tables: pandoc may count it otherwise
			}
		}
		norm := nfc(piece)
		if string(norm) == string(piece) {
			// Unchanged: one column per character.
			for _, q := range piece {
				if col == p.Col {
					return off, !doubt, nil
				}
				col++
				off += utf8.RuneLen(q)
			}
			continue
		}
		if p.Col > col && p.Col < col+len(norm) {
			return off, false, nil // inside a piece that NFC changes
		}
		col += len(norm)
		off = pend
	}
}

// Span converts a pandoc range into a half-open byte range [start, end).
func (x *Index) Span(r Range) (start, end int, exact bool, err error) {
	start, e1, err := x.Offset(r.Start)
	if err != nil {
		return 0, 0, false, err
	}
	end, e2, err := x.Offset(r.End)
	if err != nil {
		return 0, 0, false, err
	}
	if end < start {
		return 0, 0, false, fmt.Errorf("plage inversée %v", r)
	}
	return start, end, e1 && e2, nil
}
