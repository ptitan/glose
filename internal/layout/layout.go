// Package layout applies the declarative layout rules of a profile (D-66,
// P4.7): numbered paragraphs, boxes recognised by a bold prefix, and
// references to numbered paragraphs, without a Lua filter. It works on the
// pandoc tree that Glose has rewritten (words wrapped in Span data-g),
// before the HTML pass, and keeps the provenance contract: it never changes
// the text of a word; what it adds (numbers, the text of a reference) is
// generated, carries no provenance, and cannot be annotated.
//
// Glose runs this code itself: unlike a Lua filter, nothing is to approve.
package layout

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Rules is the key « layout » of glose.json. Every part is optional.
type Rules struct {
	// Numbers numbers the paragraphs of the first level, from one file to
	// the next, in the order of the project.
	Numbers *Numbers `json:"numbers,omitempty"`
	// Boxes turns a quotation whose first words are in bold and begin with
	// Prefix into a box of class Class.
	Boxes []Box `json:"boxes,omitempty"`
	// References: an empty link to a paragraph label, [](#label), shows the
	// number of that paragraph.
	References bool `json:"references,omitempty"`
}

// Numbers says which paragraphs are numbered.
type Numbers struct {
	// Within: only under the titles of level 1 that contain this text
	// (« — » for « Première partie — … »); empty: every paragraph.
	Within string `json:"within,omitempty"`
	// SkipItalic: a paragraph entirely in italics (a lead) is not numbered.
	SkipItalic bool `json:"skipItalic,omitempty"`
}

// Box is one kind of box.
type Box struct {
	Prefix string `json:"prefix"`
	Class  string `json:"class"`
}

var classRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Check validates the rules of a profile.
func (r *Rules) Check() error {
	if r.Numbers != nil && (!utf8.ValidString(r.Numbers.Within) || utf8.RuneCountInString(r.Numbers.Within) > 40) {
		return fmt.Errorf("layout.numbers.within : 40 caractères au plus")
	}
	for _, b := range r.Boxes {
		if strings.TrimSpace(b.Prefix) == "" || utf8.RuneCountInString(b.Prefix) > 60 || !utf8.ValidString(b.Prefix) {
			return fmt.Errorf("layout.boxes : préfixe vide ou trop long (60 caractères au plus)")
		}
		if !classRe.MatchString(b.Class) {
			return fmt.Errorf("layout.boxes %q : classe %q invalide (minuscules, chiffres, « - », 32 au plus)", b.Prefix, b.Class)
		}
	}
	return nil
}

// Global tells whether the rules need the whole project at once (numbers
// and references go from one file to the next): the HTML is then made in
// one pass, as with a filter that is not « filterPerFile ».
func (r *Rules) Global() bool { return r != nil && (r.Numbers != nil || r.References) }

// Apply applies the rules to the file Divs of the project (one raw pandoc
// block each, in order) and returns them changed.
func Apply(r *Rules, parts [][]byte) ([][]byte, error) {
	if r == nil {
		return parts, nil
	}
	blocks := make([]any, len(parts))
	for i, p := range parts {
		if err := json.Unmarshal(p, &blocks[i]); err != nil {
			return nil, err
		}
	}
	labels := map[string]int{}
	if r.Numbers != nil {
		n := 0
		for _, b := range blocks {
			n = number(r.Numbers, divBlocks(b), n, labels)
		}
	}
	if len(r.Boxes) > 0 {
		for i := range blocks {
			blocks[i] = walkBlocks(blocks[i], func(b map[string]any) any { return box(r.Boxes, b) })
		}
	}
	if r.References {
		for i := range blocks {
			walkInlines(blocks[i], func(l map[string]any) { reference(l, labels) })
		}
	}
	out := make([][]byte, len(blocks))
	for i, b := range blocks {
		raw, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		out[i] = raw
	}
	return out, nil
}

// divBlocks returns the blocks of a file Div (or the block itself).
func divBlocks(b any) []any {
	m, ok := b.(map[string]any)
	if ok && m["t"] == "Div" {
		if c, ok := m["c"].([]any); ok && len(c) == 2 {
			if bl, ok := c[1].([]any); ok {
				return bl
			}
		}
	}
	return []any{b}
}

var labelRe = regexp.MustCompile(`^\{#([A-Za-z0-9-]+)\}$`)

