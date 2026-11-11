package server

import (
	"net/http"
	"strconv"
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
	// P22.1: the results come by pages of searchPage (offset).
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if r.URL.Query().Get("offset") == "" {
		offset, err = 0, nil
	}
	if err != nil || offset < 0 {
		jsonError(w, http.StatusBadRequest, "query", "requête invalide")
		return
	}
	results := []result{}
	problems := []string{}
	total, seen := 0, 0
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
		// The matches of this file that fall in the page asked for.
		matches, n := search.Find(d.Bytes, q, v, r.URL.Query().Get("case") == "1", max(0, offset+searchPage-seen))
		total += n
		for k, m := range matches {
			if seen+k >= offset && len(results) < searchPage {
				results = append(results, result{Match: m, File: i, Path: f, Hash: d.Hash})
			}
		}
		seen += n
	}
	next := 0 // none
	if offset+len(results) < total {
		next = offset + len(results)
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": results, "total": total, "offset": offset, "next": next,
		"problems": problems, "filesGen": c.Gen})
}

// searchPage is how many results one answer carries.
const searchPage = 200
