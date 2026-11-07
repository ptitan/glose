// Package search searches projected Markdown without changing its source.
package search

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/projection"
)

type Match struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Before string `json:"before"`
	Text   string `json:"text"`
	After  string `json:"after"`
}

// fold keeps a map from each folded byte boundary to the original boundary:
// Unicode lowercasing may change the UTF-8 width (for example U+023A).
func fold(b []byte, sensitive bool) (string, []int) {
	var s strings.Builder
	positions := []int{0}
	for off := 0; off < len(b); {
		r, n := utf8.DecodeRune(b[off:])
		if !sensitive {
			r = unicode.ToLower(r)
		}
		s.WriteRune(r)
		for k := 1; k < utf8.RuneLen(r); k++ {
			positions = append(positions, off)
		}
		off += n
		positions = append(positions, off)
	}
	return s.String(), positions
}

// Find returns non-overlapping literal matches and their exact source byte
// ranges. Context is bounded in characters and contains no CriticMarkup metadata.
func Find(src []byte, query string, view projection.View, sensitive bool, limit int) ([]Match, int) {
	if query == "" || !utf8.Valid(src) {
		return nil, 0
	}
	p := projection.Project(src, view)
	text, pos := fold(p.Text, sensitive)
	needle, _ := fold([]byte(query), sensitive)
	var matches []Match
	total := 0
	for at := 0; at <= len(text); {
		j := strings.Index(text[at:], needle)
		if j < 0 {
			break
		}
		start, end := pos[at+j], pos[at+j+len(needle)]
		at += j + len(needle)
		total++
		if len(matches) >= limit {
			continue
		}
		s, _, ok1 := p.SourceStart(start)
		e, _, ok2 := p.SourceEnd(end)
		if !ok1 || !ok2 {
			continue
		}
		left, right := start, end
		for n := 0; n < 65 && left > 0; n++ {
			_, width := utf8.DecodeLastRune(p.Text[:left])
			left -= width
		}
		for n := 0; n < 65 && right < len(p.Text); n++ {
			_, width := utf8.DecodeRune(p.Text[right:])
			right += width
		}
		matches = append(matches, Match{Start: s, End: e, Before: string(p.Text[left:start]), Text: string(p.Text[start:end]), After: string(p.Text[end:right])})
	}
	return matches, total
}
