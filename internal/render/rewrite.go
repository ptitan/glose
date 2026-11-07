package render

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/sourcepos"
)

// The provenance contract as seen by a
// project filter: every word or space whose source range is exact is wrapped in
//
//	Span ("", [], [("data-g", "file:start,end")])      certified word
//	Span ("", [], [("data-g", "file:start,end,t")])    transformed word
//
// where file is the index of the file in the project, and start and end are
// byte offsets in that file. A filter may move
// or wrap these spans, but must not change their text nor their attributes.
// Content that a filter adds is generated: it has no data-g and is not
// editable.

// rewriter turns the pandoc tree of one projected file into the tree given to
// the rendering pass, and builds the source map of the file.
type rewriter struct {
	file int
	path string // Markdown path relative to the project
	src  []byte
	view projection.Projection
	x    *sourcepos.Index
	m    sourcemap.Map
	// lines, for a section of a big file (P6.12), gives the line of the
	// view of each line of the input given to pandoc; nil: the same.
	lines []int
}

// mapped returns n with the lines of its ranges brought back to the view
// (a section of a big file).
func (r *rewriter) mapped(n sourcepos.Node) sourcepos.Node {
	if r.lines == nil {
		return n
	}
	rs := make([]sourcepos.Range, len(n.Ranges))
	for k, rg := range n.Ranges {
		for _, p := range []*sourcepos.Pos{&rg.Start, &rg.End} {
			if p.Line >= 1 && p.Line <= len(r.lines) {
				p.Line = r.lines[p.Line-1]
			} else {
				p.Line = 0 // refused by the index
			}
		}
		rs[k] = rg
	}
	n.Ranges = rs
	return n
}

// offset converts a pandoc position of the input into a byte offset of the
// view.
func (r *rewriter) offset(p sourcepos.Pos) (int, bool, error) {
	if r.lines != nil {
		if p.Line < 1 || p.Line > len(r.lines) {
			return 0, false, fmt.Errorf("ligne %d hors de la section", p.Line)
		}
		p.Line = r.lines[p.Line-1]
	}
	return r.x.Offset(p)
}

// absorbedAttr matches an attribute block at the start of a paragraph, such
// as "{#p-label}": pandoc's commonmark_x reader drops it silently. Glose shows
// it again as generated text, as pandoc's markdown reader does, so that no
// byte of the source disappears from the review.
var absorbedAttr = regexp.MustCompile(`^\{[#.][^{}\n]*\}`)

// list processes a list of elements; an element may be replaced by several.
func (r *rewriter) list(l []any, parent int) []any {
	out := make([]any, 0, len(l))
	for _, v := range l {
		el, ok := v.(map[string]any)
		if !ok {
			out = append(out, r.value(v, parent))
			continue
		}
		out = append(out, r.element(el, parent)...)
	}
	return out
}

// value processes anything that is not an element (lists inside tables…).
// A table cell [attr, alignment, rowspan, colspan, blocks] is a block of its
// own: a selection cannot cross from one cell to another.
func (r *rewriter) value(v any, parent int) any {
	l, ok := v.([]any)
	if !ok {
		return v
	}
	if len(l) == 5 {
		if al, ok := l[1].(map[string]any); ok && strings.HasPrefix(fmt.Sprint(al["t"]), "Align") {
			if blocks, ok := l[4].([]any); ok {
				l[4] = r.list(blocks, r.m.Scope(parent))
				return l
			}
		}
	}
	return r.list(l, parent)
}

