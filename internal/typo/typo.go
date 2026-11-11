// Package typo computes deterministic corrections on certified source bytes.
package typo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/store"
)

type Rule struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Disabled    bool   `json:"disabled,omitempty"`
}
type Correction struct {
	Rule       string
	Start, End int
	Text       string
}

func Defaults() []Rule {
	r := []Rule{}
	for i, p := range []string{";", ":", "!", "?", "»"} {
		r = append(r, Rule{ID: []string{"semicolon", "colon", "exclamation", "question", "closing-quote"}[i], Description: "Place une espace insécable avant « " + p + " ».", Pattern: `([^\s ` + regexp.QuoteMeta(p) + `])[ \t]*(` + regexp.QuoteMeta(p) + `)`, Replacement: "${1}\u00a0${2}"})
	}
	return append(r,
		Rule{ID: "opening-quote", Description: "Place une espace insécable après «.", Pattern: `«[ \t]*([^\s «])`, Replacement: "«\u00a0${1}"},
		Rule{ID: "paired-quotes", Description: "Remplace deux guillemets droits sûrement appariés par des guillemets français.", Pattern: `"([^"\r\n]+)"`, Replacement: "«\u00a0${1}\u00a0»"},
		Rule{ID: "footnote", Description: "Retire les espaces avant un appel de note.", Pattern: `[ \t]+(\[\^[^\]\s]+\])`, Replacement: "${1}"},
		Rule{ID: "article", Description: "Lie art. au numéro qui suit par une espace insécable.", Pattern: `\bart\.[ \t]+([0-9])`, Replacement: "art.\u00a0${1}"},
		Rule{ID: "number", Description: "Lie n° au numéro qui suit par une espace insécable.", Pattern: `\bn°[ \t]+([0-9])`, Replacement: "n°\u00a0${1}"},
		Rule{ID: "ellipsis", Description: "Remplace trois points par un caractère de points de suspension.", Pattern: `\.\.\.`, Replacement: "…"})
}

func Validate(rules []Rule) error {
	seen := map[string]bool{}
	for _, r := range rules {
		if !review.ValidName(r.ID) || seen[r.ID] || strings.TrimSpace(r.Description) == "" || len(r.Pattern) > 4096 || len(r.Replacement) > 4096 || !utf8.ValidString(r.Replacement) || critic.ContainsDelimiter([]byte(r.Replacement)) {
			return fmt.Errorf("règle Typo invalide : %q", r.ID)
		}
		re, e := regexp.Compile(r.Pattern)
		if e != nil || re.MatchString("") {
			return fmt.Errorf("motif Typo invalide : %s", r.ID)
		}
		seen[r.ID] = true
	}
	return nil
}

// Load creates defaults once under a lock. Existing author bytes are never rewritten.
func Load() ([]Rule, error) {
	dir, e := agents.ConfigDir()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, e := store.WaitLockNamed(ctx, dir, "typo.lock")
	if e != nil {
		return nil, e
	}
	defer lock.Unlock()
	path := filepath.Join(dir, "typo.json")
	b, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		r := Defaults()
		b, e = json.MarshalIndent(r, "", "  ")
		if e == nil {
			e = store.CreateAtomic(path, b, 0600)
		}
		if e != nil {
			return nil, e
		}
		return r, nil
	}
	if e != nil {
		return nil, e
	}
	var r []Rule
	if e = json.Unmarshal(b, &r); e != nil {
		return nil, fmt.Errorf("typo.json illisible : %w", e)
	}
	return r, Validate(r)
}

