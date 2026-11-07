package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/sourcepos"
	"github.com/ptitan/glose/internal/store"
)

// Rendering by section inside a very big file (P6.12). A file whose view is
// bigger than chunkMin, in a project without filter, is cut into sections
// before ATX headings; each section goes through pandoc on its own, with a
// copy of the footnote and link definitions of the whole file, and a table
// that brings every line of that input back to its line in the file. After a
// change, only the sections whose input changed go through pandoc again; the
// others are only moved by the Go rewrite (their byte offsets change), and
// their HTML is kept: it is made with offsets relative to the section and
// moved afterwards. Footnotes are numbered across the whole file and shown
// at the end of their section.

// chunkMin is the size of view from which a file is cut; chunkTarget the
// size a section reaches before the next cut.
var (
	chunkMin    = 512 << 10
	chunkTarget = 96 << 10
)

// chunk is a section: a line-aligned byte range of the view, and the number
// of its first line (from 1).
type chunk struct {
	start, end int
	line       int
}

// lineRange is a block of whole lines of the view: bytes and lines (from 1,
// last included).
type lineRange struct {
	start, end  int
	first, last int
}

var (
	atxHeading  = regexp.MustCompile(`^#{1,6}([ \t]|$)`)
	fenceOpen   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	divFence    = regexp.MustCompile(`^ {0,3}(:{3,})(.*)$`)
	noteDef     = regexp.MustCompile(`^ {0,3}\[\^[^\]\n]+\]:`)
	linkDef     = regexp.MustCompile(`^ {0,3}\[[^\]^\n][^\]\n]*\]:[ \t]*\S`)
	linkDefOpen = regexp.MustCompile(`^ {0,3}\[[^\]^\n][^\]\n]*\]:[ \t]*$`)
	indented    = regexp.MustCompile(`^(    |\t)`)
)

// lines returns the lines of text as byte ranges, line breaks included.
func lines(text []byte) [][2]int {
	var out [][2]int
	for start := 0; start < len(text); {
		end := bytes.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start + 1
		}
		out = append(out, [2]int{start, end})
		start = end
	}
	return out
}

// splitChunks cuts text into sections, or returns nil if it is small. A cut
// is made only before an ATX heading at the start of a line, after a blank
// line, outside fenced code and fenced divs.
func splitChunks(text []byte) []chunk {
	if len(text) < chunkMin {
		return nil
	}
	ls := lines(text)
	var out []chunk
	cur := chunk{start: 0, line: 1}
	var fence []byte
	divs := 0
	blank := true
	for i, l := range ls {
		line := bytes.TrimRight(text[l[0]:l[1]], "\r\n")
		// The cuts depend on the headings themselves, within size bounds:
		// a change that makes a section longer does not move the cuts after
		// it (they would all go through pandoc again).
		since := l[0] - cur.start
		if fence == nil && divs == 0 && blank && i > 0 && atxHeading.Match(line) && since >= chunkTarget/2 &&
			(headingPicked(line) || since >= chunkTarget*2) {
			cur.end = l[0]
			out = append(out, cur)
			cur = chunk{start: l[0], line: i + 1}
		}
		switch {
		case fence != nil:
			if closesFence(line, fence) {
				fence = nil
			}
		case fenceOpen.Match(line):
			m := fenceOpen.FindSubmatch(line)
			fence = m[1]
		case divFence.Match(line):
			if m := divFence.FindSubmatch(line); len(bytes.TrimSpace(m[2])) > 0 {
				divs++
			} else if divs > 0 {
				divs--
			}
		}
		blank = len(bytes.TrimSpace(line)) == 0
	}
	cur.end = len(text)
	out = append(out, cur)
	if len(out) < 2 {
		return nil
	}
	return out
}

// closesFence reports whether line closes the code block opened by fence:
// up to three spaces, at least as many fence characters, then only blanks.
func closesFence(line, fence []byte) bool {
	t := bytes.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || !bytes.HasPrefix(t, fence) {
		return false
	}
	return len(bytes.TrimRight(bytes.TrimLeft(t, string(fence[:1])), " \t")) == 0
}

// headingPicked chooses about one heading in three, by its text.
func headingPicked(line []byte) bool {
	h := fnv.New32a()
	h.Write(line)
	return h.Sum32()%3 == 0
}