func (r *rewriter) element(el map[string]any, parent int) []any {
	t, _ := el["t"].(string)
	c, _ := el["c"].([]any)
	switch t {
	case "RawBlock", "RawInline":
		// Raw HTML (or anything else) is shown as text, never interpreted (D-07).
		text := ""
		if len(c) == 2 {
			text, _ = c[1].(string)
		}
		attr := []any{"", []any{"glose-raw"}, []any{}}
		if t == "RawBlock" {
			return []any{map[string]any{"t": "CodeBlock", "c": []any{attr, text}}}
		}
		return []any{map[string]any{"t": "Code", "c": []any{attr, text}}}
	case "Image":
		if len(c) == 3 && external(target(c[2])) {
			// An image from outside would make the browser fetch it (D-07):
			// show its description instead.
			return []any{map[string]any{"t": "Span", "c": []any{
				[]any{"", []any{"glose-image-externe"}, []any{}}, r.list(asList(c[1]), parent)}}}
		}
		if len(c) == 3 {
			if dest, ok := c[2].([]any); ok && len(dest) == 2 {
				dest[0] = resourceURL(r.path, target(c[2]))
			}
		}
	case "Note":
		// The content of a footnote is a block of its own, not part of the
		// paragraph that calls it.
		el["c"] = r.list(c, r.m.Scope(parent))
		return []any{el}
	case "Link":
		if len(c) == 3 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(target(c[2]))), "javascript:") {
			c[2] = []any{"#", ""}
		}
	}
	n, positioned, err := sourcepos.NodeOf(el)
	sanitize(el)
	if err != nil || !positioned {
		if c != nil {
			el["c"] = r.list(c, parent)
		}
		return []any{el}
	}
	switch n.Kind {
	case sourcepos.KindStr, sourcepos.KindSpace:
		return r.word(el, n, parent)
	case sourcepos.KindInline:
		return r.container(el, n, parent)
	default:
		return r.block(el, n, parent)
	}
}

// span converts a pandoc range of the view into a source range.
func (r *rewriter) span(n sourcepos.Node) (s, e int, sameSegment, ok bool) {
	if len(n.Ranges) == 0 {
		return 0, 0, false, false
	}
	vs, exact1, err1 := r.offset(n.Ranges[0].Start)
	ve, exact2, err2 := r.offset(n.Ranges[len(n.Ranges)-1].End)
	if err1 != nil || err2 != nil || !exact1 || !exact2 || ve < vs {
		return 0, 0, false, false
	}
	if vs == ve {
		return 0, 0, false, false
	}
	s, segS, ok1 := r.view.SourceStart(vs)
	e, segE, ok2 := r.view.SourceEnd(ve)
	if !ok1 || !ok2 {
		return 0, 0, false, false
	}
	return s, e, segS.Out == segE.Out, true
}

func (r *rewriter) word(el map[string]any, n sourcepos.Node, parent int) []any {
	c := el["c"].([]any)
	attr := c[0].([]any)
	inner := asList(c[1])
	l := sourcepos.LocateOne(r.x, r.mapped(n))
	s, e, same, ok := r.span(n)
	if !ok || !sourcemap.Accepts(l.Status) || len(n.Ranges) != 1 {
		return r.unwrap(attr, inner)
	}
	if !same && n.Kind == sourcepos.KindStr {
		if pieces := r.split(n, attr, parent); pieces != nil {
			return pieces
		}
	}
	certified := l.Status == sourcepos.Certified && same && string(r.src[s:e]) == n.Text
	if n.Kind == sourcepos.KindSpace {
		certified = l.Status == sourcepos.Certified && same
	}
	kind := sourcemap.Word
	if n.Kind == sourcepos.KindSpace {
		kind = sourcemap.Space
	}
	r.m.Add(sourcemap.Item{Kind: kind, Start: s, End: e, Certified: certified, Parent: parent})
	g := strconv.Itoa(r.file) + ":" + strconv.Itoa(s) + "," + strconv.Itoa(e)
	if !certified {
		g += ",t"
	}
	id, classes, kvs := attrParts(attr)
	return []any{map[string]any{"t": "Span", "c": []any{
		[]any{id, classes, append(withoutPos(kvs), []any{"data-g", g})}, inner}}}
}

// split cuts a word that straddles several segments of the projection
// (« chat » + « s » in « {~~chat~>chien~~}s », view Current; « chien » + « s »,
// view Proposed) into one certified Span per segment, so that the interface
// can tell the words of a proposal from the text around them. Each piece is a
// word of the source map, exact to the byte. It returns nil if the word is not
// rendered exactly as it reads in the view.
func (r *rewriter) split(n sourcepos.Node, attr []any, parent int) []any {
	vs, exact1, err1 := r.offset(n.Ranges[0].Start)
	ve, exact2, err2 := r.offset(n.Ranges[0].End)
	if err1 != nil || err2 != nil || !exact1 || !exact2 || ve <= vs || string(r.view.Text[vs:ve]) != n.Text {
		return nil
	}
	id, classes, kvs := attrParts(attr)
	var out []any
	for _, sg := range r.view.Segments {
		a, b := max(vs, sg.Out.Start), min(ve, sg.Out.End)
		if a >= b {
			continue
		}
		s := sg.Src.Start + a - sg.Out.Start
		r.m.Add(sourcemap.Item{Kind: sourcemap.Word, Start: s, End: s + b - a, Certified: true, Parent: parent})
		g := strconv.Itoa(r.file) + ":" + strconv.Itoa(s) + "," + strconv.Itoa(s+b-a)
		out = append(out, map[string]any{"t": "Span", "c": []any{
			[]any{id, classes, append(withoutPos(kvs), []any{"data-g", g})},
			[]any{map[string]any{"t": "Str", "c": string(r.view.Text[a:b])}}}})
		id = "" // an identifier stays on the first piece
	}
	return out
}

