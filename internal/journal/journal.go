// Package journal keeps the history of a project (D-13): one JSON event per
// line in .glose/journal.jsonl, always appended. The Markdown files hold the
// present (text and active annotations); the journal holds what happened:
// turns, operations, and a copy of every annotation closed by a decision.
//
// A change of the files follows a two-step protocol (SPEC N3):
//
//  1. a "prepared" event describes the operation (files with their hash
//     before and after, annotations created or closed), and is synced;
//  2. the files are written, each under the condition of its hash;
//  3. a "committed" event confirms the operation, and is synced.
//
// If the process dies in between, Reconcile finds the operation prepared and
// never confirmed, and looks at the files: all of them as after → committed;
// all of them as before → aborted; anything else → aborted and reported. An
// operation counts only once committed.
//
// The journal has a single writer at a time: the holder of the project lock
// (package store). A last line without its line ending (an interrupted
// append) is reported, never ignored: the next append starts a new line and
// records a "repair" event that names it.
package journal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ptitan/glose/internal/store"
)

// Name is the file name of the journal in the state directory.
const Name = "journal.jsonl"

// Event types.
const (
	Prepared  = "prepared"
	Committed = "committed"
	Aborted   = "aborted"
	Turn      = "turn"
	Repair    = "repair"
)

// Event is one line of the journal.
type Event struct {
	Seq    int64     `json:"seq"`
	Time   string    `json:"time"`
	Type   string    `json:"type"`
	Actor  string    `json:"actor,omitempty"` // author, agent, unknown
	OpID   string    `json:"op_id,omitempty"` // committed, aborted: the operation
	Op     *Op       `json:"op,omitempty"`    // prepared
	TurnEv *TurnInfo `json:"turn,omitempty"`
	Reason string    `json:"reason,omitempty"` // aborted, repair
	// Partial marks an operation aborted at the restart while some of its
	// files were written (or changed since): the author has to look at them.
	Partial bool     `json:"partial,omitempty"`
	Files   []string `json:"files,omitempty"` // partial: the files concerned
}

