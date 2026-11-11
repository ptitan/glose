package server

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/edit"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

type sinceReference struct {
	Turn int    `json:"turn"`
	Time string `json:"time"`
}
type sinceChange struct {
	Path   string `json:"path"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Status string `json:"status,omitempty"`
}
type sinceResult struct {
	Hashes     map[string]string `json:"hashes"`
	FilesGen   int64             `json:"filesGen"`
	References []sinceReference  `json:"references"`
	Turn       int               `json:"turn"`
	Changes    []sinceChange     `json:"changes"`
}

// since compares only verified snapshots and current projections. No cache or write.
func (s *Server) since(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	h, err := c.N.History()
	if err != nil {
		jsonError(w, 500, "journal", err.Error())
		return
	}
	out := sinceResult{Hashes: map[string]string{}, FilesGen: c.Gen, References: []sinceReference{}, Changes: []sinceChange{}}
	available := map[int]journal.Event{}
	for _, ev := range h.Turns {
		if ev.Actor != review.Author {
			continue
		}
		valid := len(ev.TurnEv.Files) > 0
		for _, f := range ev.TurnEv.Files {
			if _, err := journal.LoadSnapshot(c.P.State(), f.Hash); err != nil {
				valid = false
				break
			}
		}
		if valid {
			out.References = append(out.References, sinceReference{ev.TurnEv.Number, ev.Time})
			available[ev.TurnEv.Number] = ev
			out.Turn = ev.TurnEv.Number
		}
	}
	if q := r.URL.Query().Get("turn"); q != "" {
		n, e := strconv.Atoi(q)
		if e != nil || available[n].TurnEv == nil {
			jsonError(w, 400, "reference", "copie de remise indisponible")
			return
		}
		out.Turn = n
	}
	if out.Turn == 0 {
		writeJSON(w, 200, out)
		return
	}
	old := map[string]string{}
	for _, f := range available[out.Turn].TurnEv.Files {
		old[f.Path] = f.Hash
	}
	for _, path := range c.P.Files {
		abs, _, e := c.P.Inside(path)
		if e != nil {
			jsonError(w, 422, "path", e.Error())
			return
		}
		d, e := store.Read(abs)
		out.Hashes[path] = d.Hash
		hash, existed := old[path]
		delete(old, path)
		if e != nil {
			if existed {
				out.Changes = append(out.Changes, sinceChange{Path: path, Status: "disappeared"})
			}
			continue
		}
		cur := projection.Project(d.Bytes, projection.Current)
		if !existed {
			out.Changes = append(out.Changes, sinceChange{Path: path, End: len(d.Bytes), New: string(cur.Text), Status: "appeared"})
			continue
		}
		b, e := journal.LoadSnapshot(c.P.State(), hash)
		if e != nil {
			jsonError(w, 409, "reference", e.Error())
			return
		}
		before := projection.Project(b, projection.Current).Text
		if bytes.Equal(before, cur.Text) {
			continue
		}
		pe, e := edit.Propose(before, critic.Span{End: len(before)}, string(cur.Text))
		if e != nil {
			jsonError(w, 422, "comparison", fmt.Sprintf("%s : %v", path, e))
			return
		}
		comparison := pe.Apply(before)
		diff := projection.Project(comparison, projection.Proposed)
		for i, m := range diff.Marks {
			span := diff.Places[i]
			a, z := len(d.Bytes), len(d.Bytes)
			if span.Start < len(cur.Text) {
				a, _, _ = cur.SourceStart(span.Start)
				z = a
			}
			if span.End > span.Start {
				z, _, _ = cur.SourceEnd(span.End)
			}
			change := sinceChange{Path: path, Start: a, End: z}
			if m.Kind == critic.Substitution || m.Kind == critic.Deletion {
				change.Old = string(comparison[m.Text.Start:m.Text.End])
			}
			if span.End > span.Start {
				change.New = string(cur.Text[span.Start:span.End])
			}
			out.Changes = append(out.Changes, change)
		}
	}
	for path := range old {
		out.Changes = append(out.Changes, sinceChange{Path: path, Status: "disappeared"})
	}
	writeJSON(w, 200, out)
}
