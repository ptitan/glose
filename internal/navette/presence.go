package navette

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/proc"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// Presence of the agent (P10.1): who listens to the exchange, and what he
// did last. It lives in .glose/agent.json and is not normative (invariant 4):
// nothing is decided from it, it may be lost without harm, and the last
// writer wins. A file that cannot be read stands for nobody.
type Presence struct {
	Progress   string `json:"progress,omitempty"`
	ProgressAt string `json:"progressAt,omitempty"`
	Name       string `json:"name"`
	PID        int    `json:"pid,omitempty"`      // set when Glose launched the agent (P10.5)
	Since      string `json:"since"`              // first sign of this name
	Seen       string `json:"seen"`               // last sign
	Activity   string `json:"activity"`           // wait, read, write, done, joined
	Turn       int    `json:"turn"`               // turn of the journal at the last sign
	Launched   bool   `json:"launched,omitempty"` // launched by Glose (P10.5)
	// Link is the code of « glose join » that bound this agent (P10.2):
	// the panel « Lier un agent » says « lié » only for its own code.
	Link string `json:"link,omitempty"`
	// Identity of the launched process (its start time, where the system
	// gives it) and Deadline of its run (P10.5): a server restarted during
	// the run finds it again, and never takes for it another process that
	// got the same number.
	Identity string `json:"identity,omitempty"`
	Deadline string `json:"deadline,omitempty"`
	// Model of the agent (P10.11), and where it comes from: « glose » (an
	// agent launched by Glose, from glose set), « agent » (read in the
	// agent, e.g. its OpenCode session) or « declared » (glose join --model).
	Model       string `json:"model,omitempty"`
	ModelSource string `json:"modelSource,omitempty"`
}

// Sources of the model of an agent.
const (
	ModelByGlose  = "glose"
	ModelByAgent  = "agent"
	ModelDeclared = "declared"
)

// Activities of the agent.
const (
	ActWait   = "wait"   // glose wait: listening
	ActRead   = "read"   // status, list, or wait that returned: has the hand
	ActWrite  = "write"  // reply, propose, comment
	ActDone   = "done"   // glose done: the hand went back to the author
	ActJoined = "joined" // glose join: bound to a server opened by hand (P10.2)
	// ActLaunched: launched by Glose at the handover (P10.5).
	ActLaunched = "launched"
)

// PresenceFile is the name of the presence file in the state directory.
const PresenceFile = "agent.json"

// Freshness of a presence: an agent who waits renews his sign every 10 s
// (WaitBeat), so that 60 s without a sign means nobody listens. During the
// agent's turn, an agent may think or edit files for a long while without
// running glose: once he has taken the hand, his sign stays fresh longer.
const (
	FreshWaiting = 60 * time.Second
	FreshWorking = 30 * time.Minute
	WaitBeat     = 10 * time.Second
)

// ErrAgentName is returned for a name that is not 1 to 40 printable
// characters on one line.
var ErrAgentName = errors.New("nom d'agent invalide : 1 à 40 caractères imprimables, sur une ligne")

// CheckAgentName validates the name of an agent.
func CheckAgentName(name string) error {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 40 || strings.TrimSpace(name) != name {
		return ErrAgentName
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return ErrAgentName
		}
	}
	return nil
}

// ReadPresence returns the presence written in the state directory dir, or
// nil if there is none or it cannot be read.
func ReadPresence(dir string) *Presence {
	b, err := os.ReadFile(filepath.Join(dir, PresenceFile))
	if err != nil {
		return nil
	}
	var p Presence
	if json.Unmarshal(b, &p) != nil || CheckAgentName(p.Name) != nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, p.Seen); err != nil {
		return nil
	}
	return &p
}

// Touch records a sign of the agent name: activity at turn. The first sign
// keeps its date while the name does not change. A launched agent keeps its
// process number. Errors are returned but the callers ignore them: a lost
// sign of life never stops a command.
func Touch(dir, name, activity string, turn int) error {
	return touch(dir, name, activity, turn, "", "", "", nil)
}

