// Package notes handles footnotes in proposals: a note is a reference
// « [^a] » in the text and its definition « [^a]: … », two places of the file.
// For the author, a note added or deleted is one proposal:
//
//   - in a rewriting, « ^[text of the note] » (written where the reference
//     goes) is expanded into a reference « [^n] » and a definition, added at
//     the end of the file (Expand, Append);
//   - deleting the last reference to a note also deletes its definition
//     (Definition);
//   - the proposals that add (or delete) a reference and its definition are
//     linked (Links): deciding one decides the others.
//
// The reader of Glose (commonmark_x) knows only « [^a] » notes: « ^[…] » is a
// writing aid of the editor, never left in the file.
package notes

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ptitan/glose/internal/critic"
)

// Def is a definition to add.
type Def struct {
	Label string
	Text  string
}

var labelRe = regexp.MustCompile(`\[\^([^\]\s]+)\](:?)`)

// Norm normalises a label as pandoc does when it matches a reference to its
// definition: case and runs of spaces do not count.
func Norm(label string) string { return strings.ToLower(strings.Join(strings.Fields(label), " ")) }

// matches returns the labels of b outside code and not escaped: each with
// its normalised label and whether it is a definition.
func matches(b []byte) []struct {
	label string
	def   bool
} {
	code := critic.Scan(b).Code
	var out []struct {
		label string
		def   bool
	}
	for _, m := range labelRe.FindAllSubmatchIndex(b, -1) {
		if m[0] > 0 && b[m[0]-1] == '\\' {
			continue
		}
		inCode := false
		for _, c := range code {
			if m[0] >= c.Start && m[0] < c.End {
				inCode = true
				break
			}
		}
		if !inCode {
			out = append(out, struct {
				label string
				def   bool
			}{Norm(string(b[m[2]:m[3]])), m[5] > m[4] && atLineStart(b, m[0])})
		}
	}
	return out
}

// atLineStart tells whether only up to three spaces precede offset i on its
// line: a definition « [^a]: » starts its line; elsewhere, « [^a]: » is a
// reference followed by a colon.
//
// Container markers may come first, as pandoc reads them: « > [^a]: »,
// « - [^a]: », « 1. [^a]: », and the indentation of a list item.
func atLineStart(b []byte, i int) bool {
	j := i
	for j > 0 && b[j-1] != '\n' {
		j--
	}
	return containerPrefix.Match(b[j:i])
}

var containerPrefix = regexp.MustCompile(`^(?:[ \t]*(?:>|(?:[-*+]|\d{1,9}[.)])[ \t]))*[ \t]*$`)

// Labels returns every label used in src (normalised), references and
// definitions, code included: labels that a new note must not take.
func Labels(src []byte) map[string]bool {
	out := map[string]bool{}
	for _, m := range labelRe.FindAllSubmatch(src, -1) {
		out[Norm(string(m[1]))] = true
	}
	return out
}

// Refs returns the labels referenced in b (not the definitions), and Defs
// the labels defined in b.
func Refs(b []byte) map[string]bool { return kinds(b, false) }
func Defs(b []byte) map[string]bool { return kinds(b, true) }

func kinds(b []byte, def bool) map[string]bool {
	out := map[string]bool{}
	for _, m := range matches(b) {
		if m.def == def {
			out[m.label] = true
		}
	}
	return out
}

