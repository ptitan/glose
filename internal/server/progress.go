package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/store"
)

// The progress of the review (SPEC F2 quinquies): the author marks each
// section of the summary « en cours » or « relue », to keep track of her
// reading. It is a note of the author for herself, neither the text nor the
// exchange: it lives in .glose/progression.json, not in the Markdown nor in
// the journal, and may be written whoever has the hand.

// Section statuses.
const (
	ProgressReading = "en-cours"
	ProgressDone    = "relue"
)

// ProgressEntry is the status of one section.
type ProgressEntry struct {
	Status string `json:"status"`
	Title  string `json:"title,omitempty"` // to recognise the section if its key is lost
	Time   string `json:"time"`
}

// Progress is the content of .glose/progression.json.
type Progress struct {
	Version  int                      `json:"version"`
	Sections map[string]ProgressEntry `json:"sections"`
}

// progressBeforeWrite is called between the reading and the writing of the
// progress; tests set it to simulate a change made by another program.
var progressBeforeWrite func()

func (s *Server) progressPath() string { return filepath.Join(s.cur().P.State(), "progression.json") }

// loadProgress reads the progress. A missing file is an empty progress; an
// unreadable one is an error, and is never overwritten: it may hold the
// statuses of many sections, edited by hand.
//
// It also returns the hash of the file read ("" if there is none), so that
// a write never replaces a version changed on disk meanwhile.
func (s *Server) loadProgress() (Progress, string, error) {
	pr := Progress{Version: 1, Sections: map[string]ProgressEntry{}}
	d, err := store.Read(s.progressPath())
	if errors.Is(err, os.ErrNotExist) {
		return pr, "", nil
	}
	if err != nil {
		return pr, "", err
	}
	var saved Progress
	if err := json.Unmarshal(d.Bytes, &saved); err != nil {
		return pr, d.Hash, fmt.Errorf("%s illisible (%v) : corrigez-le ou supprimez-le", filepath.Join(".glose", "progression.json"), err)
	}
	if saved.Sections == nil {
		saved.Sections = map[string]ProgressEntry{}
	}
	saved.Version = 1
	return saved, d.Hash, nil
}

// progressReply is the progress, with the reason why it is empty if the
// file cannot be read.
type progressReply struct {
	Progress
	Error string `json:"error,omitempty"`
}

func (s *Server) getProgress(w http.ResponseWriter, r *http.Request) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	pr, _, err := s.loadProgress()
	rep := progressReply{Progress: pr}
	if err != nil {
		rep.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, rep)
}

type progressRequest struct {
	Key    string `json:"key"`    // file path, « # », identifier of the title
	Title  string `json:"title"`  // text of the title
	Status string `json:"status"` // "", en-cours or relue
}

func (s *Server) setProgress(w http.ResponseWriter, r *http.Request) {
	var req progressRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Key == "" || len(req.Key) > 1000 || len(req.Title) > 1000 || !utf8.ValidString(req.Key) || !utf8.ValidString(req.Title) ||
		(req.Status != "" && req.Status != ProgressReading && req.Status != ProgressDone) {
		jsonError(w, http.StatusBadRequest, "progress", "section ou état invalide")
		return
	}
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	pr, hash, err := s.loadProgress()
	if err != nil {
		jsonError(w, http.StatusConflict, "progress", err.Error())
		return
	}
	if req.Status == "" {
		delete(pr.Sections, req.Key)
	} else {
		pr.Sections[req.Key] = ProgressEntry{Status: req.Status, Title: strings.TrimSpace(req.Title),
			Time: time.Now().UTC().Format(time.RFC3339)}
	}
	data, _ := json.MarshalIndent(pr, "", "  ")
	if progressBeforeWrite != nil {
		progressBeforeWrite()
	}
	if hash == "" {
		err = store.CreateAtomic(s.progressPath(), data, 0o600)
	} else {
		_, err = store.WriteIfUnchanged(s.progressPath(), hash, data)
	}
	if errors.Is(err, store.ErrNotSynced) {
		err = nil // écrit ; seule la synchronisation du dossier a échoué
	}
	if errors.Is(err, store.ErrConflict) {
		jsonError(w, http.StatusConflict, "conflict", "progression.json a changé sur le disque : rechargez la page, puis recommencez")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pr)
}
