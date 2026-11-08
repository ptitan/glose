// Package render turns the files of a project into the HTML shown to the
// author: each file is projected (view Current: proposals rejected, marks
// hidden), read by pandoc with source positions, rewritten so that every word
// carries its byte range in the original file (rewrite.go), then all files are
// rendered together by pandoc, with the project filter if there is one.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/notes"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/sourcepos"
	"github.com/ptitan/glose/internal/store"
)

// File describes one file of the rendered document.
type File struct {
	Index int    `json:"index"`
	Path  string `json:"path"`
	Hash  string `json:"hash"`
	Size  int    `json:"size"`  // in bytes
	Shown bool   `json:"shown"` // rendered in the HTML (RenderPart)
	Error string `json:"error,omitempty"`
}

// Annotation is an annotation of a file, for the margin of the interface and
// for « glose list ».
type Annotation struct {
	File      int       `json:"file"`
	ID        string    `json:"id"`    // empty until the identity is written (D-19)
	By        string    `json:"by"`    // author, agent, unknown, or empty
	Start     int       `json:"start"` // byte range of the whole annotation (mark and thread)
	End       int       `json:"end"`
	Kind      string    `json:"kind"` // comment, highlight, addition, deletion, substitution
	TextStart int       `json:"textStart"`
	TextEnd   int       `json:"textEnd"`
	NewStart  int       `json:"newStart,omitempty"` // byte range of the new text of a substitution
	NewEnd    int       `json:"newEnd,omitempty"`
	Text      string    `json:"text"`          // content; old text of a substitution
	New       string    `json:"new,omitempty"` // new text of a substitution
	Comments  []string  `json:"comments"`      // the thread, technical comments excluded
	Messages  []Message `json:"messages"`      // the thread with its identities
	Line      int       `json:"line"`
	Column    int       `json:"column"`
	// Footnotes (package notes): a reference and its definition, added or
	// deleted, are one proposal. Partners gives the starts of the linked
	// proposals; Primary the one whose card stands for the group; Note the
	// text of the note.
	Partners []int  `json:"partners,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
	Note     string `json:"note,omitempty"`
	Status   string `json:"status,omitempty"` // new or open (package navette)
	Turn     int    `json:"turn,omitempty"`   // turn of birth (package navette)
	// To is the short name of the agent that must answer (review.Recipient:
	// to= of the annotation or of the last message of the author); "" for
	// the default agent. Via: the agent that wrote a proposal (D-49).
	To    string `json:"to,omitempty"`
	Via   string `json:"via,omitempty"`
	Model string `json:"model,omitempty"` // model= : the model of that agent (P10.11)
}

// Message is one message of a thread. For a comment, the first message is
// the comment itself; for a highlighted passage, its remark.
type Message struct {
	ID    string `json:"id"`
	By    string `json:"by"`
	Text  string `json:"text"`
	Start int    `json:"start"`           // byte offset of the message (to undo a reply)
	Reply bool   `json:"reply"`           // false for the head of the annotation
	Fix   bool   `json:"fix,omitempty"`   // a correction of the proposal (review.Meta.Fix)
	To    string `json:"to,omitempty"`    // to= : the agent that must answer (D-49)
	Via   string `json:"via,omitempty"`   // via= : the agent that wrote it (D-49)
	Model string `json:"model,omitempty"` // model= : its model (P10.11)
	// Status is new or open, like that of an annotation (package navette):
	// a message not handed over yet may still be amended by its author.
	Status string `json:"status,omitempty"`
}

// Issue is a malformed mark.
type Issue struct {
	File    int    `json:"file"`
	Offset  int    `json:"offset"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Doc is the rendered project.
type Doc struct {
	Title       string       `json:"title"`
	Files       []File       `json:"files"`
	HTML        string       `json:"html"`
	Annotations []Annotation `json:"annotations"`
	Issues      []Issue      `json:"issues"`
	Millis      int64        `json:"millis"`
	// Part is the part of the project rendered ("" : all of it); PerFile
	// says whether a part can be rendered alone (P6.8).
	Part    string `json:"part"`
	PerFile bool   `json:"perFile"`
}

// KindName is the name of a mark kind in the JSON outputs (stable, English).
func KindName(k critic.Kind) string {
	return [...]string{"addition", "deletion", "substitution", "comment", "highlight"}[k]
}

// Renderer renders a project and keeps the source maps of the files it has
// rendered. It is safe for concurrent use.
type Renderer struct {
	P *project.Project

	mu     sync.Mutex
	stage1 map[string]*fileResult // by file path + hash
	html   map[string][]byte      // HTML of one file (P6.5), by file path + view + hash + index
	disk   *diskCache             // on disk (P6.4), when enabled
	// Sections of big files (P6.12): pandoc trees by input, and the keys
	// kept for each file (only its current sections stay).
	trees     map[string][]byte
	treesOf   map[int][]string
	chunkKeys map[int][]string
	// filterHash is the hash of the filter when the HTML kept was made: a
	// filter changed under the same name invalidates it.
	filterHash string
}

// filterState returns the hash of the filter as it is now ("" without
// filter), and forgets the HTML kept if the filter changed.
func (r *Renderer) filterState() string {
	if r.P.Filter == "" {
		return ""
	}
	b, err := os.ReadFile(r.P.Filter)
	if err != nil {
		return ""
	}
	h := store.Hash(b)
	r.mu.Lock()
	defer r.mu.Unlock()
	if h != r.filterHash {
		if r.filterHash != "" {
			r.html = map[string][]byte{}
			r.chunkKeys = map[int][]string{}
		}
		r.filterHash = h
	}
	return h
}

// loaded is a file read and its first stage.
type loaded struct {
	idx int // rank of the file in the project
	src []byte
	res *fileResult
}

type fileResult struct {
	api    any
	blocks json.RawMessage
	m      *sourcemap.Map
	err    error
	parts  []chunkPart // a big file cut into sections (P6.12)
}

// New returns a renderer for p.
func New(p *project.Project) *Renderer {
	return &Renderer{P: p, stage1: map[string]*fileResult{}, html: map[string][]byte{},
		trees: map[string][]byte{}, treesOf: map[int][]string{}, chunkKeys: map[int][]string{}}
}

// Render renders every file of the project, in the view Current.
func (r *Renderer) Render(ctx context.Context) (*Doc, error) {
	return r.RenderView(ctx, projection.Current)
}

// RenderView renders every file of the project in view v (SPEC F3): Current
// (every proposal rejected) or Proposed (every proposal accepted). In the view
// Proposed, the words of the proposals carry the place of their new text in
// the source. The view Review is composed by the interface on the view
// Current, from the annotations.
func (r *Renderer) RenderView(ctx context.Context, v projection.View) (*Doc, error) {
	return r.RenderPart(ctx, v, "")
}

// AutoPart asks RenderPart to choose: the whole project if it is small,
// its first file otherwise.
const AutoPart = "?"

// autoLimit is the size of text under which AutoPart shows everything.
const autoLimit = 1 << 20

// InPart reports whether path belongs to part: "" (everything), a file, or
// a directory ending with « / ».
func InPart(path, part string) bool {
	return part == "" || path == part || (strings.HasSuffix(part, "/") && strings.HasPrefix(path, part))
}

// RenderPart renders the part of the project named by part (P6.8): only its
// files go through pandoc; the annotations, hashes and sizes are given for
// every file. When the HTML cannot be made file by file (a filter that
// numbers across files), everything is rendered. A part that names no file
// renders everything.
func (r *Renderer) RenderPart(ctx context.Context, v projection.View, part string) (*Doc, error) {
	if v != projection.Current && v != projection.Proposed {
		return nil, fmt.Errorf("vue non rendue : %s", v)
	}
	start := time.Now()
	doc := &Doc{Title: r.P.Title, Annotations: []Annotation{}, Issues: []Issue{}, PerFile: r.P.PerFile}
	srcs := make([]store.Doc, len(r.P.Files))
	errs := make([]error, len(r.P.Files))
	total := 0
	for i := range r.P.Files {
		srcs[i], errs[i] = store.Read(r.P.Path(i))
		total += len(srcs[i].Bytes)
	}
	if !r.P.PerFile {
		part = ""
	}
	if part == AutoPart {
		part = ""
		if total > autoLimit && len(r.P.Files) > 1 {
			part = r.P.Files[0]
		}
	}
	if part != "" {
		any := false
		for _, f := range r.P.Files {
			any = any || InPart(f, part)
		}
		if !any {
			part = ""
		}
	}
	doc.Part = part
	files := make([]loaded, len(r.P.Files))
	var wg sync.WaitGroup
	for i := range r.P.Files {
		d, err := srcs[i], errs[i]
		files[i].idx = i
		f := File{Index: i, Path: r.P.Files[i], Shown: InPart(r.P.Files[i], part)}
		if err != nil {
			f.Error = err.Error()
			doc.Files = append(doc.Files, f)
			files[i].res = &fileResult{err: err}
			continue
		}
		f.Hash, f.Size = d.Hash, len(d.Bytes)
		doc.Files = append(doc.Files, f)
		files[i].src = d.Bytes
		if !f.Shown {
			files[i].res = nil // not rendered: its annotations only
			continue
		}
		wg.Add(1)
		go func(i int, d store.Doc) {
			defer wg.Done()
			files[i].res = r.file(ctx, i, d, v)
		}(i, d)
	}
	wg.Wait()

	// The tree is assembled from the JSON of each file as it is, without
	// reading it again (P6.3): it was 2 s of the 13 of a large test book.
	var api any
	parts := make([][]byte, 0, len(files))
	shown := make([]loaded, 0, len(files))
	for i, l := range files {
		if l.res == nil {
			doc.Annotations = append(doc.Annotations, annotations(i, l.src)...)
			doc.Issues = append(doc.Issues, issues(i, l.src)...)
			continue
		}
		if l.res.err != nil {
			doc.Files[i].Error = l.res.err.Error()
			ep, err := json.Marshal([]any{errorPara(l.res.err)})
			if err != nil {
				return nil, err
			}
			parts = append(parts, fileDivRaw(i, r.P.Files[i], ep))
			shown = append(shown, l)
			continue
		}
		if api == nil {
			api = l.res.api
		}
		parts = append(parts, fileDivRaw(i, r.P.Files[i], l.res.blocks))
		shown = append(shown, l)
		doc.Annotations = append(doc.Annotations, annotations(i, l.src)...)
		doc.Issues = append(doc.Issues, issues(i, l.src)...)
	}
	doc.Issues = append(doc.Issues, CrossDuplicates(doc.Annotations)...)
	if api == nil {
		api = []any{1, 23}
	}
	if r.P.PerFile {
		html, err := r.htmlPerFile(ctx, api, v, shown, parts)
		if err != nil {
			return nil, err
		}
		doc.HTML = string(html)
		doc.Millis = time.Since(start).Milliseconds()
		return doc, nil
	}
	tree, err := treeRaw(api, parts)
	if err != nil {
		return nil, err
	}
	// --id-prefix keeps the identifiers of the text (titles, labels) apart
	// from those of the interface; pandoc prefixes internal links as well.
	args := []string{"-f", "json", "-t", "html5", "--wrap=none", "--mathml", "--id-prefix=gl-", "-M", "glose=true"}
	if r.P.Filter != "" {
		args = append(args, "--lua-filter", r.P.Filter)
	}
	html, err := r.pandoc(ctx, tree, args...)
	if err != nil {
		return nil, err
	}
	doc.HTML = string(html)
	doc.Millis = time.Since(start).Milliseconds()
	return doc, nil
}

// Map returns the source map of a file for the given content. It comes from
// the cache of the last renderings, or is computed.
func (r *Renderer) Map(ctx context.Context, i int, d store.Doc) (*sourcemap.Map, error) {
	res := r.file(ctx, i, d, projection.Current)
	return res.m, res.err
}

// file renders one file in view v (first stage), or takes it from the cache,
// which keeps one version per file and per view. Only the map of the view
// Current serves to annotate: the others are used for display only.
func (r *Renderer) file(ctx context.Context, i int, d store.Doc, v projection.View) *fileResult {
	path := r.P.Files[i] + "\x00" + strconv.Itoa(int(v))
	key := path + "\x00" + d.Hash
	r.mu.Lock()
	res, ok := r.stage1[key]
	r.mu.Unlock()
	if ok {
		return res
	}
	var dkey string
	if r.disk != nil {
		dkey = r.disk.stage1Key(r.P.Files[i], i, v, d.Hash)
		res = r.disk.loadStage1(dkey)
	}
	if res == nil {
		res = r.stage1File(ctx, i, d.Bytes, v)
		if errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded) || errors.Is(res.err, errPandocRun) {
			return res
		}
		if r.disk != nil && res.err == nil {
			r.disk.saveStage1(dkey, res)
		}
	}
	r.mu.Lock()
	for k := range r.stage1 { // keep one version per file
		if len(k) > len(path) && k[:len(path)+1] == path+"\x00" {
			delete(r.stage1, k)
		}
	}
	r.stage1[key] = res
	r.mu.Unlock()
	return res
}