// Expand replaces each « ^[text] » of s (outside code) by a reference to a
// new note, numbered with the smallest integers not in taken, and returns the
// definitions to add.
func Expand(s string, taken map[string]bool) (string, []Def) {
	b := []byte(s)
	code := critic.Scan(b).Code
	inCode := func(i int) bool {
		for _, c := range code {
			if i >= c.Start && i < c.End {
				return true
			}
		}
		return false
	}
	var out strings.Builder
	var defs []Def
	n := 1
	pos := 0
	for i := 0; i+1 < len(b); i++ {
		if b[i] != '^' || b[i+1] != '[' || inCode(i) || (i > 0 && b[i-1] == '\\') {
			continue
		}
		// The matching bracket, nested brackets and escapes allowed.
		depth, j := 0, i+1
		for ; j < len(b); j++ {
			switch b[j] {
			case '\\':
				j++
				continue
			case '[':
				depth++
			case ']':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if j >= len(b) {
			break
		}
		text := strings.TrimSpace(string(b[i+2 : j]))
		if text == "" || strings.Contains(text, "\n\n") {
			continue
		}
		for taken[Norm(strconv.Itoa(n))] {
			n++
		}
		label := strconv.Itoa(n)
		taken[label] = true
		out.Write(b[pos:i])
		out.WriteString("[^" + label + "]")
		defs = append(defs, Def{Label: label, Text: text})
		pos = j + 1
		i = j
	}
	out.Write(b[pos:])
	return out.String(), defs
}

// Append returns the text to add at the end of src so that it ends with the
// definitions defs, each in a paragraph of its own (with the line ending of
// src).
func Append(src []byte, defs []Def) []byte {
	nl := []byte("\n")
	if bytes.Contains(src, []byte("\r\n")) {
		nl = []byte("\r\n")
	}
	var b bytes.Buffer
	switch {
	case len(src) == 0:
	case bytes.HasSuffix(src, append(append([]byte{}, nl...), nl...)):
	case bytes.HasSuffix(src, nl):
		b.Write(nl)
	default:
		b.Write(nl)
		b.Write(nl)
	}
	for i, d := range defs {
		if i > 0 {
			b.Write(nl)
		}
		b.WriteString("[^" + d.Label + "]: " + d.Text)
		b.Write(nl)
	}
	return b.Bytes()
}

// Definition returns the range to delete to remove the definition of label
// in src: its lines (with its continuation lines and the indented paragraphs
// of a long note) and the blank line before them. The definition must be
// outside any mark; it may hold code.
func Definition(src []byte, label string) (critic.Span, bool) {
	res := critic.Scan(src)
	label = Norm(label)
	lineEnd := func(i int) int {
		if j := bytes.IndexByte(src[i:], '\n'); j >= 0 {
			return i + j + 1
		}
		return len(src)
	}
	blank := func(s, e int) bool { return len(bytes.TrimSpace(src[s:e])) == 0 }
	// defAt returns the label defined by the line at i, or "": its start,
	// after the markers of its containers (« > », « - », « 1. ») and their
	// indentation, as for atLineStart.
	defAt := func(i int) string {
		line := src[i:lineEnd(i)]
		if m := labelRe.FindSubmatchIndex(line); m != nil && m[5] > m[4] && containerPrefix.Match(line[:m[0]]) {
			return Norm(string(line[m[2]:m[3]]))
		}
		return ""
	}
	inCode := func(i int) bool {
		for _, c := range res.Code {
			if i >= c.Start && i < c.End {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(src); i = lineEnd(i) {
		if inCode(i) || defAt(i) != label {
			continue
		}
		end := lineEnd(i)
		for end < len(src) {
			next := lineEnd(end)
			if !blank(end, next) {
				if defAt(end) != "" || interrupts(src[end:next]) {
					break // the next definition, or a block of its own
				}
				end = next // a continuation line of the paragraph
				continue
			}
			// A blank line: the note goes on only with an indented paragraph.
			after := next
			for after < len(src) && blank(after, lineEnd(after)) {
				after = lineEnd(after)
			}
			if after < len(src) && (src[after] == '\t' || bytes.HasPrefix(src[after:], []byte("    "))) {
				end = lineEnd(after)
				continue
			}
			break
		}
		start := i
		if i > 0 {
			prev := bytes.LastIndexByte(src[:i-1], '\n') + 1
			if blank(prev, i) {
				start = prev
			}
		}
		for _, m := range res.Marks {
			if start < m.Span.End && end > m.Span.Start {
				return critic.Span{}, false
			}
		}
		return critic.Span{Start: start, End: end}, true
	}
	return critic.Span{}, false
}

// interrupts reports whether a line starts a block that ends a paragraph
// (and thus a note): title, fence, blockquote, list item, thematic break,
// table row, fenced div.
func interrupts(line []byte) bool {
	t := bytes.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return false
	}
	t = bytes.TrimRight(t, "\r\n")
	for _, p := range []string{"# ", "## ", "### ", "#### ", "##### ", "###### ", "```", "~~~", ">", "- ", "* ", "+ ", "|", ":::", "---", "***", "___"} {
		if bytes.HasPrefix(t, []byte(p)) {
			return true
		}
	}
	if len(t) > 0 && t[0] == '#' && bytes.Count(t, []byte("#")) == len(t) {
		return true
	}
	k := 0
	for k < len(t) && t[k] >= '0' && t[k] <= '9' {
		k++
	}
	return k > 0 && k+1 < len(t) && (t[k] == '.' || t[k] == ')') && t[k+1] == ' '
}

// Link is the note group of a proposal.
type Link struct {
	Partners []int // starts of the linked proposals
	Primary  bool  // holds a reference: its card stands for the group
	Note     string
}

// Links groups the proposals of src that add, or delete, a reference and its
// definition. It returns, for the start of each linked proposal, its group.
func Links(src []byte) map[int]Link {
	marks, _ := critic.Parse(src)
	type prop struct {
		start                          int
		addRef, addDef, delRef, delDef map[string]bool
		added, removed                 []byte
	}
	var ps []prop
	for _, a := range critic.Group(marks) {
		m := marks[a.Main]
		var added, removed []byte
		switch m.Kind {
		case critic.Addition:
			added = src[m.Text.Start:m.Text.End]
		case critic.Deletion:
			removed = src[m.Text.Start:m.Text.End]
		case critic.Substitution:
			removed, added = src[m.Text.Start:m.Text.End], src[m.New.Start:m.New.End]
		default:
			continue
		}
		ps = append(ps, prop{a.Span.Start, Refs(added), Defs(added), Refs(removed), Defs(removed), added, removed})
	}
	meets := func(a, b map[string]bool) bool {
		for k := range a {
			if b[k] {
				return true
			}
		}
		return false
	}
	parent := make([]int, len(ps))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i := range ps {
		for j := range ps {
			if i != j && (meets(ps[i].addRef, ps[j].addDef) || meets(ps[i].delRef, ps[j].delDef)) {
				parent[find(i)] = find(j)
			}
		}
	}
	groups := map[int][]int{}
	for i := range ps {
		groups[find(i)] = append(groups[find(i)], i)
	}
	out := map[int]Link{}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		sort.Ints(g)
		primary := -1
		var note []string
		for _, i := range g {
			if primary < 0 && (len(ps[i].addRef) > 0 || len(ps[i].delRef) > 0) {
				primary = i
			}
			for _, b := range [][]byte{ps[i].added, ps[i].removed} {
				for _, line := range strings.Split(string(b), "\n") {
					if m := labelRe.FindStringSubmatchIndex(strings.TrimSpace(line)); m != nil && m[0] == 0 && m[4] < m[5] {
						note = append(note, strings.TrimSpace(strings.TrimSpace(line)[m[1]:]))
					}
				}
			}
		}
		for _, i := range g {
			l := Link{Primary: i == primary, Note: strings.Join(note, " ")}
			for _, j := range g {
				if j != i {
					l.Partners = append(l.Partners, ps[j].start)
				}
			}
			out[ps[i].start] = l
		}
	}
	return out
}
