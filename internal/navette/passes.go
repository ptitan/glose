package navette

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/store"
)

// Passes of a turn of the agent with several recipients (P10.6, D-49): the
// annotations of the author are grouped by the agent that must answer them;
// Glose launches one agent per group, one after the other, each seeing only
// its annotations (glose list --to), and hands the turn back itself after
// the last one. A session bound to Glose (glose join) answers in its own
// name, at the same time. Not normative: .glose/passes.json says what
// is in progress; nothing is decided from it.
type Passes struct {
	// ID identifies the plan: an orchestration only changes or removes its
	// own plan, never the one of a later turn.
	ID     string `json:"id"`
	Turn   int    `json:"turn"`
	Passes []Pass `json:"passes"`
	// Linked is the name of a session bound to Glose that answers its own
	// annotations during the passes; LinkedDone once it ran glose done.
	Linked     string `json:"linked,omitempty"`
	LinkedDone bool   `json:"linkedDone,omitempty"`
	LinkedNote string `json:"linkedNote,omitempty"`
}

// Pass is one recipient of the turn.
type Pass struct {
	Alias string `json:"alias"`
	Agent string `json:"agent"`
	Model string `json:"model,omitempty"`
	Count int    `json:"count"` // annotations addressed to it
	// State: waiting, running, done (glose done), ended (stopped without
	// glose done), failed (not launched).
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
	Error string `json:"error,omitempty"`
	// IDs are the annotations given to this pass, fixed at the handover:
	// glose list --to and --mine follow them, even if glose set changes
	// meanwhile.
	IDs []string `json:"ids"`
}

// Assigned returns the pass of the plan that holds the annotation id.
func (p *Passes) Assigned(id string) (Pass, bool) {
	if p == nil || id == "" {
		return Pass{}, false
	}
	for _, x := range p.Passes {
		for _, y := range x.IDs {
			if y == id {
				return x, true
			}
		}
	}
	return Pass{}, false
}

// Find returns the pass of the short name alias.
func (p *Passes) Find(alias string) (Pass, bool) {
	if p == nil {
		return Pass{}, false
	}
	for _, x := range p.Passes {
		if strings.EqualFold(x.Alias, alias) {
			return x, true
		}
	}
	return Pass{}, false
}

// RemovePlan removes the plan id, and only it.
func RemovePlan(dir, id string) error {
	return UpdatePasses(dir, func(p *Passes) *Passes {
		if p != nil && p.ID == id {
			return nil
		}
		return p
	})
}

// States of a pass.
const (
	PassWaiting = "waiting"
	PassRunning = "running"
	PassDone    = "done"
	PassEnded   = "ended"
	PassFailed  = "failed"
)

// PassesFile is the file of the passes, relative to the state directory.
const PassesFile = "passes.json"

// PassEnv is set by Glose in the environment of an agent launched for a
// pass: « glose done » then closes the pass and does not hand over.
const PassEnv = "GLOSE_PASS"

// ErrNoPass means that no pass of this turn waits for this agent.
var ErrNoPass = errors.New("aucun passage en cours pour cet agent")

func lockPasses(dir string) (*store.Lock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.WaitLockNamed(ctx, dir, "passes.lock")
}

// ReadPasses returns the passes in progress, or nil.
func ReadPasses(dir string) *Passes {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(PassesFile)))
	if err != nil {
		return nil
	}
	var p Passes
	if json.Unmarshal(b, &p) != nil || p.Turn <= 0 {
		return nil
	}
	return &p
}

// WritePasses records the passes; nil removes them.
func WritePasses(dir string, p *Passes) error {
	l, err := lockPasses(dir)
	if err != nil {
		return err
	}
	defer l.Unlock()
	return writePasses(dir, p)
}

func writePasses(dir string, p *Passes) error {
	path := filepath.Join(dir, filepath.FromSlash(PassesFile))
	if p == nil {
		return store.Remove(path)
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := store.WriteAtomic(path, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return err
	}
	return nil
}

// UpdatePasses changes the passes under their lock; f receives nil if there
// are none, and returns what to write (nil: remove).
func UpdatePasses(dir string, f func(*Passes) *Passes) error {
	l, err := lockPasses(dir)
	if err != nil {
		return err
	}
	defer l.Unlock()
	return writePasses(dir, f(ReadPasses(dir)))
}

// ClosePass records that the agent name ran « glose done » during the turn
// turn: its pass (launched by Glose, pass is its short name) or the part of
// the bound session. It returns ErrNoPass if no pass of this turn waits for
// it: « glose done » then hands over as usual.
func ClosePass(dir string, turn int, pass, name, note string) error {
	found := false
	err := UpdatePasses(dir, func(p *Passes) *Passes {
		if p == nil || p.Turn != turn {
			return p
		}
		for i := range p.Passes {
			if pass != "" && strings.EqualFold(p.Passes[i].Alias, pass) && p.Passes[i].State == PassRunning {
				p.Passes[i].State, p.Passes[i].Note = PassDone, note
				found = true
				return p
			}
		}
		if pass == "" && p.Linked != "" && p.Linked == name && !p.LinkedDone {
			p.LinkedDone, p.LinkedNote = true, note
			found = true
		}
		return p
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrNoPass
	}
	return nil
}