// Op describes an operation on the files.
type Op struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"` // comment, reply, propose, decide, withdraw, identify, track…
	Files   []FileState `json:"files"`
	Created []string    `json:"created,omitempty"` // identifiers of the annotations and messages created
	Closed  []Closed    `json:"closed,omitempty"`  // annotations closed, with a copy
	// Revised holds the previous version of the proposals corrected by the
	// operation (P3.2): same identity, another proposed text.
	Revised []Closed `json:"revised,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// FileState is the hash of a file before and after an operation (paths are
// relative to the project, with forward slashes).
type FileState struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Closed is the copy of an annotation closed by a decision (SPEC F11).
type Closed struct {
	ID       string    `json:"id"`
	File     string    `json:"file"`
	Decision string    `json:"decision"` // accept, reject, resolve, withdraw (revise: a previous version)
	Kind     string    `json:"kind"`
	By       string    `json:"by,omitempty"`
	Text     string    `json:"text"`
	New      string    `json:"new,omitempty"`
	Messages []Message `json:"messages,omitempty"`
}

// Message is a message of a closed thread.
type Message struct {
	ID   string `json:"id,omitempty"`
	By   string `json:"by,omitempty"`
	Text string `json:"text"`
}

// TurnInfo is the content of a turn event: an actor hands the text over.
type TurnInfo struct {
	Number    int            `json:"number"` // the turn that ends, from 1
	Note      string         `json:"note,omitempty"`
	Files     []FileHash     `json:"files"`         // the files handed over
	IDs       []string       `json:"ids,omitempty"` // annotations active at the handover
	Counts    map[string]int `json:"counts,omitempty"`
	Reclaimed bool           `json:"reclaimed,omitempty"` // the author took the hand back
	// Existing is the inventory of the Markdown files (and of the files of
	// the scope) that existed at the handover; Complete says that it is
	// complete (D-38). A file of a later scope absent from Existing was
	// created after the handover; without Complete, nothing can be said.
	Existing []string `json:"existing,omitempty"`
	Complete bool     `json:"complete,omitempty"`
}

// FileHash is a file and its hash.
type FileHash struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// Problem is a line of the journal that cannot be read.
type Problem struct {
	Line    int    `json:"line"`
	Offset  int    `json:"offset"`
	Message string `json:"message"`
}

// Journal is the journal of a project.
type Journal struct {
	Dir  string // state directory (.glose)
	now  func() time.Time
	sync func(*os.File) error
}

// Open returns the journal of the state directory dir; the file is created by
// the first append.
func Open(dir string) *Journal {
	return &Journal{Dir: dir, now: time.Now, sync: func(f *os.File) error { return f.Sync() }}
}

// Path is the path of the journal file.
func (j *Journal) Path() string { return filepath.Join(j.Dir, Name) }

// Read returns the events of the journal in order, and the lines that cannot
// be read. A missing journal is empty.
func (j *Journal) Read() ([]Event, []Problem, error) {
	b, err := os.ReadFile(j.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return parse(b)
}

func parse(b []byte) ([]Event, []Problem, error) {
	var events []Event
	var problems []Problem
	off := 0
	for n := 1; off < len(b); n++ {
		i := bytes.IndexByte(b[off:], '\n')
		line := b[off:]
		if i >= 0 {
			line = b[off : off+i]
		}
		if i < 0 {
			// An append interrupted just before its line ending: the event
			// is whole and counts (Append then only adds the line ending).
			// Anything else is a truncated line, reported.
			var e Event
			if err := json.Unmarshal(line, &e); err == nil && e.Type != "" {
				events = append(events, e)
			} else {
				problems = append(problems, Problem{n, off, "dernière ligne tronquée (écriture interrompue)"})
			}
			break
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var e Event
			if err := json.Unmarshal(line, &e); err != nil || e.Type == "" {
				problems = append(problems, Problem{n, off, "ligne illisible"})
			} else {
				events = append(events, e)
			}
		}
		off += i + 1
	}
	return events, problems, nil
}

// failpoint lets tests interrupt the protocol at a given step.
var failpoint func(step string) error

func fail(step string) error {
	if failpoint != nil {
		return failpoint(step)
	}
	return nil
}

// Append adds events at the end of the journal, with their sequence number
// and time, and syncs the file. It must be called by the holder of the
// project lock.
func (j *Journal) Append(evs ...Event) ([]Event, error) {
	if err := os.MkdirAll(j.Dir, 0o700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(j.Path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	events, _, _ := parse(b)
	seq := int64(0)
	if len(events) > 0 {
		seq = events[len(events)-1].Seq
	}
	var buf bytes.Buffer
	if len(b) > 0 && b[len(b)-1] != '\n' {
		start := bytes.LastIndexByte(b, '\n') + 1
		buf.WriteByte('\n')
		var whole Event
		if err := json.Unmarshal(b[start:], &whole); err == nil && whole.Type != "" {
			goto events // a whole event without its line ending: already counted by parse
		}
		seq++
		rep := Event{Seq: seq, Time: j.stamp(), Type: Repair,
			Reason: fmt.Sprintf("ligne tronquée à l'octet %d (%d octets) : écriture interrompue, gardée telle quelle", start, len(b)-start)}
		line, _ := json.Marshal(rep)
		buf.Write(line)
		buf.WriteByte('\n')
	}
events:
	out := make([]Event, len(evs))
	for i, e := range evs {
		seq++
		e.Seq, e.Time = seq, j.stamp()
		line, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
		out[i] = e
	}
	f, err := store.OpenJournal(j.Path())
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return nil, err
	}
	if err := j.sync(f); err != nil {
		f.Close()
		return nil, err
	}
	return out, f.Close()
}

func (j *Journal) stamp() string { return j.now().UTC().Format(time.RFC3339Nano) }

// NewOpID returns a fresh operation identifier.
func NewOpID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Change is the change of one file within an operation.
type Change struct {
	Path   string // relative to the project
	Abs    string // absolute path
	Before []byte // content read
	After  []byte // content to write
}

// ErrPartial means that an operation failed after writing some of its files,
// and that they could not all be restored.
var ErrPartial = errors.New("opération interrompue : des fichiers n'ont pas pu être remis dans leur état d'avant")

// Commit runs the protocol for op: prepared, writes, committed. On failure,
// the files already written are restored and the operation is aborted. It
// returns the events written. It must be called by the holder of the project
// lock.
func (j *Journal) Commit(actor string, op Op, changes []Change) ([]Event, error) {
	if op.ID == "" {
		op.ID = NewOpID()
	}
	op.Files = op.Files[:0]
	for _, c := range changes {
		op.Files = append(op.Files, FileState{Path: c.Path, Before: store.Hash(c.Before), After: store.Hash(c.After)})
	}
	prep, err := j.Append(Event{Type: Prepared, Actor: actor, Op: &op})
	if err != nil {
		return nil, err
	}
	if err := fail("prepared"); err != nil {
		return prep, err
	}
	var written []Change
	var werr error
	for _, c := range changes {
		if bytes.Equal(c.Before, c.After) {
			continue
		}
		if _, err := store.WriteIfUnchanged(c.Abs, store.Hash(c.Before), c.After); err != nil {
			werr = fmt.Errorf("%s : %w", c.Path, err)
			// Written by Glose, but not synced: restored with the others.
			// (A file that another program changed is never touched.)
			if errors.Is(err, store.ErrNotSynced) {
				written = append(written, c)
			}
			break
		}
		written = append(written, c)
		if err := fail("written:" + c.Path); err != nil {
			return prep, err
		}
	}
	if werr != nil {
		reason := werr.Error()
		var rerr error
		var unsynced []string
		for i := len(written) - 1; i >= 0; i-- {
			c := written[i]
			_, err := store.WriteIfUnchanged(c.Abs, store.Hash(c.After), c.Before)
			if errors.Is(err, store.ErrNotSynced) {
				// Restored, but its directory was not flushed: try once more
				// (P5.4, D-37); a power cut could otherwise bring the
				// operation's content back after it was recorded as aborted.
				dir := filepath.Dir(c.Abs)
				if real, rerr := filepath.EvalSymlinks(c.Abs); rerr == nil {
					dir = filepath.Dir(real)
				}
				if store.SyncDir(dir) == nil {
					err = nil
				} else {
					unsynced = append(unsynced, c.Path)
					err = nil
				}
			}
			if err != nil {
				rerr = ErrPartial
				reason += " ; " + c.Path + " non restauré : " + err.Error()
			}
		}
		ev := Event{Type: Aborted, Actor: actor, OpID: op.ID, Reason: reason}
		if rerr == nil && len(unsynced) > 0 {
			// The files are back in their state before, but perhaps not on
			// the disk: signalled until the next handover, never hidden.
			ev.Partial = true
			ev.Files = unsynced
			ev.Reason = "opération " + op.ID + " (" + op.Kind + ") abandonnée ; fichiers restaurés mais non synchronisés sur le disque : après une coupure de courant, vérifiez-les ; " + reason
		}
		if rerr != nil {
			// Some files keep part of the operation: the author has to look.
			ev.Partial = true
			ev.Reason = "opération " + op.ID + " (" + op.Kind + ") interrompue à moitié, fichiers non restaurés : à vérifier ; " + reason
			for _, c := range written {
				ev.Files = append(ev.Files, c.Path)
			}
		}
		ab, err := j.Append(ev)
		if err != nil {
			return prep, err
		}
		if rerr != nil {
			return append(prep, ab...), fmt.Errorf("%w (%s)", rerr, reason)
		}
		return append(prep, ab...), werr
	}
	if err := fail("before-commit"); err != nil {
		return prep, err
	}
	com, err := j.Append(Event{Type: Committed, Actor: actor, OpID: op.ID})
	if err != nil {
		return prep, err
	}
	return append(prep, com...), nil
}

// Pending returns the operations prepared and neither committed nor aborted.
func Pending(events []Event) []Op {
	done := map[string]bool{}
	for _, e := range events {
		if e.Type == Committed || e.Type == Aborted {
			done[e.OpID] = true
		}
	}
	var out []Op
	for _, e := range events {
		if e.Type == Prepared && e.Op != nil && !done[e.Op.ID] {
			out = append(out, *e.Op)
		}
	}
	return out
}

// Reconcile settles the operations left pending by an interruption, by
// looking at the files (abs gives the absolute path of a project path). It
// returns the events written and the anomalies found (an operation half
// done: its files are left as they are, for the author to see).
func (j *Journal) Reconcile(abs func(string) string) ([]Event, []string, error) {
	events, _, err := j.Read()
	if err != nil {
		return nil, nil, err
	}
	var written []Event
	var anomalies []string
	for _, op := range Pending(events) {
		after, before := 0, 0
		for _, f := range op.Files {
			d, err := store.Read(abs(f.Path))
			h := ""
			if err == nil {
				h = d.Hash
			}
			switch h {
			case f.After:
				after++
			case f.Before:
				before++
			}
		}
		var ev Event
		switch {
		case after == len(op.Files):
			ev = Event{Type: Committed, OpID: op.ID, Reason: "confirmée à la reprise : les fichiers étaient écrits"}
		case before == len(op.Files):
			ev = Event{Type: Aborted, OpID: op.ID, Reason: "abandonnée à la reprise : aucun fichier n'avait été écrit"}
		default:
			msg := fmt.Sprintf("opération %s (%s) interrompue à moitié ou fichiers modifiés depuis : à vérifier", op.ID, op.Kind)
			anomalies = append(anomalies, msg)
			ev = Event{Type: Aborted, OpID: op.ID, Reason: msg, Partial: true}
			for _, f := range op.Files {
				ev.Files = append(ev.Files, f.Path)
			}
		}
		w, err := j.Append(ev)
		if err != nil {
			return written, anomalies, err
		}
		written = append(written, w...)
	}
	return written, anomalies, nil
}

// Effective returns the operations committed, in order, with the sequence
// number of their confirmation.
func Effective(events []Event) []EffectiveOp {
	ops := map[string]*Op{}
	actors := map[string]string{} // the actor of the first preparation of each operation
	var out []EffectiveOp
	for _, e := range events {
		switch e.Type {
		case Prepared:
			if e.Op != nil {
				ops[e.Op.ID] = e.Op
				if _, seen := actors[e.Op.ID]; !seen {
					actors[e.Op.ID] = e.Actor
				}
			}
		case Committed:
			if op, ok := ops[e.OpID]; ok {
				out = append(out, EffectiveOp{Op: *op, Seq: e.Seq, Time: e.Time, Actor: actors[op.ID]})
			}
		}
	}
	return out
}

// EffectiveOp is an operation committed.
type EffectiveOp struct {
	Op
	Seq   int64
	Time  string
	Actor string
}

// Turns returns the turn events, in order.
func Turns(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Type == Turn && e.TurnEv != nil {
			out = append(out, e)
		}
	}
	return out
}

// SnapshotDir is the directory of the copies of the files handed over to the
// agent (D-25), named by their hash.
const SnapshotDir = "snapshots"

// SaveSnapshot keeps a copy of content in the state directory dir and returns
// its hash. A copy already kept is not written again.
func SaveSnapshot(dir string, content []byte) (string, error) {
	h := store.Hash(content)
	path := filepath.Join(dir, SnapshotDir, h)
	if d, err := store.Read(path); err == nil && d.Hash == h {
		return h, nil
	}
	return h, store.WriteAtomic(path, content, 0o600)
}

// LoadSnapshot returns the copy of hash h, checked against its hash.
func LoadSnapshot(dir, h string) ([]byte, error) {
	d, err := store.Read(filepath.Join(dir, SnapshotDir, h))
	if err != nil {
		return nil, fmt.Errorf("copie %s introuvable : %w", h, err)
	}
	if d.Hash != h {
		return nil, fmt.Errorf("copie %s altérée", h)
	}
	return d.Bytes, nil
}

// Partials returns the operations aborted at a restart while half done.
func Partials(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Type == Aborted && e.Partial {
			out = append(out, e)
		}
	}
	return out
}