// TouchJoined records that the agent name was bound by the code link
// (glose join), with its model and where it comes from (P10.11), if known:
// in one write.
func TouchJoined(dir, name string, turn int, link, model, source string) error {
	return touch(dir, name, ActJoined, turn, normalCode(link), model, source, nil)
}

// TouchBound renews the sign of a bound session with its binding and its
// model (audit of P10.12): a presence replaced meanwhile (an agent launched
// by Glose) gets them back as soon as it is free.
func TouchBound(dir, name, activity string, turn int, link, model, source string) error {
	return touch(dir, name, activity, turn, normalCode(link), model, source, nil)
}

// lockPresence serialises the reading and writing of the presence, across
// processes: a sign never erases a launch written meanwhile.
func lockPresence(dir string) (*store.Lock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.WaitLockNamed(ctx, dir, "agent.lock")
}

// TouchLaunched records that Glose launched the agent name, process pid
// (P10.5): a server restarted meanwhile finds it again.
func TouchLaunched(dir, name string, turn, pid int, identity string, deadline time.Time, model string) error {
	if err := CheckAgentName(name); err != nil {
		return err
	}
	lk, err := lockPresence(dir)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p := Presence{Name: name, Since: now, Seen: now, Activity: ActLaunched, Turn: turn, PID: pid, Launched: true,
		Identity: identity, Deadline: deadline.UTC().Format(time.RFC3339Nano)}
	if model != "" {
		p.Model, p.ModelSource = model, ModelByGlose
	}
	return writePresence(dir, p)
}

// ClearLaunched records that the process pid launched by Glose has ended:
// its number may be reused.
func ClearLaunched(dir string, pid int) error {
	lk, err := lockPresence(dir)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	p := ReadPresence(dir)
	if p == nil || !p.Launched || p.PID != pid {
		return nil
	}
	p.Launched, p.PID, p.Identity, p.Deadline = false, 0, "", ""
	return writePresence(dir, *p)
}

// ErrLaunchedAgent means that an agent launched by Glose works: the sign of
// another name is not recorded (P10.8), so that its process, identity and
// deadline stay known.
var ErrLaunchedAgent = errors.New("un agent lancé par Glose travaille")

// LaunchedAgent returns the presence of the agent launched by Glose if its
// process still runs, else nil. With an identity, the same process is kept
// until it is gone, past its deadline too (the watch stops it then, and needs
// its number); without one (a system that gives none), the deadline bounds
// it, since its number may have been reused.
func LaunchedAgent(dir string) *Presence {
	p := ReadPresence(dir)
	if p == nil || !p.Launched || p.PID <= 0 || !proc.Alive(p.PID) {
		return nil
	}
	if p.Identity != "" {
		if proc.Identity(p.PID) != p.Identity {
			return nil
		}
		return p
	}
	if d, err := time.Parse(time.RFC3339Nano, p.Deadline); err == nil && time.Now().After(d) {
		return nil
	}
	return p
}

func touch(dir, name, activity string, turn int, link, model, source string, progress *string) error {
	if err := CheckAgentName(name); err != nil {
		return err
	}
	lk, err := lockPresence(dir)
	if err != nil {
		return err
	}
	defer lk.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p := Presence{Name: name, Since: now, Seen: now, Activity: activity, Turn: turn, Link: link, Model: model, ModelSource: source}
	old := ReadPresence(dir)
	if old != nil && old.Name != name && LaunchedAgent(dir) != nil {
		return ErrLaunchedAgent
	}
	if old != nil && old.Name == name {
		p.Since, p.PID, p.Launched, p.Identity, p.Deadline = old.Since, old.PID, old.Launched, old.Identity, old.Deadline
		if old.Turn == turn && activity != ActDone && activity != ActWait {
			p.Progress, p.ProgressAt = old.Progress, old.ProgressAt
		}
		if model == "" {
			p.Model, p.ModelSource = old.Model, old.ModelSource
		}
		if link == "" {
			p.Link = old.Link
		}
	}
	if progress != nil {
		p.Progress, p.ProgressAt = *progress, now
	}
	return writePresence(dir, p)
}

