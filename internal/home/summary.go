package home

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// States of the reading of a project: a project that cannot be read is one
// line of the overview, never a general error.
const (
	StateOK        = "ok"
	StateMissing   = "missing"    // the directory is out of reach (a disk, a synchronised folder)
	StateNoProfile = "no-profile" // the directory has lost its glose.json
	StateProfile   = "profile"    // the project cannot be loaded (profile, files)
	StateJournal   = "journal"    // the journal is damaged, or cannot be read
)

// Turn is the last handover of a project.
type Turn struct {
	Time  string `json:"time"`
	Actor string `json:"actor"`
}

// Agent is what the overview shows of the agent of a project. It leaves out
// the code of its binding (navette.AgentState.Link), which is the author's.
type Agent struct {
	Name      string `json:"name"`
	Model     string `json:"model,omitempty"`
	Activity  string `json:"activity"`
	Seen      string `json:"seen"`
	Fresh     bool   `json:"fresh"`
	Listening bool   `json:"listening"`
	Taken     bool   `json:"taken"`
	Launched  bool   `json:"launched"`
}

// Server is the running server of a project: its address without any token
// (D-14), and its process.
type Server struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

// Progress is the last known progress of the review. The sections are the
// headings last rendered by the interface, kept file by file; those read are
// the ones the author marked (F2 quinquies, F29). Files is the number of files
// of the project, FilesSeen those that have been displayed: below Files, the
// total is partial, and said so.
type Progress struct {
	Read      int `json:"read"`
	Total     int `json:"total"`
	FilesSeen int `json:"filesSeen"`
	Files     int `json:"files"`
}

