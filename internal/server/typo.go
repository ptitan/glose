package server

import (
	"net/http"

	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/typo"
)

func (s *Server) typo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File int    `json:"file"`
		Hash string `json:"hash"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.edit(w, r, req.File, req.Hash, "typo", "", func(src []byte, m *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		rules, e := typo.Load()
		if e != nil {
			return patch.Edit{}, &apiError{status: 422, code: "rules", message: e.Error()}
		}
		corrections, e := typo.Find(src, m, rules)
		if e != nil {
			return patch.Edit{}, &apiError{status: 422, code: "typo", message: e.Error()}
		}
		if len(corrections) == 0 {
			return patch.Edit{}, &apiError{status: 422, code: "unchanged", message: "Aucune correction typographique sûre dans ce fichier."}
		}
		edit, e := typo.Propose(src, corrections, gen)
		if e != nil {
			return patch.Edit{}, &apiError{status: 422, code: "typo", message: e.Error()}
		}
		return edit, nil
	})
}