// definitions returns the footnote and link reference definitions of text,
// outside fenced code: a footnote definition goes on while its lines are
// indented (blank lines included). It returns false when a definition is of
// a form it does not read (the file is then rendered whole).
func definitions(text []byte) ([]lineRange, bool) {
	ls := lines(text)
	var out []lineRange
	var fence []byte
	for i := 0; i < len(ls); i++ {
		line := bytes.TrimRight(text[ls[i][0]:ls[i][1]], "\r\n")
		if fence != nil {
			if closesFence(line, fence) {
				fence = nil
			}
			continue
		}
		if m := fenceOpen.FindSubmatch(line); m != nil {
			fence = m[1]
			continue
		}
		switch {
		case noteDef.Match(line):
			j := i
			for k := i + 1; k < len(ls); k++ {
				next := bytes.TrimRight(text[ls[k][0]:ls[k][1]], "\r\n")
				if indented.Match(next) {
					j = k
					continue
				}
				if len(bytes.TrimSpace(next)) == 0 {
					continue
				}
				break
			}
			out = append(out, lineRange{ls[i][0], ls[j][1], i + 1, j + 1})
			i = j
		case linkDef.Match(line):
			j := i
			title := linkTitle(line)
			// A title on the next line belongs to the definition.
			if len(title) == 0 && i+1 < len(ls) {
				next := bytes.TrimSpace(text[ls[i+1][0]:ls[i+1][1]])
				if len(next) > 0 && (next[0] == '"' || next[0] == '\'' || next[0] == '(') {
					j, title = i+1, next
				}
			}
			// A title that goes on over several lines: not read here, the
			// file is rendered whole rather than with a truncated copy.
			if unclosedTitle(title) {
				return nil, false
			}
			out = append(out, lineRange{ls[i][0], ls[j][1], i + 1, j + 1})
			i = j
		case linkDefOpen.Match(line):
			// The URL on the next line: not read here; the file is rendered
			// whole rather than with a definition cut in two.
			return nil, false
		}
	}
	return out, true
}

// linkTitle returns what follows the address on the line of a link reference
// definition: its title, if any, trimmed.
func linkTitle(line []byte) []byte {
	rest := bytes.TrimSpace(line[bytes.Index(line, []byte("]:"))+2:])
	if len(rest) > 0 && rest[0] == '<' {
		if e := bytes.IndexByte(rest, '>'); e >= 0 {
			return bytes.TrimSpace(rest[e+1:])
		}
		return nil
	}
	if e := bytes.IndexAny(rest, " \t"); e >= 0 {
		return bytes.TrimSpace(rest[e:])
	}
	return nil
}

// unclosedTitle reports whether a link title opened on its line does not
// close on it.
func unclosedTitle(t []byte) bool {
	if len(t) == 0 {
		return false
	}
	end := map[byte]byte{'"': '"', '\'': '\'', '(': ')'}[t[0]]
	if end == 0 {
		return false
	}
	if len(t) < 2 || t[len(t)-1] != end {
		return true
	}
	// « \" » at the end does not close the title: count the backslashes.
	n := 0
	for k := len(t) - 2; k > 0 && t[k] == '\\'; k-- {
		n++
	}
	return n%2 == 1
}

// chunkInput builds the input of a section: its lines, a blank line, then
// each definition outside it followed by a blank line. lineMap gives, for
// each line of the input, its line in the view (a blank line stands for the
// line after the block before it).
func chunkInput(text []byte, c chunk, defs []lineRange) (input []byte, lineMap []int) {
	var b bytes.Buffer
	body := text[c.start:c.end]
	b.Write(body)
	n := bytes.Count(body, []byte{'\n'})
	if len(body) > 0 && body[len(body)-1] != '\n' {
		b.WriteByte('\n')
		n++
	}
	for k := 0; k < n; k++ {
		lineMap = append(lineMap, c.line+k)
	}
	b.WriteByte('\n')
	lineMap = append(lineMap, c.line+n)
	for _, d := range defs {
		if d.start >= c.start && d.end <= c.end {
			continue
		}
		blk := text[d.start:d.end]
		b.Write(blk)
		if blk[len(blk)-1] != '\n' {
			b.WriteByte('\n')
		}
		for l := d.first; l <= d.last; l++ {
			lineMap = append(lineMap, l)
		}
		b.WriteByte('\n')
		lineMap = append(lineMap, d.last+1)
	}
	return b.Bytes(), lineMap
}

// chunkPart is the result of one section: its blocks with byte offsets
// relative to base, the smallest source offset of its words.
type chunkPart struct {
	rel  []byte
	base int
}

