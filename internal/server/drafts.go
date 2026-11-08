package server

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/store"
)

// Drafts are private, unfinished input, not annotations. Saving or removing
// one never writes a manuscript or the exchange journal, whoever has the hand.
// Paths (not the indices of the current render) survive a reordered profile.
type Draft struct {
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	Hash         string `json:"hash"`
	Mode         string `json:"mode"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
	At           int    `json:"at"`
	To           int    `json:"to"`
	Annotation   int    `json:"annotation"`
	AnnotationID string `json:"annotationID"`
	ProposalKind string `json:"proposalKind"`
	Old          string `json:"old"`
	Original     string `json:"original"`
	Text         string `json:"text"`
	Note         string `json:"note"`
	Excerpt      string `json:"excerpt"`
	Prefix       string `json:"prefix"`
	Where        string `json:"where"`
	Tight        bool   `json:"tight"`
	Whole        bool   `json:"whole"`
	Editor       string `json:"editor"`
	Title        string `json:"title"`
	Info         string `json:"info"`
	// Sending is persisted BEFORE submitting a manuscript operation. If its
	// response is lost, recovery must not offer an automatic second submission.
	Sending bool `json:"sending"`
}

type draftRecord struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Updated string `json:"updated"`
	Deleted bool   `json:"deleted,omitempty"`
	Draft   *Draft `json:"draft,omitempty"`
}

type draftReply struct {
	draftRecord
	Hash string `json:"hash"`
}

type draftRequest struct {
	ID     string `json:"id"`
	Hash   string `json:"hash"` // hash of the previous record, empty only on creation
	Draft  *Draft `json:"draft"`
	Remove bool   `json:"remove"`
}

func validDraftID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && id == strings.ToLower(id)
}

func (s *Server) draftsDir() (string, error) {
	c := s.cur()
	// Also check existing parents when the leaf has not been created yet.
	if _, _, err := c.P.Inside(".glose"); err != nil {
		return "", err
	}
	dir, _, err := c.P.Inside(".glose/brouillons")
	return dir, err
}

// maxDraft is the largest draft file read back: a record is refused at write
// time if it would exceed it (JSON escapes « < » as six bytes), so that a
// draft confirmed to the author can always be read again.
const maxDraft = 1 << 20

func readDraft(path string) (draftReply, error) {
	var rep draftReply
	fi, err := os.Lstat(path)
	if err != nil {
		return rep, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxDraft {
		return rep, fmt.Errorf("brouillon non régulier ou trop volumineux")
	}
	d, err := store.Read(path)
	if err != nil {
		return rep, err
	}
	if err := json.Unmarshal(d.Bytes, &rep.draftRecord); err != nil {
		return rep, err
	}
	if rep.Version != 1 || !validDraftID(rep.ID) || (!rep.Deleted && rep.Draft == nil) {
		return rep, fmt.Errorf("format de brouillon inconnu ou incomplet")
	}
	rep.Hash = d.Hash
	return rep, nil
}

func (s *Server) getDrafts(w http.ResponseWriter, r *http.Request) {
	dir, err := s.draftsDir()
	if err != nil {
		jsonError(w, http.StatusConflict, "draft", err.Error())
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		jsonError(w, http.StatusInternalServerError, "read", err.Error())
		return
	}
	list := []draftReply{}
	problems := []string{}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if !strings.HasSuffix(e.Name(), ".json") || !validDraftID(id) {
			continue
		}
		rep, err := readDraft(filepath.Join(dir, e.Name()))
		if err != nil || rep.ID != id {
			problems = append(problems, "Brouillon "+id+" illisible : fichier conservé, sans écrasement.")
			continue
		}
		if !rep.Deleted {
			list = append(list, rep)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Updated > list[j].Updated })
	writeJSON(w, http.StatusOK, map[string]any{"drafts": list, "problems": problems})
}

func (s *Server) setDraft(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	var req draftRequest
	if !decode(w, r, &req) {
		return
	}
	if !validDraftID(req.ID) || (req.Remove && req.Draft != nil) || (!req.Remove && req.Draft == nil) {
		jsonError(w, http.StatusBadRequest, "draft", "brouillon invalide")
		return
	}
	if d := req.Draft; d != nil {
		_, clean, err := c.P.Inside(d.Path)
		validKind := d.Kind == "comment" || d.Kind == "reply" || d.Kind == "amend" || d.Kind == "edit"
		if err != nil || clean != d.Path || !validKind || len(d.Hash) != 64 || d.Start < 0 || d.End < d.Start ||
			d.At < 0 || d.To < 0 || d.Annotation < 0 || len(d.Path) > 4096 {
			jsonError(w, http.StatusBadRequest, "draft", "cible du brouillon invalide")
			return
		}
		// An untrusted record is only ever restored through the usual validated
		// manuscript routes, with the ORIGINAL hash; it cannot grant write access.
	}
	dir, err := s.draftsDir()
	if err != nil {
		jsonError(w, http.StatusConflict, "draft", err.Error())
		return
	}
	lock, err := c.N.Lock(5 * time.Second)
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, "lock", err.Error())
		return
	}
	defer lock.Unlock()
	path := filepath.Join(dir, req.ID+".json")
	prev, err := readDraft(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		jsonError(w, http.StatusConflict, "draft", "brouillon illisible : fichier conservé, sans écrasement")
		return
	}
	if !missing && prev.ID != req.ID {
		jsonError(w, http.StatusConflict, "draft", "identité du brouillon incohérente")
		return
	}
	// An identical retry is harmless, including a retry whose success response
	// was lost. A changed version from another tab is never overwritten.
	if !missing && prev.Deleted == req.Remove && reflect.DeepEqual(prev.Draft, req.Draft) {
		writeJSON(w, http.StatusOK, prev)
		return
	}
	if prev.Hash != req.Hash || prev.Deleted || (missing && req.Remove) {
		jsonError(w, http.StatusConflict, "conflict", "brouillon modifié ou supprimé ailleurs : votre saisie est conservée")
		return
	}
	record := draftRecord{Version: 1, ID: req.ID, Updated: time.Now().UTC().Format(time.RFC3339Nano), Deleted: req.Remove, Draft: req.Draft}
	data, err := json.Marshal(record)
	if err == nil && len(data) > maxDraft {
		jsonError(w, http.StatusRequestEntityTooLarge, "draft", "brouillon trop volumineux pour être conservé : votre saisie reste dans la page")
		return
	}
	if err == nil {
		if missing {
			err = store.CreateAtomic(path, data, 0o600)
		} else {
			_, err = store.WriteIfUnchanged(path, req.Hash, data)
		}
		// Written: only the flush of the directory failed. Refusing would
		// make the retry a conflict with its own write (D-33 promises no
		// survival of a power cut before confirmation).
		if errors.Is(err, store.ErrNotSynced) {
			err = nil
		}
	}
	if err != nil {
		jsonError(w, http.StatusConflict, "write", "brouillon non confirmé : "+err.Error())
		return
	}
	// A tombstone prevents a delayed creation request from resurrecting a
	// discarded draft. It contains no text of the deleted input.
	writeJSON(w, http.StatusOK, draftReply{draftRecord: record, Hash: store.Hash(data)})
}
