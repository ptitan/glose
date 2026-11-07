package server

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/edit"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/sourcemap"
)

// proposeEdit validates the requested source range and computes the proposals,
// without writing. Both the preview and the write handler use this one path.
func proposeEdit(req editRequest, note string, src []byte, m *sourcemap.Map, gen *review.Gen) (patch.Edit, critic.Span, *apiError) {
	var start, end int
	switch req.Mode {
	case "", "block":
		var err error
		start, end, err = m.BlockRange(src, req.At)
		if err != nil || start != req.Start || end != req.End {
			return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "boundary",
				message: "le passage modifié ne correspond plus à un bloc du texte"}
		}
	case "lines":
		var err error
		start, end, err = m.LinesRange(src, req.At, req.To)
		if err != nil || start != req.Start || end != req.End {
			return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "boundary",
				message: "le passage modifié ne correspond plus aux lignes du texte"}
		}
	case "insert":
		// A new element before or after the lines of an element (SPEC F8
		// ter): one addition, separated by a blank line (or a line ending
		// for a list item).
		s, e, err := m.LinesRange(src, req.At, req.To)
		if err != nil || (req.Where != "before" && req.Where != "after") {
			return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "boundary",
				message: "l'élément visé ne correspond plus aux lignes du texte"}
		}
		body := strings.Trim(req.Text, "\r\n")
		if strings.TrimSpace(body) == "" {
			return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: edit.ErrUnchanged.Error()}
		}
		sep := "\n\n"
		if req.Tight {
			sep = "\n"
		}
		at, text := s, body+sep
		if req.Where == "after" {
			// At the start of what follows, so that the addition starts its
			// line; at the end of the element if nothing follows.
			q := e
			if strings.HasPrefix(string(src[q:]), "\r\n") {
				q += 2
			} else if q < len(src) && src[q] == '\n' {
				q++
			}
			for !req.Tight && q < len(src) {
				line := q
				for line < len(src) && (src[line] == ' ' || src[line] == '\t' || src[line] == '\r') {
					line++
				}
				if line >= len(src) || src[line] != '\n' {
					break
				}
				q = line + 1
			}
			if q > e && q < len(src) {
				at = q
			} else {
				at, text = e, sep+body
			}
		}
		if bytes.Contains(src, []byte("\r\n")) {
			text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
		}
		// An insertion goes after an annotation and its whole thread,
		// never inside it.
		marks, _ := critic.Parse(src)
		for _, g := range critic.Group(marks) {
			if g.Span.Start < at && at < g.Span.End {
				at = g.Span.End
			}
		}
		e2, rng, err := review.Propose(src, critic.Span{Start: at, End: at}, text, note, review.Author, gen)
		if err != nil {
			return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		return e2, rng, nil
	case "passage":
		res, err := m.Check(src, req.Start, req.End)
		if err != nil || res.Widened {
			return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "boundary",
				message: "le passage ne peut pas être réécrit tel quel ; sélectionnez-le de nouveau"}
		}
		start, end = res.Start, res.End
	default:
		return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusBadRequest, code: "mode", message: "mode inconnu"}
	}
	text := req.Text
	if req.Mode == "" || req.Mode == "block" || req.Mode == "lines" {
		// Compare the closing line ending too: an appended line must start
		// at its beginning, not inside the preceding line.
		nl := ""
		if strings.HasPrefix(string(src[end:]), "\r\n") {
			nl = "\r\n"
		} else if end < len(src) && src[end] == '\n' {
			nl = "\n"
		}
		if nl != "" && !strings.HasSuffix(text, "\n") {
			end += len(nl)
			text += nl
		}
	}
	propose := review.Propose
	if req.Whole {
		propose = review.ProposeWhole
	}
	e, rng, err := propose(src, critic.Span{Start: start, End: end}, text, note, review.Author, gen)
	if err != nil {
		return patch.Edit{}, critic.Span{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
	}
	return e, rng, nil
}