func (r *Renderer) stage1File(ctx context.Context, file int, src []byte, v projection.View) *fileResult {
	if !utf8.Valid(src) {
		return &fileResult{err: sourcepos.ErrNotUTF8}
	}
	view := projection.Project(src, v)
	x, err := sourcepos.NewIndex(view.Text)
	if err != nil {
		return &fileResult{err: err}
	}
	// A very big file, without filter: rendered section by section (P6.12).
	if r.P.Filter == "" {
		if _, readable := definitions(view.Text); !readable {
			// a definition of a form not read: rendered whole
		} else if chunks := splitChunks(view.Text); chunks != nil {
			return r.stage1Chunked(ctx, file, src, view, &rewriter{file: file, path: r.P.Files[file], src: src, view: view, x: x}, chunks)
		}
	}
	out, err := r.pandoc(ctx, view.Text, "-f", sourcepos.Reader, "-t", "json")
	if err != nil {
		return &fileResult{err: err}
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var tree map[string]any
	if err := dec.Decode(&tree); err != nil {
		return &fileResult{err: fmt.Errorf("arbre pandoc illisible : %w", err)}
	}
	rw := &rewriter{file: file, path: r.P.Files[file], src: src, view: view, x: x}
	blocks := clean(rw.list(asList(tree["blocks"]), -1))
	rw.m.Finish()
	raw, err := json.Marshal(blocks)
	if err != nil {
		return &fileResult{err: err}
	}
	return &fileResult{api: tree["pandoc-api-version"], blocks: raw, m: &rw.m}
}

// pandocSlots bounds the pandoc processes run at once by all renderers
// (P6.3): thirty readings started together took 3.6 s on a large test book, against
// 1.2 s eight at a time (16 cores).
var pandocSlots = make(chan struct{}, max(2, min(8, runtime.NumCPU()/2)))

// errPandocRun marks a failed run of pandoc (missing program, lack of
// memory…): often passing, it is not kept in the caches, so that the next
// render tries again.
var errPandocRun = errors.New("pandoc")

func (r *Renderer) pandoc(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	select {
	case pandocSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-pandocSlots }()
	cmd := exec.CommandContext(ctx, r.P.Pandoc, args...)
	cmd.Dir = r.P.Root
	cmd.Stdin = bytes.NewReader(input)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w : %v : %s", errPandocRun, err, bytes.TrimSpace(errOut.Bytes()))
	}
	return out.Bytes(), nil
}

