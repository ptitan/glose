// Package critic parses CriticMarkup marks in a text, without ever modifying
// it. It works on bytes and reports byte offsets.
//
// The five marks are:
//
//	{++ added ++}   {-- deleted --}   {~~ old ~> new ~~}
//	{>> comment <<} {== highlighted ==}
//
// The grammar is the CriticMarkup one: a mark may span several lines, and
// marks do not nest (a mark ends at the first closing delimiter of its kind).
// One rule comes from Markdown (D-24): a mark written inside code, a code span
// (`…`) or a fenced code block (``` or ~~~), is literal text, so that a text
// may quote the syntax. Code is found before the marks, by the CommonMark
// rules on backtick strings and fences (see code.go).
package critic

import (
	"bytes"
	"fmt"
)

// Kind is the kind of a mark.
type Kind int

const (
	Addition Kind = iota
	Deletion
	Substitution
	Comment
	Highlight
)

func (k Kind) String() string {
	return [...]string{"ajout", "suppression", "remplacement", "commentaire", "surlignage"}[k]
}

// Delimiters of each kind, and the separator of a substitution.
var (
	openers = [...][]byte{
		Addition: []byte("{++"), Deletion: []byte("{--"), Substitution: []byte("{~~"),
		Comment: []byte("{>>"), Highlight: []byte("{=="),
	}
	closers = [...][]byte{
		Addition: []byte("++}"), Deletion: []byte("--}"), Substitution: []byte("~~}"),
		Comment: []byte("<<}"), Highlight: []byte("==}"),
	}
	arrow = []byte("~>")
)

// Opener and Closer return the delimiters of a kind.
func Opener(k Kind) string { return string(openers[k]) }
func Closer(k Kind) string { return string(closers[k]) }

// Arrow is the separator between the old and the new text of a substitution.
const Arrow = "~>"

// Span is a half-open byte range [Start, End).
type Span struct{ Start, End int }

// Len returns the length of the span in bytes.
func (s Span) Len() int { return s.End - s.Start }

// Mark is one CriticMarkup mark.
type Mark struct {
	Kind Kind
	Span Span // the whole mark, delimiters included
	Text Span // the content; for a substitution, the old text
	New  Span // for a substitution only: the new text
}

// IssueCode identifies a problem found while parsing.
type IssueCode int

const (
	// Unclosed: an opening delimiter without its closing one. The opener is
	// kept as ordinary text.
	Unclosed IssueCode = iota
	// MissingArrow: a substitution without "~>". It is kept as ordinary text.
	MissingArrow
	// Nested: a mark contains the opening delimiter of another mark. The outer
	// mark is kept; the inner one is part of its content.
	Nested
	// Empty: a mark without content (or a substitution whose old and new texts
	// are both empty).
	Empty
)

func (c IssueCode) String() string {
	return [...]string{"marque non fermée", "remplacement sans « ~> »", "marques imbriquées", "marque vide"}[c]
}

// Issue is a problem at a byte offset.
type Issue struct {
	Offset int
	Code   IssueCode
}

func (i Issue) String() string { return fmt.Sprintf("octet %d : %s", i.Offset, i.Code) }

// Parse returns the marks of src in order, and the problems found. Parse never
// fails: malformed marks are reported and treated as ordinary text.
func Parse(src []byte) ([]Mark, []Issue) {
	r := Scan(src)
	return r.Marks, r.Issues
}

// Result is the full analysis of a text.
type Result struct {
	Marks  []Mark
	Issues []Issue
	// Code holds the code spans and fenced code blocks, in order: their bytes
	// are literal, marks included.
	Code []Span
	// Literal holds the offsets of the mark openers ("{++", "{>>"…) found
	// inside code and therefore left literal (D-24): a hint for « glose check ».
	Literal []int
}