// unwrap keeps the content of a sourcepos Span, and the Span itself only if
// it carries other attributes.
func (r *rewriter) unwrap(attr []any, inner []any) []any {
	id, classes, kvs := attrParts(attr)
	kvs = withoutPos(kvs)
	if id == "" && len(classes) == 0 && len(kvs) == 0 {
		return inner
	}
	return []any{map[string]any{"t": "Span", "c": []any{[]any{id, classes, kvs}, inner}}}
}

func (r *rewriter) container(el map[string]any, n sourcepos.Node, parent int) []any {
	c := el["c"].([]any)
	attr := c[0].([]any)
	me := parent
	if s, e, _, ok := r.span(n); ok && len(n.Ranges) == 1 {
		me = r.m.Add(sourcemap.Item{Kind: sourcemap.Container, Start: s, End: e, Parent: parent})
	}
	if n.Type == "Span" || el["t"] == "Span" {
		return r.unwrap(attr, r.list(asList(c[1]), me))
	}
	// Link, Image, Code: the position is in their own attributes.
	id, classes, kvs := attrParts(attr)
	c[0] = []any{id, classes, withoutPos(kvs)}
	for i := 1; i < len(c); i++ {
		c[i] = r.value(c[i], me)
	}
	return []any{el}
}

func (r *rewriter) block(el map[string]any, n sourcepos.Node, parent int) []any {
	t, _ := el["t"].(string)
	c := el["c"].([]any)
	me := parent
	vs := -1
	if s, e, _, ok := r.span(n); ok {
		me = r.m.Add(sourcemap.Item{Kind: sourcemap.Block, Start: s, End: e, Parent: parent})
		vs, _, _ = r.offset(n.Ranges[0].Start)
	}
	at := 0
	if t == "Header" {
		at = 1
	}
	attr := c[at].([]any)
	id, classes, kvs := attrParts(attr)
	kvs = withoutPos(kvs)
	// A section of a big file: an identifier written by the author is
	// marked, so that the numbering across sections keeps it (htmlChunked
	// removes the mark).
	if t == "Header" && r.lines != nil && id != "" && vs >= 0 && explicitID(r.view.Text[vs:], id) {
		kvs = append(kvs, []any{explicitKey, "1"})
	}
	c[at] = []any{id, classes, kvs}
	if t != "Div" {
		// Other blocks keep their structure; their children are processed.
		for i := range c {
			if i != at {
				c[i] = r.value(c[i], me)
			}
		}
		return []any{el}
	}
	blocks := r.list(asList(c[1]), me)
	if vs >= 0 && len(blocks) == 1 {
		r.restoreAttr(blocks[0], vs)
	}
	if id == "" && len(classes) == 0 && len(withoutPos(kvs)) == 0 {
		return blocks
	}
	c[1] = blocks
	return []any{el}
}

// explicitKey marks, in a section of a big file, a title whose identifier
// is written in the source ({#id}).
const explicitKey = "data-glose-explicit"

// explicitID reports whether the first line of text (a title) writes the
// identifier id in an attribute block.
func explicitID(text []byte, id string) bool {
	if e := bytes.IndexByte(text, '\n'); e >= 0 {
		text = text[:e]
	}
	for k := 0; ; {
		i := bytes.Index(text[k:], []byte("{#"+id))
		if i < 0 {
			return false
		}
		k += i + 2 + len(id)
		if k < len(text) && (text[k] == '}' || text[k] == ' ' || text[k] == '\t') {
			return true
		}
	}
}