func fileDiv(i int, path string, blocks []any) any {
	return map[string]any{"t": "Div", "c": []any{
		[]any{"glose-f" + strconv.Itoa(i), []any{"glose-file"}, []any{[]any{"data-f", strconv.Itoa(i)}, []any{"data-path", path}}},
		blocks,
	}}
}

// htmlArgs are the arguments of the HTML pass of pandoc.
func (r *Renderer) htmlArgs() []string {
	args := []string{"-f", "json", "-t", "html5", "--wrap=none", "--mathml", "--id-prefix=gl-", "-M", "glose=true"}
	if r.P.Filter != "" {
		args = append(args, "--lua-filter", r.P.Filter)
	}
	return args
}

// htmlPerFile makes the HTML file by file (P6.5): each file is one pandoc
// pass, kept while the file does not change, so that after an annotation
// only the file written is made again. The footnotes of a file are numbered
// in it and placed at its end; their identifiers are prefixed by the file
// (footnoteIDs), since every file starts again at note 1.
func (r *Renderer) htmlPerFile(ctx context.Context, api any, v projection.View, files []loaded, parts [][]byte) ([]byte, error) {
	out := make([][]byte, len(parts))
	errs := make([]error, len(parts))
	fh := r.filterState()
	var wg sync.WaitGroup
	for k := range parts {
		i := files[k].idx
		if files[k].res.err == nil && files[k].res.parts != nil {
			wg.Add(1)
			go func(k, i int) {
				defer wg.Done()
				out[k], errs[k] = r.htmlChunked(ctx, api, v, i, files[k].res.parts)
				out[k] = namespaceAnchors(out[k], i)
			}(k, i)
			continue
		}
		var key string
		if files[k].res.err == nil {
			key = htmlSlot(r.P.Files[i], v, i) + store.Hash(files[k].src) + "\x00" + fh
			r.mu.Lock()
			h, ok := r.html[key]
			r.mu.Unlock()
			if !ok && r.disk != nil {
				if h = r.disk.loadHTML(r.disk.htmlKey(r.P.Files[i], i, v, store.Hash(files[k].src), fh)); h != nil {
					ok = true
					r.keepHTML(key, htmlSlot(r.P.Files[i], v, i), h)
				}
			}
			if ok {
				out[k] = h
				continue
			}
		}
		wg.Add(1)
		go func(k, i int, key string) {
			defer wg.Done()
			tree, err := treeRaw(api, [][]byte{parts[k]})
			if err == nil {
				out[k], err = r.pandoc(ctx, tree, r.htmlArgs()...)
			}
			if err != nil {
				errs[k] = err
				return
			}
			out[k] = footnoteIDs(out[k], i)
			out[k] = namespaceAnchors(out[k], i)
			if key == "" {
				return
			}
			if r.disk != nil {
				r.disk.saveHTML(r.disk.htmlKey(r.P.Files[i], i, v, store.Hash(files[k].src), fh), out[k])
			}
			r.keepHTML(key, htmlSlot(r.P.Files[i], v, i), out[k])
		}(k, i, key)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return bytes.Join(out, nil), nil
}

// htmlSlot is the start of the keys of r.html for one file, view and index:
// the content and the filter follow.
func htmlSlot(file string, v projection.View, i int) string {
	return file + "\x00" + strconv.Itoa(int(v)) + "\x00" + strconv.Itoa(i) + "\x00"
}

// keepHTML keeps h under key, and only it among the keys of its slot: one
// version per file, view and index (the older versions of a file annotated
// again and again would otherwise pile up in memory).
func (r *Renderer) keepHTML(key, slot string, h []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.html {
		if strings.HasPrefix(k, slot) {
			delete(r.html, k)
		}
	}
	r.html[key] = h
}

// footnoteRef matches the identifiers that pandoc gives to footnotes.
var footnoteRef = regexp.MustCompile(`(id|href)="(#?)(?:gl-)?(fnref|fn|footnotes)(\d*)"`)

// footnoteIDs prefixes the footnote identifiers of the HTML of file i.
func footnoteIDs(html []byte, i int) []byte {
	repl := []byte(`${1}="${2}gl-f` + strconv.Itoa(i) + `-${3}${4}"`)
	return inTags(html, "f", func(tag []byte) []byte { return footnoteRef.ReplaceAll(tag, repl) })
}

// inTags applies f to each tag of the HTML made by pandoc (from « < » to the
// next « > ») that contains marker, and leaves the text between tags as it is. pandoc escapes « < »
// and « > » in text and in attribute values, but not the quotes of inline
// code: `id="fn1"` written in a text must not be taken for an attribute.
func inTags(h []byte, marker string, f func(tag []byte) []byte) []byte {
	m := []byte(marker)
	if !bytes.Contains(h, m) {
		return h
	}
	out := make([]byte, 0, len(h)+len(h)/16)
	for {
		s := bytes.IndexByte(h, '<')
		if s < 0 {
			return append(out, h...)
		}
		e := bytes.IndexByte(h[s:], '>')
		if e < 0 {
			return append(out, h...)
		}
		e += s + 1
		out = append(out, h[:s]...)
		if tag := h[s:e]; bytes.Contains(tag, m) {
			out = append(out, f(tag)...)
		} else {
			out = append(out, tag...)
		}
		h = h[e:]
	}
}

// fileDivRaw is fileDiv for blocks already in JSON.
func fileDivRaw(i int, path string, blocks []byte) []byte {
	attr, _ := json.Marshal([]any{"glose-f" + strconv.Itoa(i), []any{"glose-file"}, []any{[]any{"data-f", strconv.Itoa(i)}, []any{"data-path", path}}})
	var b bytes.Buffer
	b.Grow(len(blocks) + len(attr) + 32)
	b.WriteString(`{"t":"Div","c":[`)
	b.Write(attr)
	b.WriteByte(',')
	b.Write(blocks)
	b.WriteString(`]}`)
	return b.Bytes()
}

// treeRaw assembles a pandoc document from blocks already in JSON.
func treeRaw(api any, blocks [][]byte) ([]byte, error) {
	a, err := json.Marshal(api)
	if err != nil {
		return nil, err
	}
	n := len(a) + 64
	for _, p := range blocks {
		n += len(p) + 1
	}
	var b bytes.Buffer
	b.Grow(n)
	b.WriteString(`{"pandoc-api-version":`)
	b.Write(a)
	b.WriteString(`,"meta":{},"blocks":[`)
	for i, p := range blocks {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(p)
	}
	b.WriteString(`]}`)
	return b.Bytes(), nil
}

func errorPara(err error) any {
	return map[string]any{"t": "Div", "c": []any{
		[]any{"", []any{"glose-erreur"}, []any{}},
		[]any{map[string]any{"t": "Para", "c": []any{map[string]any{"t": "Str", "c": "Ce fichier ne peut pas être affiché : " + err.Error()}}}},
	}}
}

// LineCol returns the line (from 1) and the column (from 1, in characters) of
// a byte offset.
func LineCol(src []byte, off int) (int, int) {
	off = max(0, min(off, len(src)))
	line, start := 1, 0
	for i := 0; i < off && i < len(src); i++ {
		if src[i] == '\n' {
			line, start = line+1, i+1
		}
	}
	return line, utf8.RuneCount(src[start:min(off, len(src))]) + 1
}

// Annotations returns the annotations of a file, in order.
func Annotations(file int, src []byte) []Annotation {
	out := []Annotation{}
	out = append(out, annotations(file, src)...)
	return out
}

func annotations(file int, src []byte) []Annotation {
	d := review.Read(src)
	links := notes.Links(src)
	var out []Annotation
	for _, ra := range d.Annotations {
		m := ra.Mark
		a := Annotation{File: file, ID: ra.ID, By: ra.By, Start: ra.Span.Start, End: ra.Span.End, Kind: KindName(m.Kind),
			TextStart: m.Text.Start, TextEnd: m.Text.End, Text: string(src[m.Text.Start:m.Text.End]),
			NewStart: m.New.Start, NewEnd: m.New.End,
			New: string(src[m.New.Start:m.New.End]), Comments: []string{}, Messages: []Message{}, To: ra.Recipient(), Via: ra.Via, Model: ra.Model}
		head := len(ra.Messages) - len(ra.Replies())
		for k, msg := range ra.Messages {
			text := string(src[msg.Mark.Text.Start:msg.Mark.Text.End])
			if !(k == 0 && ra.Kind == critic.Comment) {
				a.Comments = append(a.Comments, text)
			}
			a.Messages = append(a.Messages, Message{ID: msg.ID, By: msg.By, Text: text, Start: msg.Span.Start, Reply: k >= head, Fix: msg.Fix,
				To: msg.To, Via: msg.Via, Model: msg.Model})
		}
		if l, ok := links[ra.Span.Start]; ok {
			a.Partners, a.Primary, a.Note = l.Partners, l.Primary, l.Note
		}
		a.Line, a.Column = LineCol(src, ra.Span.Start)
		out = append(out, a)
	}
	return out
}

// Issues returns the malformed marks of a file.
func Issues(file int, src []byte) []Issue { return issues(file, src) }

func issues(file int, src []byte) []Issue {
	d := review.Read(src)
	var out []Issue
	for _, i := range d.MarkIssues {
		l, c := LineCol(src, i.Offset)
		out = append(out, Issue{File: file, Offset: i.Offset, Line: l, Column: c, Message: i.Code.String()})
	}
	for _, i := range d.Issues {
		l, c := LineCol(src, i.Offset)
		out = append(out, Issue{File: file, Offset: i.Offset, Line: l, Column: c, Message: i.Message})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Offset < out[b].Offset })
	return out
}