// shiftOffsets adds delta to the two offsets that follow every prefix in b
// (« "data-g","3:120,125 » in JSON, « data-g="3:120,125 » in HTML), and
// returns the smallest start offset met. A plain scan: regular expressions
// cost seconds on a file of several megabytes.
func shiftOffsets(b []byte, prefix string, delta int) ([]byte, int) {
	out := make([]byte, 0, len(b)+len(b)/50)
	low := -1
	for {
		i := bytes.Index(b, []byte(prefix))
		if i < 0 {
			return append(out, b...), low
		}
		j := i + len(prefix)
		// file index, then ':'
		k := j
		for k < len(b) && b[k] >= '0' && b[k] <= '9' {
			k++
		}
		if k == j || k >= len(b) || b[k] != ':' {
			out = append(out, b[:j]...)
			b = b[j:]
			continue
		}
		s0 := k + 1
		s1 := s0
		for s1 < len(b) && b[s1] >= '0' && b[s1] <= '9' {
			s1++
		}
		if s1 == s0 || s1 >= len(b) || b[s1] != ',' {
			out = append(out, b[:j]...)
			b = b[j:]
			continue
		}
		e0 := s1 + 1
		e1 := e0
		for e1 < len(b) && b[e1] >= '0' && b[e1] <= '9' {
			e1++
		}
		if e1 == e0 {
			out = append(out, b[:j]...)
			b = b[j:]
			continue
		}
		st, _ := strconv.Atoi(string(b[s0:s1]))
		en, _ := strconv.Atoi(string(b[e0:e1]))
		if low < 0 || st < low {
			low = st
		}
		out = append(out, b[:s0]...)
		out = strconv.AppendInt(out, int64(st+delta), 10)
		out = append(out, ',')
		out = strconv.AppendInt(out, int64(en+delta), 10)
		b = b[e1:]
	}
}

const jsonDataG = `"data-g","`

// relativize moves the data-g offsets of blocks so that they start at 0.
func relativize(blocks []byte) ([]byte, int) {
	_, base := shiftOffsets(blocks, jsonDataG, 0)
	if base <= 0 {
		return blocks, 0
	}
	rel, _ := shiftOffsets(blocks, jsonDataG, -base)
	return rel, base
}

// moveHTML adds base to the data-g offsets of a section's HTML.
func moveHTML(h []byte, base int) []byte {
	if base == 0 {
		return h
	}
	return inTags(h, `data-g="`, func(tag []byte) []byte {
		out, _ := shiftOffsets(tag, `data-g="`, base)
		return out
	})
}

var (
	headerFull = regexp.MustCompile(`(?s)<h([1-6])( data-glose-explicit="1")? id="([^"]+)"([^>]*)>(.*?)</h[1-6]>`)
	tags       = regexp.MustCompile(`(?s)<a [^>]*class="footnote-ref"[^>]*>.*?</a>|<[^>]+>`)
)

// headerText is the text of a title, as pandoc reads it to make its
// identifier: without its notes and markup.
func headerText(inner []byte) string {
	return html.UnescapeString(string(tags.ReplaceAll(inner, nil)))
}

// autoID is the identifier that pandoc makes from the text of a title
// (gfm_auto_identifiers): lower case, letters, digits, « - » and « _ »,
// spaces changed into « - »; « section » when nothing is left.
func autoID(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "section"
	}
	return b.String()
}

var (
	noteRef    = regexp.MustCompile(`role="doc-noteref"><sup>(\d+)</sup>`)
	noteIDs    = regexp.MustCompile(`(id|href)="(#?)(?:gl-)?(fnref|fn)(\d+)"`)
	notesAside = regexp.MustCompile(`<aside id="(?:gl-)?footnotes"([^>]*>\s*<hr />\s*)<ol>`)
)