// Summary is the state of one project, read without any effect on it. The
// schema is documented in docs/SPEC.md (F28); keys are stable. Without a
// state « ok », what could not be read stays empty.
type Summary struct {
	ID      string `json:"id"`
	Root    string `json:"root"`
	Title   string `json:"title"`
	Opened  string `json:"opened"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
	// ProfileHash is the hash of glose.json, sent back to change the metadata.
	ProfileHash string `json:"profileHash,omitempty"`
	// What the author says of the project (D-51), with what was left out.
	Meta     project.Meta `json:"meta"`
	Warnings []string     `json:"warnings"`
	// What Glose computes.
	Hand         string    `json:"hand"`
	Round        int       `json:"round"`
	Annotations  int       `json:"annotations"`
	Issues       int       `json:"issues"`
	Problems     []string  `json:"problems"`
	LastHandover *Turn     `json:"lastHandover"`
	LastActivity string    `json:"lastActivity"`
	Agent        *Agent    `json:"agent"`
	Server       *Server   `json:"server"`
	Progress     *Progress `json:"progress"`
}

// Running tells whether a server runs for a state directory. It is given by
// the caller: the server package serves this one.
type Running func(stateDir string) (*Server, bool)

// Summarize reads one project. It writes nothing, takes no lock, leaves no sign
// of life and settles no pending operation (D-50): an operation found pending
// is only reported.
func Summarize(e Entry, running Running) Summary {
	s := Summary{ID: e.ID(), Root: e.Root, Title: filepath.Base(e.Root), Opened: e.Opened, LastActivity: e.Opened,
		State: StateOK, Warnings: []string{}, Problems: []string{}}
	fi, err := os.Stat(e.Root)
	if err != nil || !fi.IsDir() {
		s.State, s.Message = StateMissing, "dossier introuvable : "+e.Root
		return s
	}
	// Checked here: project.Load would look above, and take for this
	// project the one of a parent directory.
	if fi, err := os.Stat(filepath.Join(e.Root, project.ProfileName)); err != nil || fi.IsDir() {
		s.State, s.Message = StateNoProfile, "ce dossier n'a plus de "+project.ProfileName
		return s
	}
	if d, err := store.Read(filepath.Join(e.Root, project.ProfileName)); err == nil {
		s.ProfileHash = d.Hash
	}
	if running != nil {
		if srv, ok := running(filepath.Join(e.Root, project.StateDir)); ok {
			s.Server = srv
		}
	}
	p, err := project.Load(e.Root)
	if err != nil {
		s.State, s.Message = StateProfile, err.Error()
		return s
	}
	s.Title, s.Meta, s.Warnings = p.Title, p.Meta, append(s.Warnings, p.MetaWarnings...)
	st, h, _, err := navette.New(p).State()
	if err != nil {
		s.State, s.Message = StateJournal, "journal illisible : "+err.Error()
		return s
	}
	s.Hand, s.Round, s.Annotations, s.Issues, s.Problems = st.Hand, st.Round, st.Annotations, st.Issues, st.Problems
	if len(h.Problems) > 0 {
		s.State = StateJournal
		s.Message = fmt.Sprintf("journal abîmé, ligne %d : %s", h.Problems[0].Line, h.Problems[0].Message)
	}
	if st.Since != "" {
		s.LastHandover = &Turn{Time: st.Since, Actor: st.LastActor}
	}
	s.LastActivity = lastActivity(e.Opened, h.Events)
	if a := st.Agent; a != nil {
		s.Agent = &Agent{Name: a.Name, Model: a.Model, Activity: a.Activity, Seen: a.Seen, Fresh: a.Fresh, Listening: a.Listening, Taken: a.Taken, Launched: a.Launched}
	}
	s.Progress = readProgress(p)
	return s
}

// lastActivity is the most recent of the last opening and of the last event
// of the journal.
func lastActivity(opened string, events []journal.Event) string {
	if len(events) == 0 {
		return opened
	}
	last := events[len(events)-1].Time
	a, errA := time.Parse(time.RFC3339Nano, opened)
	b, errB := time.Parse(time.RFC3339Nano, last)
	if errB == nil && (errA != nil || b.After(a)) {
		return last
	}
	return opened
}

// readProgress gives the last known progress, or nil. A file that cannot be
// read stands for an unknown progress: the interface reports it.
func readProgress(p *project.Project) *Progress {
	b, err := os.ReadFile(filepath.Join(p.State(), project.ProgressFile))
	if err != nil {
		return nil
	}
	var saved struct {
		Sections map[string]struct {
			Status string `json:"status"`
		} `json:"sections"`
		Headings map[string][]string `json:"headings"`
	}
	if json.Unmarshal(b, &saved) != nil {
		return nil
	}
	out := Progress{Files: len(p.Files)}
	for _, f := range p.Files {
		list, ok := saved.Headings[f]
		if !ok {
			continue
		}
		out.FilesSeen++
		out.Total += len(list)
		for _, id := range list {
			if saved.Sections[f+"#"+id].Status == project.ProgressRead {
				out.Read++
			}
		}
	}
	if out.Total == 0 {
		return nil
	}
	return &out
}

// Summaries reads every project of entries, one after the other, and returns
// them in the default order of the overview (Sort). Measured on 8 November
// 2026 with 10 projects, among them a copy of a book of 316 000 words under
// /mnt/c: about 0.4 s, with or without reading them in parallel.
func Summaries(entries []Entry, running Running) []Summary {
	out := make([]Summary, len(entries))
	for i, e := range entries {
		out[i] = Summarize(e, running)
	}
	Sort(out)
	return out
}

// Sort puts the projects in the default order of the overview: the hand to the
// author first, then to the agent, then what cannot be read; within each, the
// nearest deadline first, those without one after; then by title.
func Sort(list []Summary) {
	rank := func(s Summary) int {
		switch {
		case s.Hand == review.Author:
			return 0
		case s.Hand != "":
			return 1
		}
		return 2
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.Meta.Deadline != b.Meta.Deadline {
			switch {
			case a.Meta.Deadline == "":
				return false
			case b.Meta.Deadline == "":
				return true
			}
			return a.Meta.Deadline < b.Meta.Deadline
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
}

// Totals are the indicators of the overview, computed from the projects that
// could be read.
type Totals struct {
	Projects    int `json:"projects"`
	Annotations int `json:"annotations"`
	InReview    int `json:"inReview"`
	AgentHand   int `json:"agentHand"`
	DueSoon     int `json:"dueSoon"` // a deadline within seven days, not past
	Overdue     int `json:"overdue"`
	Unreadable  int `json:"unreadable"`
}

// Compute gives the totals of list on the local day of now. A project « done »
// or « archived » has no deadline to come.
func Compute(list []Summary, now time.Time) Totals {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	t := Totals{Projects: len(list)}
	for _, s := range list {
		t.Annotations += s.Annotations
		if s.Meta.Status == project.StatusReview {
			t.InReview++
		}
		if s.Hand == review.Agent {
			t.AgentHand++
		}
		if s.State != StateOK {
			t.Unreadable++
		}
		if s.Meta.Status == project.StatusDone || s.Meta.Status == project.StatusArchived {
			continue
		}
		if d, err := time.Parse("2006-01-02", s.Meta.Deadline); err == nil {
			switch days := int(d.Sub(today).Hours() / 24); {
			case days < 0:
				t.Overdue++
			case days <= 7:
				t.DueSoon++
			}
		}
	}
	return t
}