// restoreAttr re-inserts, as generated text, an attribute block that pandoc
// dropped at the start of a paragraph (see absorbedAttr).
func (r *rewriter) restoreAttr(b any, viewStart int) {
	el, ok := b.(map[string]any)
	if !ok || (el["t"] != "Para" && el["t"] != "Plain") {
		return
	}
	m := absorbedAttr.Find(r.view.Text[viewStart:])
	if m == nil {
		return
	}
	inl := asList(el["c"])
	el["c"] = append([]any{map[string]any{"t": "Str", "c": string(m)}}, inl...)
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func attrParts(attr []any) (string, []any, []any) {
	id, _ := attr[0].(string)
	classes, _ := attr[1].([]any)
	kvs, _ := attr[2].([]any)
	if classes == nil {
		classes = []any{}
	}
	return id, classes, kvs
}

// withoutPos removes the position attribute of sourcepos, and the attributes
// that a text must not set through the attribute syntax of commonmark_x
// ({onclick=…}): event handlers, addresses, and data-g, which would forge the
// provenance of a word (D-07).
func withoutPos(kvs []any) []any {
	out := []any{}
	for _, kv := range kvs {
		p, ok := kv.([]any)
		if !ok || len(p) != 2 {
			continue
		}
		k, _ := p[0].(string)
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "data-pos" || k == "data-g" || strings.HasPrefix(k, "on") || strings.HasPrefix(k, "data-glose-") || forbiddenAttr[k] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// onlyWithoutPos removes data-pos and keeps everything else, the data-g of
// Glose included.
func onlyWithoutPos(kvs []any) []any {
	out := []any{}
	for _, kv := range kvs {
		if p, ok := kv.([]any); ok && len(p) == 2 && p[0] == "data-pos" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

var forbiddenAttr = map[string]bool{
	"src": true, "srcset": true, "href": true, "xlink:href": true, "action": true, "formaction": true,
	"background": true, "poster": true, "ping": true, "data": true, "codebase": true, "srcdoc": true,
	// Any style would apply to the reviewed text: a passage hidden
	// (display:none) or covering the interface (position:fixed).
	"style": true,
	// The provenance of the interface (the Div of each file).
	"data-f": true, "data-path": true,
}

func target(v any) string {
	l := asList(v)
	if len(l) == 0 {
		return ""
	}
	s, _ := l[0].(string)
	return s
}

// external reports whether an image address would be fetched from outside
// the project: any scheme (http:, data:, file:…), a drive letter, or a
// network path.
func external(u string) bool {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") || strings.HasPrefix(u, `\\`) {
		return true
	}
	i := strings.IndexByte(u, ':')
	return i > 0 && !strings.ContainsAny(u[:i], "/?#")
}

// attrAt gives the index of the attributes in the content of an element.
var attrAt = map[string]int{
	"Span": 0, "Div": 0, "Code": 0, "CodeBlock": 0, "Link": 0, "Image": 0, "Table": 0, "Figure": 0, "Header": 1,
}

// sanitize cleans the attributes that the text gave to an element (see
// withoutPos), before Glose adds its own.
func sanitize(el map[string]any) {
	at, ok := attrAt[fmt.Sprint(el["t"])]
	c, _ := el["c"].([]any)
	if !ok || len(c) <= at {
		return
	}
	if attr, ok := c[at].([]any); ok && len(attr) == 3 {
		id, classes, kvs := attrParts(attr)
		c[at] = []any{id, withoutGloseClasses(classes), withoutPos(kvs)}
	}
}

// withoutGloseClasses removes the classes « glose-… » that a text gives
// itself: they are those of the interface ([mot]{.glose-file} would pass for
// the Div of a file).
func withoutGloseClasses(classes []any) []any {
	out := classes[:0:0]
	for _, c := range classes {
		if s, _ := c.(string); !strings.HasPrefix(strings.ToLower(s), "glose-") {
			out = append(out, c)
		}
	}
	return out
}

// clean removes every data-pos attribute left in the tree (table cells…).
func clean(v any) any {
	switch x := v.(type) {
	case []any:
		if len(x) == 3 {
			if _, ok := x[0].(string); ok {
				if kvs, ok := x[2].([]any); ok && isKVs(kvs) {
					x[2] = onlyWithoutPos(kvs)
				}
			}
		}
		for i := range x {
			x[i] = clean(x[i])
		}
	case map[string]any:
		if c, ok := x["c"]; ok {
			x["c"] = clean(c)
		}
	}
	return v
}

func isKVs(l []any) bool {
	for _, kv := range l {
		p, ok := kv.([]any)
		if !ok || len(p) != 2 {
			return false
		}
		if _, ok := p[0].(string); !ok {
			return false
		}
	}
	return true
}
