package server

import (
	"context"
	"net/http"
	"time"

	"github.com/ptitan/glose/internal/projection"
)

func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	c, ok := s.curFor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	from := q.Get("from")
	known := false
	for _, f := range c.P.Files {
		known = known || from == f
	}
	if !known || len(q.Get("href")) > 8192 {
		jsonError(w, http.StatusBadRequest, "link", "fichier source ou lien invalide")
		return
	}
	v := projection.Current
	if q.Get("view") == "proposed" {
		v = projection.Proposed
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	dst, err := c.R.Resolve(ctx, from, q.Get("href"), v)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "link", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dst)
}