// Scan analyses src: code first, then the marks outside code.
func Scan(src []byte) Result {
	var marks []Mark
	var issues []Issue
	var code []Span
	cs := newCodeScanner(src)
	// One search cache per closing delimiter, and one for the arrow, so that a
	// text full of unclosed openers is parsed in linear time.
	var closer [len(closers)]finder
	for k := range closers {
		closer[k] = finder{src: src, sep: closers[k]}
	}
	arrows := finder{src: src, sep: arrow}
	// The opening line of a fenced block (its info string) is not code: its
	// marks count, if they end on that line. infoEnd is its end, and jump the
	// end of the block, where the scan resumes.
	infoEnd, jump := -1, 0
	for i := cs.bom; i < len(src); {
		if infoEnd >= 0 && i >= infoEnd {
			i, infoEnd = jump, -1
			continue
		}
		if infoEnd < 0 && cs.lineStart(i) {
			if end, ok := cs.fence(i); ok {
				eol := cs.lineEnd(i)
				if eol+1 < end {
					code = append(code, Span{eol + 1, end})
				}
				infoEnd, jump = eol, max(end, eol)
				// The fence characters themselves are not marks.
				f := i + bytes.IndexAny(src[i:], "`~")
				i = f + runLen(src, f, src[f])
				continue
			}
		}
		j := bytes.IndexAny(src[i:], "{`\n")
		if j < 0 {
			if infoEnd >= 0 {
				i = infoEnd
				continue
			}
			break
		}
		j += i
		if infoEnd >= 0 && j >= infoEnd {
			i = infoEnd
			continue
		}
		switch src[j] {
		case '\n':
			i = j + 1
			continue
		case '`':
			if infoEnd >= 0 {
				i = j + 1 // backticks of an info string are not code
				continue
			}
			if s, e, ok := cs.codeSpan(j); ok {
				code = append(code, Span{s, e})
				i = e
			} else {
				i = e
			}
			continue
		}
		k, ok := openerAt(src, j)
		if !ok {
			i = j + 1
			continue
		}
		bodyStart := j + len(openers[k])
		bodyEnd := closer[k].next(bodyStart)
		if bodyEnd < 0 {
			issues = append(issues, Issue{j, Unclosed})
			i = j + 1
			continue
		}
		if infoEnd >= 0 && bodyEnd+len(closers[k]) > infoEnd {
			i = j + 1 // a mark of an info string ends on its line
			continue
		}
		m := Mark{Kind: k, Span: Span{j, bodyEnd + len(closers[k])}, Text: Span{bodyStart, bodyEnd}}
		if k == Substitution {
			a := arrows.next(bodyStart)
			if a < 0 || a+len(arrow) > bodyEnd {
				issues = append(issues, Issue{j, MissingArrow})
				i = j + 1
				continue
			}
			m.Text = Span{bodyStart, a}
			m.New = Span{a + len(arrow), bodyEnd}
		}
		if containsOpener(src[bodyStart:bodyEnd]) {
			issues = append(issues, Issue{j, Nested})
		}
		if m.Text.Len() == 0 && (k != Substitution || m.New.Len() == 0) {
			issues = append(issues, Issue{j, Empty})
		}
		marks = append(marks, m)
		i = m.Span.End
	}
	return Result{Marks: marks, Issues: issues, Code: code, Literal: literalOpeners(src, code)}
}

// literalOpeners returns the offsets of the mark openers inside code.
func literalOpeners(src []byte, code []Span) []int {
	var out []int
	for _, c := range code {
		for i := c.Start; i < c.End; {
			j := bytes.IndexByte(src[i:c.End], '{')
			if j < 0 {
				break
			}
			j += i
			if _, ok := openerAt(src[:c.End], j); ok {
				out = append(out, j)
				i = j + 3
				continue
			}
			i = j + 1
		}
	}
	return out
}

// finder returns the first occurrence of sep at or after a position. It
// remembers its last answer: the first occurrence at or after "from" is "at"
// (-1 if none), which also answers any query between from and at.
type finder struct {
	src      []byte
	sep      []byte
	from, at int
	valid    bool
}

func (f *finder) next(s int) int {
	if f.valid && s >= f.from && (f.at < 0 || s <= f.at) {
		return f.at
	}
	f.from, f.valid = s, true
	if n := bytes.Index(f.src[s:], f.sep); n >= 0 {
		f.at = s + n
	} else {
		f.at = -1
	}
	return f.at
}

func openerAt(src []byte, i int) (Kind, bool) {
	for k, o := range openers {
		if bytes.HasPrefix(src[i:], o) {
			return Kind(k), true
		}
	}
	return 0, false
}

func containsOpener(b []byte) bool {
	for _, o := range openers {
		if bytes.Contains(b, o) {
			return true
		}
	}
	return false
}

// ContainsDelimiter reports whether text contains a delimiter that would end
// or open a mark if text were written inside a mark: such a text cannot be
// represented (CriticMarkup has no escaping).
func ContainsDelimiter(text []byte) bool {
	if containsOpener(text) {
		return true
	}
	for _, c := range closers {
		if bytes.Contains(text, c) {
			return true
		}
	}
	return false
}

// Annotation groups a mark with the comments that follow it immediately
// (no byte in between): the discussion thread of that mark.
type Annotation struct {
	Main     int   // index in the marks slice of the first mark of the group
	Comments []int // indices of the comments attached to Main, in order
	Span     Span  // from the start of Main to the end of the last comment
}

// Group gathers marks into annotations. A comment that starts exactly where
// the previous mark ends belongs to the previous annotation; any other mark
// starts a new annotation.
func Group(marks []Mark) []Annotation {
	var out []Annotation
	for i, m := range marks {
		if n := len(out); n > 0 && m.Kind == Comment && out[n-1].Span.End == m.Span.Start {
			out[n-1].Comments = append(out[n-1].Comments, i)
			out[n-1].Span.End = m.Span.End
			continue
		}
		out = append(out, Annotation{Main: i, Span: m.Span})
	}
	return out
}