// numberNotes numbers the footnotes of section k of file i after the off
// notes of the sections before it; it returns the HTML and its number of
// notes.
func numberNotes(h []byte, i, k, off int) ([]byte, int) {
	n := len(noteRef.FindAll(h, -1))
	if n == 0 {
		return h, 0
	}
	h = noteRef.ReplaceAllFunc(h, func(m []byte) []byte {
		v, _ := strconv.Atoi(string(noteRef.FindSubmatch(m)[1]))
		return []byte(fmt.Sprintf(`role="doc-noteref"><sup>%d</sup>`, v+off))
	})
	h = inTags(h, "fn", func(tag []byte) []byte {
		return noteIDs.ReplaceAllFunc(tag, func(m []byte) []byte {
			p := noteIDs.FindSubmatch(m)
			v, _ := strconv.Atoi(string(p[4]))
			return []byte(fmt.Sprintf(`%s="%sgl-f%d-%s%d"`, p[1], p[2], i, p[3], v+off))
		})
	})
	start := ""
	if off > 0 {
		start = fmt.Sprintf(` start="%d"`, off+1)
	}
	h = notesAside.ReplaceAll(h, []byte(fmt.Sprintf(`<aside id="gl-f%d-c%d-footnotes"${1}<ol%s>`, i, k, start)))
	return h, n
}

// stage1Chunked is the first stage of a file cut into sections. The pandoc
// tree of each section is kept by its input, so that an unchanged section
// is not read again; the rewrite runs on every section (offsets move).
func (r *Renderer) stage1Chunked(ctx context.Context, file int, src []byte, view projection.Projection, rw *rewriter, chunks []chunk) *fileResult {
	defs, _ := definitions(view.Text)
	inputs := make([][]byte, len(chunks))
	maps := make([][]int, len(chunks))
	trees := make([][]byte, len(chunks))
	errs := make([]error, len(chunks))
	var wg sync.WaitGroup
	keys := make([]string, len(chunks))
	for k, c := range chunks {
		inputs[k], maps[k] = chunkInput(view.Text, c, defs)
		keys[k] = store.Hash(inputs[k])
		r.mu.Lock()
		t, ok := r.trees[keys[k]]
		r.mu.Unlock()
		if ok {
			trees[k] = t
			continue
		}
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			trees[k], errs[k] = r.pandoc(ctx, inputs[k], "-f", sourcepos.Reader, "-t", "json")
		}(k)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return &fileResult{err: err}
		}
	}
	keep := make(map[string][]byte, len(chunks))
	for k := range chunks {
		keep[keys[k]] = trees[k]
	}
	// Only the trees of the current sections are kept for this file.
	r.mu.Lock()
	for _, key := range r.treesOf[file] {
		delete(r.trees, key)
	}
	r.treesOf[file] = r.treesOf[file][:0]
	for key, t := range keep {
		r.trees[key] = t
		r.treesOf[file] = append(r.treesOf[file], key)
	}
	r.mu.Unlock()

	// The sections are rewritten in parallel, each with its own index and
	// map; the maps are then put end to end.
	type done struct {
		api  any
		raw  []byte
		part chunkPart
		m    *sourcemap.Map
		err  error
	}
	res := make([]done, len(chunks))
	var wg2 sync.WaitGroup
	sem := make(chan struct{}, max(2, runtime.NumCPU()))
	for k := range chunks {
		wg2.Add(1)
		go func(k int) {
			defer wg2.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dec := json.NewDecoder(bytes.NewReader(trees[k]))
			dec.UseNumber()
			var tree map[string]any
			if err := dec.Decode(&tree); err != nil {
				res[k].err = fmt.Errorf("arbre pandoc illisible : %w", err)
				return
			}
			x, err := sourcepos.NewIndex(view.Text)
			if err != nil {
				res[k].err = err
				return
			}
			crw := &rewriter{file: file, path: r.P.Files[file], src: src, view: view, x: x, lines: maps[k]}
			blocks := clean(crw.list(asList(tree["blocks"]), -1))
			crw.m.Finish()
			raw, err := json.Marshal(blocks)
			if err != nil {
				res[k].err = err
				return
			}
			rel, base := relativize(raw)
			res[k] = done{api: tree["pandoc-api-version"], raw: raw, part: chunkPart{rel: rel, base: base}, m: &crw.m}
		}(k)
	}
	wg2.Wait()
	var api any
	var all [][]byte
	parts := make([]chunkPart, 0, len(chunks))
	for _, d := range res {
		if d.err != nil {
			return &fileResult{err: d.err}
		}
		if api == nil {
			api = d.api
		}
		rw.m.Append(d.m)
		parts = append(parts, d.part)
		if len(d.raw) > 2 {
			all = append(all, d.raw[1:len(d.raw)-1])
		}
	}
	merged := append(append([]byte{'['}, bytes.Join(all, []byte{','})...), ']')
	return &fileResult{api: api, blocks: merged, m: &rw.m, parts: parts}
}