// CrossDuplicates returns, for annotations of several files, the identifiers
// used in more than one file (each file is checked on its own by Issues).
func CrossDuplicates(anns []Annotation) []Issue {
	first := map[string]int{}
	var out []Issue
	for _, a := range anns {
		ids := []string{a.ID}
		for _, m := range a.Messages {
			if m.Reply {
				ids = append(ids, m.ID)
			}
		}
		for _, id := range ids {
			if id == "" {
				continue
			}
			if f, ok := first[id]; ok && f != a.File {
				out = append(out, Issue{File: a.File, Offset: a.Start, Line: a.Line, Column: a.Column,
					Message: review.IssueDuplicated + " : " + id})
				continue
			}
			first[id] = a.File
		}
	}
	return out
}

var versionRe = regexp.MustCompile(`pandoc(?:\.exe)? (\d+)\.(\d+)`)

// CheckPandoc runs "pandoc --version" and checks that it is 3.1 or newer
// (D-05). It returns the version line.
func CheckPandoc(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("pandoc introuvable (%s) : %v", path, err)
	}
	line, _, _ := bytes.Cut(out, []byte("\n"))
	m := versionRe.FindSubmatch(line)
	if m == nil {
		return "", fmt.Errorf("version de pandoc illisible : %q", line)
	}
	major, _ := strconv.Atoi(string(m[1]))
	minor, _ := strconv.Atoi(string(m[2]))
	if major < 3 || (major == 3 && minor < 1) {
		return string(line), fmt.Errorf("pandoc %d.%d trop ancien : 3.1 ou plus récent requis", major, minor)
	}
	return string(line), nil
}

