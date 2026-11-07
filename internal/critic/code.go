package critic

import (
	"bytes"
	"sort"
)

// Code in Markdown (D-24). Marks written inside code are literal text. Code
// is found with the CommonMark rules, simplified where they would need a full
// Markdown parser. The simplifications are prudent: when in doubt, a passage
// is not taken for code, since a mark taken for code would vanish from the
// review, while a mark interpreted in code only shows as an annotation.
//
//   - a code span opens with a string of n backticks and closes with the next
//     string of exactly n backticks; a string with no partner is literal. A
//     backslash before the first backtick escapes it (outside code only). A
//     code span does not leave its paragraph: it stops at a blank line and at
//     a line that ends the paragraph (ATX title, fence, thematic break,
//     setext underline, list item that may interrupt a paragraph, start of a
//     blockquote, definition, table row);
//   - a fenced code block opens with a line made of an optional prefix of
//     blockquote markers (">"), at most three spaces of indentation, then at
//     least three backticks or tildes (a backtick fence takes no backtick in
//     its info string). It closes with a line of at least as many of the
//     same character and nothing else, indented at most three spaces, or at
//     the end of its blockquote, or at the end of the text.
//
// Not recognised, by prudence: indented code blocks (four spaces), and fences
// indented by four spaces or more (in a nested list item, for instance).
// Marks there remain interpreted.

// codeScanner holds the backtick strings and blank lines of a text, found once,
// so that each code span is matched in logarithmic time.
type codeScanner struct {
	src   []byte
	runs  map[int][]int // length of a backtick string -> starts, in order
	blank []int         // starts of the lines that end a paragraph, in order
	bom   int           // length of the byte order mark, if any
}

func newCodeScanner(src []byte) *codeScanner {
	c := &codeScanner{src: src, runs: map[int][]int{}}
	if bytes.HasPrefix(src, []byte{0xEF, 0xBB, 0xBF}) {
		c.bom = 3
	}
	for i := 0; i < len(src); {
		j := bytes.IndexByte(src[i:], '`')
		if j < 0 {
			break
		}
		j += i
		n := runLen(src, j, '`')
		c.runs[n] = append(c.runs[n], j)
		i = j + n
	}
	prevQuote := false
	table := false // inside a table: every row is a block of its own
	list := false  // the current paragraph is a list item: any item ends it
	for i := 0; i < len(src); {
		end := c.lineEnd(i)
		p, quote, col := c.indent(i)
		blank := c.isBlank(i)
		item := col <= 3 && c.listItem(p, end)
		boundary := true
		switch {
		case blank:
			table = false
		case table:
		case quote && !prevQuote: // a blockquote starts
		case col <= 3 && c.interrupts(p, end):
		case col <= 3 && c.tableStart(p, end):
			table = true
		case list && item: // the next item of a list
		default:
			boundary = false
		}
		if boundary {
			c.blank = append(c.blank, i)
			list = item
		}
		// A one-line block ends at its line end: title, setext underline,
		// thematic break, table row.
		if !blank && (table || col <= 3 && c.oneLine(p, end)) && end < len(src) {
			c.blank = append(c.blank, end+1)
		}
		prevQuote = quote
		if end >= len(src) {
			break
		}
		i = end + 1
	}
	sort.Ints(c.blank)
	return c
}

// interrupts reports whether the line content src[p:end] (after its
// blockquote markers and at most three spaces of indentation) starts a block
// that ends the paragraph before it.
func (c *codeScanner) interrupts(p, end int) bool {
	line := bytes.TrimRight(c.src[p:end], "\r")
	if len(line) == 0 {
		return false
	}
	rest := func(k int) bool { // a space, then something
		return k < len(line) && (line[k] == ' ' || line[k] == '\t') && len(bytes.TrimSpace(line[k:])) > 0
	}
	switch line[0] {
	case '#', '=':
		return c.oneLine(p, end)
	case '`', '~':
		if runLen(line, 0, line[0]) >= 3 {
			return true
		}
		return line[0] == '~' && rest(1) // a definition (pandoc: « ~ »)
	case ':':
		if runLen(line, 0, ':') >= 3 {
			return true // a fenced div of pandoc (« ::: »)
		}
		return rest(1) // a definition
	case '-', '*', '+', '_':
		if line[0] != '_' && rest(1) {
			return true // a list item
		}
		return c.oneLine(p, end) // a thematic break or a setext underline
	case '1':
		// An ordered list interrupts a paragraph only if it starts at 1.
		return len(line) > 2 && (line[1] == '.' || line[1] == ')') && rest(2)
	}
	return false
}

// listItem reports whether the line content src[p:end] starts a list item,
// bulleted or numbered (any number).
func (c *codeScanner) listItem(p, end int) bool {
	line := bytes.TrimRight(c.src[p:end], "\r")
	rest := func(k int) bool {
		return k < len(line) && (line[k] == ' ' || line[k] == '\t') && len(bytes.TrimSpace(line[k:])) > 0
	}
	if len(line) > 1 && (line[0] == '-' || line[0] == '*' || line[0] == '+') {
		return rest(1)
	}
	k := 0
	for k < len(line) && k < 9 && line[k] >= '0' && line[k] <= '9' {
		k++
	}
	return k > 0 && k < len(line) && (line[k] == '.' || line[k] == ')') && rest(k+1)
}