func writePresence(dir string, p Presence) error {
	data, _ := json.MarshalIndent(p, "", "  ")
	err := store.WriteAtomic(filepath.Join(dir, PresenceFile), data, 0o600)
	if errors.Is(err, store.ErrNotSynced) {
		err = nil // non normatif : la synchronisation du dossier importe peu
	}
	if err != nil {
		return fmt.Errorf("présence de l'agent non écrite : %w", err)
	}
	return nil
}

// AgentState is what the interface shows of the agent (State.Agent).
type AgentState struct {
	Progress   string `json:"progress,omitempty"`
	ProgressAt string `json:"progressAt,omitempty"`
	Name       string `json:"name"`
	Activity   string `json:"activity"`
	Seen       string `json:"seen"`
	// Fresh: a recent sign (see FreshWaiting and FreshWorking).
	Fresh bool `json:"fresh"`
	// Taken: during the agent's turn, a sign after the handover: the agent
	// has taken the hand.
	Taken bool `json:"taken"`
	// Listening: during the author's turn, a fresh sign of an agent that
	// waits (glose wait, or just bound by glose join). An agent whose last
	// sign is « done » or a reading does not listen: it may be gone.
	Listening bool   `json:"listening"`
	Launched  bool   `json:"launched,omitempty"`
	Link      string `json:"link,omitempty"`
	// Model of the agent and where it comes from (P10.11).
	Model       string `json:"model,omitempty"`
	ModelSource string `json:"modelSource,omitempty"`
}

// agentState computes the state of the agent from its presence p, the hand
// and the time of the last handover (empty before the first one).
func agentState(p *Presence, hand, since string, now time.Time) *AgentState {
	if p == nil {
		return nil
	}
	seen, err := time.Parse(time.RFC3339Nano, p.Seen)
	if err != nil {
		return nil
	}
	a := &AgentState{Name: p.Name, Activity: p.Activity, Seen: p.Seen, Launched: p.Launched, Link: p.Link, Model: p.Model, ModelSource: p.ModelSource}
	if hand == review.Agent {
		if t, err := time.Parse(time.RFC3339Nano, since); err == nil && !seen.Before(t) && p.Activity != ActWait && p.Activity != ActDone {
			a.Taken = true
		}
	}
	limit := FreshWaiting
	if a.Taken {
		limit = FreshWorking
	}
	age := now.Sub(seen)
	a.Fresh = age < limit && age > -time.Minute
	if a.Taken && a.Fresh {
		at, progressErr := time.Parse(time.RFC3339Nano, p.ProgressAt)
		handed, handErr := time.Parse(time.RFC3339Nano, since)
		if progressErr == nil && handErr == nil && !at.Before(handed) {
			a.Progress, a.ProgressAt = p.Progress, p.ProgressAt
		}
	}
	a.Listening = a.Fresh && (p.Activity == ActWait || p.Activity == ActJoined)
	return a
}

// Agent returns the state of the agent of the project, computed now, for
// the hand and the time of the last handover.
func (n *Navette) Agent(hand, since string) *AgentState {
	return agentState(ReadPresence(n.P.State()), hand, since, time.Now())
}

// CheckProgress accepts one printable sentence, up to 120 Unicode characters.
func CheckProgress(text string) error {
	if text == "" || strings.TrimSpace(text) != text || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 120 {
		return fmt.Errorf("avancement : 1 à 120 caractères imprimables sur une ligne")
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("avancement : 1 à 120 caractères imprimables sur une ligne")
		}
	}
	return nil
}

// Progress only updates the non-normative presence, through store.
func Progress(dir, name string, turn int, text string) error {
	if err := CheckProgress(text); err != nil {
		return err
	}
	return touch(dir, name, ActRead, turn, "", "", "", &text)
}