// LiteralOutsideCode returns the offsets of the marks that Glose leaves
// literal, as written in code (D-24), but that pandoc does not read as code:
// marks probably hidden from the review by the prudent simplifications of
// the code detection (internal/critic/code.go). The comparison is made by
// position: pandoc gives the source range of each code element
// (commonmark_x+sourcepos), on the view « Current », which is mapped back to
// the file. « glose check » reports them.
func (r *Renderer) LiteralOutsideCode(ctx context.Context, src []byte) ([]int, error) {
	res := critic.Scan(src)
	if len(res.Literal) == 0 {
		return nil, nil
	}
	proj := projection.Project(src, projection.Current)
	tree, err := sourcepos.Run(ctx, r.P.Pandoc, proj.Text)
	if err != nil {
		return nil, err
	}
	x, err := sourcepos.NewIndex(proj.Text)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(tree, &doc); err != nil {
		return nil, err
	}
	var code [][2]int // ranges of the view
	var walk func(v any)
	walk = func(v any) {
		switch el := v.(type) {
		case map[string]any:
			if t, _ := el["t"].(string); t == "Code" || t == "CodeBlock" {
				if c, ok := el["c"].([]any); ok && len(c) == 2 {
					if attr, ok := c[0].([]any); ok && len(attr) == 3 {
						kvs, _ := attr[2].([]any)
						for _, kv := range kvs {
							p, ok := kv.([]any)
							if !ok || len(p) != 2 || p[0] != "data-pos" {
								continue
							}
							rs, err := sourcepos.ParseDataPos(fmt.Sprint(p[1]))
							if err != nil {
								continue
							}
							for _, rg := range rs {
								if s, e, _, err := x.Span(rg); err == nil {
									code = append(code, [2]int{s, e})
								}
							}
						}
					}
				}
				return
			}
			for _, y := range el {
				walk(y)
			}
		case []any:
			for _, y := range el {
				walk(y)
			}
		}
	}
	walk(doc)
	var suspect []int
	for _, off := range res.Literal {
		view := -1
		for _, sg := range proj.Segments {
			if off >= sg.Src.Start && off < sg.Src.End {
				view = sg.Out.Start + off - sg.Src.Start
				break
			}
		}
		inside := false
		for _, c := range code {
			if view >= c[0] && view < c[1] {
				inside = true
				break
			}
		}
		if !inside {
			suspect = append(suspect, off)
		}
	}
	return suspect, nil
}

