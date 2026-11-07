package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/projection"
)

// stdin is read when an option is given the value « - »; tests replace it.
var stdin io.Reader = os.Stdin

// fromStdin replaces by the standard input the one option among opts whose
// value is « - »: a long text, on several lines, with quotes or backticks,
// passes without shell quoting. The last line ending is removed (« echo »
// and here-documents add one). Two options at « - » are refused.
func fromStdin(opts map[string]*string) error {
	var name string
	for n, v := range opts {
		if *v == "-" {
			if name != "" {
				return errors.New("une seule option peut valoir « - » (lue sur l'entrée standard)")
			}
			name = n
		}
	}
	if name == "" {
		return nil
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	if !utf8.Valid(b) {
		return errors.New("--" + name + " - : l'entrée standard n'est pas en UTF-8 valide")
	}
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	*opts[name] = s
	return nil
}

// blankLine separates two paragraphs of the source.
var blankLine = regexp.MustCompile(`\n[ \t]*\r?\n`)

// contextMax bounds the context given with an annotation, in bytes.
const contextMax = 1200

// annotationContext returns the paragraph of src that holds the annotation
// starting at start and ending at end, as it reads now (view Current: without
// comments, proposals not applied), so that an agent knows where a comment
// stands without opening the file. A long paragraph is cut around the
// annotation, with « … ».
func annotationContext(src []byte, start, end int) string {
	ps, pe := 0, len(src)
	for _, m := range blankLine.FindAllIndex(src, -1) {
		if m[1] <= start {
			ps = m[1]
		} else if m[0] >= end {
			pe = m[0]
			break
		}
	}
	// A mark that crosses the edges of the paragraph (a comment with a
	// blank line) is taken whole: a mark is never cut.
	marks, _ := critic.Parse(src)
	for changed := true; changed; {
		changed = false
		for _, m := range marks {
			if m.Span.Start < pe && m.Span.End > ps && (m.Span.Start < ps || m.Span.End > pe) {
				ps, pe = min(ps, m.Span.Start), max(pe, m.Span.End)
				changed = true
			}
		}
	}
	if ps == 0 && bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) {
		ps = 3
	}
	text := projection.Project(src[ps:pe], projection.Current).Text
	if len(text) > contextMax && start >= ps {
		at := len(projection.Project(src[ps:start], projection.Current).Text)
		from := max(0, at-contextMax/3)
		to := min(len(text), from+contextMax)
		from = max(0, to-contextMax)
		for from > 0 && !utf8.RuneStart(text[from]) {
			from--
		}
		for to < len(text) && !utf8.RuneStart(text[to]) {
			to++
		}
		cut := string(text[from:to])
		if from > 0 {
			cut = "…" + cut
		}
		if to < len(text) {
			cut += "…"
		}
		return strings.TrimSpace(cut)
	}
	return strings.TrimSpace(string(text))
}
