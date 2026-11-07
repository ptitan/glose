package server

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/search"
	"github.com/ptitan/glose/internal/store"
)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" || len(q) > 512 || !utf8.ValidString(q) {
		jsonError(w, http.StatusBadRequest, "query", "saisissez un texte à chercher (512 octets au plus)")
		return
	}
	v := projection.Current
	if r.URL.Query().Get("view") == "proposed" {
		v = projection.Proposed
	}
	type result struct {
		search.Match
		File int    `json:"file"`
		Path string `json:"path"`
		Hash string `json:"hash"`
	}
	results := []result{}
	problems := []string{}
	total := 0
	for i, f := range c.P.Files {
		if r.Context().Err() != nil {
			return
		}
		if _, _, err := c.P.Inside(f); err != nil {
			problems = append(problems, f+" : "+err.Error())
			continue
		}
		d, err := store.Read(c.P.Path(i))
		if err != nil || !utf8.Valid(d.Bytes) {
			problems = append(problems, f+" : fichier illisible ou non UTF-8")
			continue
		}
		matches, n := search.Find(d.Bytes, q, v, r.URL.Query().Get("case") == "1", max(0, 200-len(results)))
		total += n
		for _, m := range matches {
			results = append(results, result{Match: m, File: i, Path: f, Hash: d.Hash})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": results, "total": total, "problems": problems, "filesGen": c.Gen})
}