// excluded deliberately over-excludes doubtful syntax. The source map is the final authority.
func excluded(src []byte, quotes bool) []bool {
	mask := make([]bool, len(src))
	mark := func(a, b int) {
		for i := max(0, a); i < min(b, len(mask)); i++ {
			mask[i] = true
		}
	}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile("(?s)`+[^`]*`+"),
		regexp.MustCompile(`(?m)^(?:\t| {4}).*$`),
		regexp.MustCompile(`(?s)\[[^\]\r\n]*\](?:\([^)]*\)|\[[^\]]*\])?`),
		regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:[^\r\n]*`),
		regexp.MustCompile(`(?s)\{[^}]*\}`),
		regexp.MustCompile(`\\[A-Za-z]+(?:\{[^}]*\})*`),
		regexp.MustCompile(`(?s)\$[^$]*\$`),
		regexp.MustCompile(`<[^>\r\n]*>`),
		regexp.MustCompile(`(?m)^ {0,3}>.*$`),
	} {
		for _, p := range re.FindAllIndex(src, -1) {
			mark(p[0], p[1])
		}
	}
	// Code fences and frontmatter are line-oriented syntax, but offsets remain bytes.
	fence := ""
	front := ""
	start := 0
	for start < len(src) {
		end := bytes.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start + 1
		}
		line := strings.TrimSpace(string(src[start:end]))
		line = strings.TrimPrefix(line, "\ufeff")
		if start == 0 && (line == "---" || line == "+++") {
			front = line
			mark(start, end)
		} else if front != "" {
			mark(start, end)
			if line == front || line == "..." {
				front = ""
			}
		} else if fence != "" {
			mark(start, end)
			if strings.HasPrefix(line, fence) {
				fence = ""
			}
		} else if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fence = line[:3]
			mark(start, end)
		}
		start = end
	}
	marks, issues := critic.Parse(src)
	for _, m := range marks {
		mark(m.Span.Start, m.Span.End)
	}
	for _, i := range issues {
		mark(i.Offset, len(src))
	}
	if !quotes {
		return mask
	}
	// Quoted contents are left alone, including an unmatched opening quote.
	for _, q := range [][2]string{{"«", "»"}, {"\"", "\""}, {"“", "”"}, {"‘", "’"}} {
		for at := 0; at < len(src); {
			a := bytes.Index(src[at:], []byte(q[0]))
			if a < 0 {
				break
			}
			a += at
			b := bytes.Index(src[a+len(q[0]):], []byte(q[1]))
			if b < 0 {
				b = bytes.IndexByte(src[a:], '\n')
				if b < 0 {
					b = len(src) - a
				}
				mark(a+len(q[0]), a+b)
				break
			}
			b += a + len(q[0])
			lo, hi := a+len(q[0]), b
			for lo < hi && (src[lo] == ' ' || src[lo] == '\t') {
				lo++
			}
			for hi > lo && (src[hi-1] == ' ' || src[hi-1] == '\t') {
				hi--
			}
			mark(lo, hi)
			at = b + len(q[1])
		}
	}
	return mask
}

func Find(src []byte, m *sourcemap.Map, rules []Rule) ([]Correction, error) {
	if e := Validate(rules); e != nil {
		return nil, e
	}
	if !utf8.Valid(src) {
		return nil, fmt.Errorf("texte non UTF-8")
	}
	mask := excluded(src, true)
	structural := excluded(src, false)
	out := []Correction{}
	for _, r := range rules {
		if r.Disabled {
			continue
		}
		re, _ := regexp.Compile(r.Pattern)
		for _, loc := range re.FindAllSubmatchIndex(src, -1) {
			a, b := loc[0], loc[1]
			replacement := re.Expand(nil, []byte(r.Replacement), src, loc)
			if bytes.Equal(src[a:b], replacement) {
				continue
			}
			// Ambiguous quotes (inches, embedded quotes, several pairs) are never guessed.
			pair := r.ID == "paired-quotes"
			if pair {
				nested := false
				for _, q := range [][2]string{{"«", "»"}, {"“", "”"}, {"‘", "’"}} {
					if bytes.LastIndex(src[:a], []byte(q[0])) > bytes.LastIndex(src[:a], []byte(q[1])) {
						nested = true
					}
				}
				if nested {
					continue
				}
				blocked := false
				for i := a; i < b; i++ {
					if structural[i] {
						blocked = true
						break
					}
				}
				if blocked {
					continue
				}
				left := bytes.LastIndexByte(src[:a], '\n') + 1
				right := bytes.IndexByte(src[b:], '\n')
				if right < 0 {
					right = len(src)
				} else {
					right += b
				}
				if bytes.Count(src[left:right], []byte(`"`)) != 2 || (a > 0 && !unicode.IsSpace(rune(src[a-1])) && !strings.ContainsRune("([", rune(src[a-1]))) || (b < len(src) && !unicode.IsSpace(rune(src[b])) && !strings.ContainsRune(".,;:!?)]", rune(src[b]))) {
					continue
				}
				// No code, links, macros, attributes or markup within the quoted phrase.
				if bytes.ContainsAny(src[a+1:b-1], "`[]{}\\$<>«»") {
					continue
				}
			}
			// Compare only changed bytes; unchanged context may include quote delimiters or a note call.
			p := 0
			for p < b-a && p < len(replacement) && src[a+p] == replacement[p] {
				p++
			}
			for p > 0 && p < b-a && !utf8.RuneStart(src[a+p]) {
				p--
			}
			q := 0
			for q < b-a-p && q < len(replacement)-p && src[b-1-q] == replacement[len(replacement)-1-q] {
				q++
			}
			for q > 0 && b-q < len(src) && !utf8.RuneStart(src[b-q]) {
				q--
			}
			s, e := a+p, b-q
			blocked := false
			for i := s; i < e; i++ {
				if mask[i] && !pair {
					blocked = true
				}
			}
			if s == e && ((s > 0 && mask[s-1]) || (s < len(mask) && mask[s])) && !pair && r.ID != "opening-quote" && r.ID != "closing-quote" {
				blocked = true
			}
			if r.ID == "opening-quote" || r.ID == "closing-quote" {
				for i := a; i < b; i++ {
					if structural[i] {
						blocked = true
						break
					}
				}
			}
			if blocked {
				continue
			}
			if r.ID == "colon" {
				if b < len(src) && src[b] == '/' {
					continue
				}
				if src[a] >= '0' && src[a] <= '9' {
					continue
				}
			}
			if r.ID == "ellipsis" && ((a > 0 && src[a-1] == '.') || (b < len(src) && src[b] == '.')) {
				continue
			}
			if m == nil {
				continue
			}
			check, err := m.Check(src, a, b)
			if err != nil || check.Widened {
				continue
			}
			c := Correction{r.ID, s, e, string(replacement[p : len(replacement)-q])}
			overlap := false
			for _, old := range out {
				if c.Start <= old.End && c.End >= old.Start {
					overlap = true
					break
				}
			}
			if !overlap {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out, nil
}

// Propose creates one signed ordinary annotation per correction, from right to left.
func Propose(src []byte, corrections []Correction, gen *review.Gen) (patch.Edit, error) {
	out := append([]byte(nil), src...)
	for i := len(corrections) - 1; i >= 0; i-- {
		c := corrections[i]
		var e patch.Edit
		var err error
		span := critic.Span{Start: c.Start, End: c.End}
		if c.Start == c.End {
			e, err = patch.ProposeAddition(out, c.Start, c.Text)
		} else if c.Text == "" {
			e, err = patch.ProposeDeletion(out, span)
		} else {
			e, err = patch.ProposeSubstitution(out, span, c.Text)
		}
		if err != nil {
			return patch.Edit{}, err
		}
		e, err = review.WithIDs(out, e, review.Author, gen)
		if err != nil {
			return patch.Edit{}, err
		}
		e, err = review.Sign(out, e, "Typo")
		if err != nil {
			return patch.Edit{}, err
		}
		out = e.Apply(out)
	}
	return review.Diff(src, out), nil
}