// Preview renders a piece of Markdown (a rewritten paragraph, with the
// definitions of its notes) the way the text is rendered: same reader, same
// neutralisation of raw HTML and attributes, and the project filter if it
// accepts the piece alone. Nothing is cached.
func (r *Renderer) Preview(ctx context.Context, file int, md []byte) (string, error) {
	res := r.stage1File(ctx, file, md, projection.Current)
	if res.err != nil {
		return "", res.err
	}
	var bl []any
	if err := json.Unmarshal(res.blocks, &bl); err != nil {
		return "", err
	}
	path := ""
	if file >= 0 && file < len(r.P.Files) {
		path = r.P.Files[file]
	}
	tree, err := json.Marshal(map[string]any{"pandoc-api-version": res.api, "meta": map[string]any{},
		"blocks": []any{fileDiv(file, path, bl)}})
	if err != nil {
		return "", err
	}
	args := []string{"-f", "json", "-t", "html5", "--wrap=none", "--mathml", "--id-prefix=gl-apercu-", "-M", "glose=true"}
	if r.P.Filter != "" {
		if html, err := r.pandoc(ctx, tree, append(args, "--lua-filter", r.P.Filter)...); err == nil {
			return string(html), nil
		}
	}
	html, err := r.pandoc(ctx, tree, args...)
	return string(html), err
}
