// Package sourcepos converts the source positions produced by pandoc
// (`-f commonmark_x+sourcepos`) into byte offsets in the original text, and
// certifies which rendered fragments can be traced back exactly to the source.
//
// It relies on observations of pandoc 3.1.3 (commonmark_x+sourcepos).
package sourcepos

import (
	"fmt"
	"strconv"
	"strings"
)

// Pos is a pandoc source position: 1-based line and 1-based column.
// Columns count Unicode code points after pandoc's own preprocessing
// (tab stops of 4, byte order mark ignored, NFC composition).
type Pos struct {
	Line, Col int
}

// Range is a half-open pandoc range [Start, End).
type Range struct {
	Start, End Pos
}

// ParseDataPos parses the value of a pandoc `data-pos` attribute, such as
// "1:3-1:5", "file.md@1:3-1:5" or "1:3-2:1;2:3-3:1" (several segments, used
// for block quotes and list items). An empty value returns no range.
func ParseDataPos(s string) ([]Range, error) {
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return nil, nil
	}
	var out []Range
	for _, seg := range strings.Split(s, ";") {
		start, end, ok := strings.Cut(seg, "-")
		if !ok {
			return nil, fmt.Errorf("data-pos %q : tiret manquant", seg)
		}
		a, err := parsePos(start)
		if err != nil {
			return nil, err
		}
		b, err := parsePos(end)
		if err != nil {
			return nil, err
		}
		if b.Line < a.Line || (b.Line == a.Line && b.Col < a.Col) {
			return nil, fmt.Errorf("data-pos %q : fin avant le début", seg)
		}
		out = append(out, Range{a, b})
	}
	return out, nil
}

func parsePos(s string) (Pos, error) {
	l, c, ok := strings.Cut(s, ":")
	if !ok {
		return Pos{}, fmt.Errorf("position %q : deux-points manquant", s)
	}
	line, err1 := strconv.Atoi(l)
	col, err2 := strconv.Atoi(c)
	if err1 != nil || err2 != nil || line < 1 || col < 1 {
		return Pos{}, fmt.Errorf("position %q invalide", s)
	}
	return Pos{line, col}, nil
}