// htmlChunked makes the HTML of a file cut into sections: each section is
// one pandoc pass on its relative blocks, kept by them; the pieces are then
// moved to their offsets, their notes numbered across the file, and put
// together in the Div of the file.
func (r *Renderer) htmlChunked(ctx context.Context, api any, v projection.View, i int, parts []chunkPart) ([]byte, error) {
	out := make([][]byte, len(parts))
	errs := make([]error, len(parts))
	var wg sync.WaitGroup
	for k, p := range parts {
		key := "chunk\x00" + strconv.Itoa(int(v)) + "\x00" + store.Hash(p.rel)
		r.mu.Lock()
		h, ok := r.html[key]
		r.mu.Unlock()
		if !ok && r.disk != nil {
			if h = r.disk.loadHTML(r.disk.key("chunk-html", key)); h != nil {
				ok = true
			}
		}
		if ok {
			out[k] = h
			continue
		}
		wg.Add(1)
		go func(k int, key string, rel []byte) {
			defer wg.Done()
			tree, err := treeRaw(api, [][]byte{rel[1 : len(rel)-1]})
			if err == nil {
				out[k], err = r.pandoc(ctx, tree, r.htmlArgs()...)
			}
			if err != nil {
				errs[k] = err
				return
			}
			r.mu.Lock()
			r.html[key] = out[k]
			r.mu.Unlock()
			if r.disk != nil {
				r.disk.saveHTML(r.disk.key("chunk-html", key), out[k])
			}
		}(k, key, p.rel)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	// Only the current sections of this file stay in memory.
	r.mu.Lock()
	keys := make(map[string]bool, len(parts))
	for _, p := range parts {
		keys["chunk\x00"+strconv.Itoa(int(v))+"\x00"+store.Hash(p.rel)] = true
	}
	for _, key := range r.chunkKeys[i*4+int(v)] {
		if !keys[key] {
			delete(r.html, key)
		}
	}
	r.chunkKeys[i*4+int(v)] = r.chunkKeys[i*4+int(v)][:0]
	for key := range keys {
		r.chunkKeys[i*4+int(v)] = append(r.chunkKeys[i*4+int(v)], key)
	}
	r.mu.Unlock()
	var b bytes.Buffer
	fmt.Fprintf(&b, `<div id="gl-glose-f%d" class="glose-file" data-f="%d" data-path="%s">`+"\n", i, i, html.EscapeString(r.P.Files[i]))
	// Notes numbered across the file: counted first, then each section is
	// moved and numbered in parallel.
	offs := make([]int, len(out))
	for k, h := range out {
		if k > 0 {
			offs[k] = offs[k-1] + bytes.Count(out[k-1], []byte(`role="doc-noteref"`))
		}
		_ = h
	}
	final := make([][]byte, len(out))
	var wg3 sync.WaitGroup
	for k := range out {
		wg3.Add(1)
		go func(k int) {
			defer wg3.Done()
			h := moveHTML(out[k], parts[k].base)
			final[k], _ = numberNotes(h, i, k, offs[k])
		}(k)
	}
	wg3.Wait()
	// Identifiers of titles made unique across the sections, as pandoc does
	// on a whole document: an automatic identifier (made from the text of
	// the title) gets « -1 », « -2 »… when it is taken, by any title before
	// it or by an identifier written by the author anywhere in the file; an
	// identifier written by the author ({#x}, marked by the rewriter) is kept.
	// pandoc writes the mark of the rewriter before the identifier.
	seen := map[string]bool{}
	for _, h := range final {
		for _, m := range headerFull.FindAllSubmatch(h, -1) {
			if len(m[2]) > 0 {
				seen[string(m[3])] = true
			}
		}
	}
	for _, h := range final {
		h = headerFull.ReplaceAllFunc(h, func(m []byte) []byte {
			p := headerFull.FindSubmatch(m)
			id := string(p[3])
			if len(p[2]) == 0 {
				base := "gl-" + autoID(headerText(p[5]))
				id = base
				if seen[id] {
					n := 1
					for seen[fmt.Sprintf("%s-%d", base, n)] {
						n++
					}
					id = fmt.Sprintf("%s-%d", base, n)
				}
			}
			seen[id] = true
			return []byte(fmt.Sprintf(`<h%s id="%s"%s>%s</h%s>`, p[1], id, p[4], p[5], p[1]))
		})
		b.Write(bytes.TrimRight(h, "\n"))
		b.WriteByte('\n')
	}
	b.WriteString("</div>\n")
	return b.Bytes(), nil
}