// oneLine reports whether the line content src[p:end] is a block of a single
// line: an ATX title, a setext underline, a thematic break.
func (c *codeScanner) oneLine(p, end int) bool {
	line := bytes.TrimRight(c.src[p:end], " \t\r")
	if len(line) == 0 {
		return false
	}
	switch line[0] {
	case '#':
		n := runLen(line, 0, '#')
		return n <= 6 && (n == len(line) || line[n] == ' ' || line[n] == '\t')
	case '=':
		return runLen(line, 0, '=') == len(line)
	case '-', '*', '_':
		n := 0
		for _, b := range line {
			switch b {
			case line[0]:
				n++
			case ' ', '\t':
			default:
				return false
			}
		}
		return n >= 3 || line[0] == '-' && n == len(line) // « --- », or a setext underline « - »
	}
	return false
}

// tableStart reports whether the line content src[p:end] is the header row of
// a pipe table: the next line is a delimiter row (« |---|:--| »).
func (c *codeScanner) tableStart(p, end int) bool {
	if bytes.IndexByte(c.src[p:end], '|') < 0 || end >= len(c.src) {
		return false
	}
	q, _ := c.prefix(end + 1)
	row := bytes.TrimSpace(c.src[q:c.lineEnd(end+1)])
	if len(row) == 0 || bytes.IndexByte(row, '-') < 0 {
		return false
	}
	pipes := 0
	for _, b := range row {
		switch b {
		case '|':
			pipes++
		case '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return pipes > 0
}

func runLen(src []byte, i int, ch byte) int {
	n := 0
	for i+n < len(src) && src[i+n] == ch {
		n++
	}
	return n
}

// isBlank reports whether the line that starts at i holds only spaces, tabs
// and blockquote markers: it ends a paragraph.
func (c *codeScanner) isBlank(i int) bool {
	for ; i < len(c.src); i++ {
		switch c.src[i] {
		case ' ', '\t', '\r', '>':
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

// lineStart reports whether i is the start of a line.
func (c *codeScanner) lineStart(i int) bool {
	return i == c.bom || i > 0 && c.src[i-1] == '\n'
}

// codeSpan examines the backtick string at j (the first backtick of a
// string). It returns the code span that it opens, or ok == false and the end
// of the string, where the scan resumes.
func (c *codeScanner) codeSpan(j int) (start, end int, ok bool) {
	src := c.src
	n := runLen(src, j, '`')
	if j > 0 && src[j-1] == '`' {
		// Inside a string already examined (cannot happen from Scan).
		return 0, j + n, false
	}
	open, olen := j, n
	bs := 0
	for k := j - 1; k >= 0 && src[k] == '\\'; k-- {
		bs++
	}
	if bs%2 == 1 { // "\`": the first backtick is escaped
		open, olen = j+1, n-1
	}
	if olen == 0 {
		return 0, j + n, false
	}
	limit := len(src)
	if k := sort.SearchInts(c.blank, j+n); k < len(c.blank) {
		limit = c.blank[k]
	}
	starts := c.runs[olen]
	k := sort.SearchInts(starts, j+n)
	if k == len(starts) || starts[k] >= limit {
		return 0, j + n, false
	}
	return open, starts[k] + olen, true
}

// prefix skips the blockquote markers and the indentation that start the
// line at i. It returns the position after them and whether a ">" was seen.
func (c *codeScanner) prefix(i int) (int, bool) {
	p, quote, _ := c.indent(i)
	return p, quote
}

// indent is prefix, with the indentation in columns after the last
// blockquote marker (and the optional space that follows it).
func (c *codeScanner) indent(i int) (int, bool, int) {
	quote := false
	col := 0
	for i < len(c.src) {
		switch c.src[i] {
		case ' ':
			col++
			i++
		case '\t':
			col += 4 - col%4
			i++
		case '>':
			quote = true
			i++
			col = 0
			if i < len(c.src) && c.src[i] == ' ' {
				i++
			}
		default:
			return i, quote, col
		}
	}
	return i, quote, col
}

// lineEnd returns the end of the line that contains i (the position of its
// "\n", or the end of the text).
func (c *codeScanner) lineEnd(i int) int {
	if j := bytes.IndexByte(c.src[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(c.src)
}

// fence examines the line that starts at i. If it opens a fenced code block,
// it returns the end of the block: the end of its closing line (before the
// line ending), or the start of the first line out of its blockquote, or the
// end of the text.
func (c *codeScanner) fence(i int) (int, bool) {
	src := c.src
	p, quote, col := c.indent(i)
	if p >= len(src) || (src[p] != '`' && src[p] != '~') {
		return 0, false
	}
	if col > 3 {
		return 0, false // indented code, a paragraph continued, or a nested list: not taken for a fence
	}
	ch := src[p]
	n := runLen(src, p, ch)
	if n < 3 {
		return 0, false
	}
	eol := c.lineEnd(p)
	if ch == '`' && bytes.IndexByte(src[p+n:eol], '`') >= 0 {
		return 0, false // inline code such as ```a```, not a fence
	}
	for line := eol + 1; line <= len(src); {
		if line == len(src) {
			return len(src), true
		}
		q, inQuote, qcol := c.indent(line)
		if quote && !inQuote {
			return line, true // the blockquote ends, and the code block with it
		}
		end := c.lineEnd(line)
		if m := runLen(src, q, ch); m >= n && qcol <= 3 && len(bytes.Trim(src[q+m:end], " \t\r")) == 0 {
			return end, true
		}
		if end == len(src) {
			return len(src), true
		}
		line = end + 1
	}
	return len(src), true
}