// number numbers the paragraphs of blocks, from n, and returns the last
// number given. A label « {#p-…} » at the start of a paragraph is removed
// from the display and names the number (its anchor).
func number(rules *Numbers, blocks []any, n int, labels map[string]int) int {
	active := rules.Within == ""
	for _, b := range blocks {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		switch m["t"] {
		case "Header":
			c, _ := m["c"].([]any)
			if len(c) == 3 && toInt(c[0]) == 1 {
				active = rules.Within == "" || strings.Contains(text(c[2]), rules.Within)
			}
		case "Para":
			c, _ := m["c"].([]any)
			if !active || (rules.SkipItalic && len(c) == 1 && kind(c[0]) == "Emph") {
				continue
			}
			n++
			label := ""
			if len(c) > 0 {
				if w := word(c[0]); w != "" {
					if mm := labelRe.FindStringSubmatch(w); mm != nil {
						label = mm[1]
						labels[label] = n
						c = c[1:]
					}
				}
			}
			num := strconv.Itoa(n)
			span := map[string]any{"t": "Span", "c": []any{
				[]any{label, []any{"numpar"}, []any{[]any{"data-n", num}}},
				[]any{map[string]any{"t": "Str", "c": num}},
			}}
			m["c"] = append([]any{span}, c...)
		}
	}
	return n
}

// box turns a quotation that begins with a bold prefix into a box.
func box(boxes []Box, b map[string]any) any {
	if b["t"] != "BlockQuote" {
		return b
	}
	inner, _ := b["c"].([]any)
	if len(inner) == 0 {
		return b
	}
	first, _ := inner[0].(map[string]any)
	if first == nil || (first["t"] != "Para" && first["t"] != "Plain") {
		return b
	}
	fc, _ := first["c"].([]any)
	if len(fc) == 0 || kind(fc[0]) != "Strong" {
		return b
	}
	key := text(fc[0].(map[string]any)["c"])
	for _, x := range boxes {
		if strings.HasPrefix(key, x.Prefix) {
			return map[string]any{"t": "Div", "c": []any{
				[]any{"", []any{"encadre", x.Class}, []any{}},
				inner,
			}}
		}
	}
	return b
}

// reference fills an empty link to a label with the number of its paragraph.
func reference(l map[string]any, labels map[string]int) {
	c, _ := l["c"].([]any)
	if len(c) != 3 {
		return
	}
	content, _ := c[1].([]any)
	target, _ := c[2].([]any)
	if len(content) != 0 || len(target) != 2 {
		return
	}
	url, _ := target[0].(string)
	if !strings.HasPrefix(url, "#") {
		return
	}
	attr, _ := c[0].([]any)
	if len(attr) != 3 {
		return
	}
	classes, _ := attr[1].([]any)
	n, ok := labels[url[1:]]
	shown := "?"
	if ok {
		shown = strconv.Itoa(n)
	} else {
		classes = append(classes, "renvoi-inconnu")
	}
	attr[1] = append(classes, "renvoi")
	c[1] = []any{map[string]any{"t": "Str", "c": shown}}
}

// walkBlocks replaces, depth first, every block for which f returns
// another value.
func walkBlocks(v any, f func(map[string]any) any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = walkBlocks(x[i], f)
		}
		return x
	case map[string]any:
		if c, ok := x["c"]; ok {
			x["c"] = walkBlocks(c, f)
		}
		if _, ok := x["t"].(string); ok {
			return f(x)
		}
		return x
	}
	return v
}

// walkInlines calls f on every Link.
func walkInlines(v any, f func(map[string]any)) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			walkInlines(e, f)
		}
	case map[string]any:
		if x["t"] == "Link" {
			f(x)
		}
		if c, ok := x["c"]; ok {
			walkInlines(c, f)
		}
	}
}

func kind(v any) string {
	m, _ := v.(map[string]any)
	s, _ := m["t"].(string)
	return s
}

// word returns the text of a word: a Str, or a Span of Glose around one Str.
func word(v any) string {
	m, _ := v.(map[string]any)
	switch m["t"] {
	case "Str":
		s, _ := m["c"].(string)
		return s
	case "Span":
		c, _ := m["c"].([]any)
		if len(c) == 2 {
			if in, ok := c[1].([]any); ok && len(in) == 1 {
				return word(in[0])
			}
		}
	}
	return ""
}

// text returns the text of inlines (Str, spaces, and what they contain).
func text(v any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			switch x["t"] {
			case "Str":
				s, _ := x["c"].(string)
				b.WriteString(s)
			case "Space", "SoftBreak", "LineBreak":
				b.WriteByte(' ')
			case "Span", "Link":
				if c, ok := x["c"].([]any); ok && len(c) >= 2 {
					walk(c[1])
				}
			default:
				walk(x["c"])
			}
		}
	}
	walk(v)
	return b.String()
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
