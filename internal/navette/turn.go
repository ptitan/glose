package navette

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// lockWait is how long a handover waits for the lock of the project.
const lockWait = 30 * time.Second

// Handover is the result of a handover.
type Handover struct {
	Event      journal.Event // the turn event
	Identified int           // annotations and messages that received their identity
	Converted  int           // proposals made from the free changes of the agent (D-25)
	Files      int           // files written
}

// ErrFiles is returned when a file of the project cannot be read.
var ErrFiles = errors.New("un fichier du projet est illisible")

// Handoff ends the turn of actor: the annotations and messages without
// identity receive one, attributed to actor (D-19, D-20); for the agent, its
// free changes become proposals first (D-25, see track.go). A copy of every
// file is kept (D-25), and the turn event records the files, their hashes and
// the active annotations.
func (n *Navette) Handoff(actor, note string) (*Handover, error) {
	return n.handover(actor, actor, note, false, false, 0, nil)
}

// HandoffThen is Handoff, with then called under the lock of the project
// once every check has passed, just before the turn event, with the number
// of the turn that begins: an error cancels the handover; the function it
// returns undoes its writes if the event cannot be written. It records what
// must hold from the very moment the hand changes (the owner of the agent's
// turn, a plan of passes, P10.6 and P10.9).
func (n *Navette) HandoffThen(actor, note string, then func(next int) (undo func(), err error)) (*Handover, error) {
	return n.handover(actor, actor, note, false, false, 0, then)
}

// ErrTurnChanged means that the turn expected by HandoffTurn is over.
var ErrTurnChanged = errors.New("le tour attendu est terminé")

// HandoffTurn is Handoff if the turn in progress is still turn, checked
// under the lock (the passes of P10.6: a turn reclaimed then handed again
// is not closed by the old passes).
func (n *Navette) HandoffTurn(actor, note string, turn int) (*Handover, error) {
	return n.handover(actor, actor, note, false, false, turn, nil)
}

// Reclaim lets the author take the hand back while the agent has it: the
// changes of the agent are processed as by « glose done », then the turn
// passes to the author. With force, the changes of the agent are left as
// they are, without being converted (when the conversion fails and the agent
// cannot fix it).
func (n *Navette) Reclaim(note string, force bool) (*Handover, error) {
	return n.handover(review.Author, review.Agent, note, true, force, 0, nil)
}

// handover runs a handover for the turn of owner (whose changes are
// processed), recorded as done by actor.
func (n *Navette) handover(actor, owner, note string, reclaimed, force bool, expect int, then func(next int) (func(), error)) (*Handover, error) {
	lock, err := n.Lock(lockWait)
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	if err := n.prepare(); err != nil {
		return nil, err
	}
	h, err := n.History()
	if err != nil {
		return nil, err
	}
	if expect > 0 && h.Turn() != expect {
		return nil, ErrTurnChanged
	}
	// A handover covers the whole project as it is now (D-38), not the files
	// known when this navette was made: computed under the lock.
	sc, err := n.Scope(h)
	if err != nil {
		return nil, err
	}
	return New(sc).locked(h, actor, owner, note, reclaimed, force, then)
}

// Scope returns the project that a handover covers (D-38): the project read
// again from its root (files created, or added to the profile, since this
// navette was made), plus the files of n (a file opened alone) and the files
// of the last handover that still exist, so that nothing handed over leaves
// the comparison.
func (n *Navette) Scope(h *History) (*project.Project, error) {
	whole, err := project.Load(n.P.Root)
	switch {
	case errors.Is(err, project.ErrNoMarkdown):
		// Nothing found from the root (a file opened alone that discovery
		// skips, such as AGENTS.md): the scope starts empty.
		cp := *n.P
		cp.Files = nil
		whole = &cp
	case err != nil:
		return nil, fmt.Errorf("remise : le projet entier (%s) ne peut pas être lu : %w", n.P.Root, err)
	}
	sc := *whole
	sc.Files = append([]string(nil), whole.Files...)
	seen := map[string]bool{}
	for _, f := range sc.Files {
		seen[f] = true
	}
	extra := append([]string(nil), n.P.Files...)
	if l := h.Last(); l != nil {
		for _, fh := range l.TurnEv.Files {
			extra = append(extra, fh.Path)
		}
	}
	for _, f := range extra {
		if seen[f] {
			continue
		}
		abs, clean, err := sc.Inside(f)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
			continue // deleted: track refuses it
		}
		seen[clean] = true
		sc.Files = append(sc.Files, clean)
	}
	return &sc, nil
}

// locked runs a handover under the lock, n being the scope of the handover.
func (n *Navette) locked(h *History, actor, owner, note string, reclaimed, force bool, then func(next int) (func(), error)) (*Handover, error) {
	if h.Hand() != owner {
		if owner == review.Author {
			return nil, ErrAgentTurn
		}
		return nil, fmt.Errorf("%w : la main est à l'auteur, qui ne l'a pas remise", ErrNotYourTurn)
	}
	f := n.ReadFiles()
	for i, err := range f.Errs {
		if err != nil {
			return nil, fmt.Errorf("%w : %s : %v", ErrFiles, n.P.Files[i], err)
		}
	}
	res := &Handover{}
	gen := n.Gen(f, h)

	// The agent's turn: its free changes become proposals (D-25).
	if owner == review.Agent && !force {
		changes, converted, err := n.track(f, h, gen)
		if err != nil {
			return nil, err
		}
		if len(changes) > 0 {
			if err := n.Commit(review.Agent, "track", "", note, changes); err != nil {
				return nil, err
			}
			res.Converted = converted
			res.Files += len(changes)
			if f, err = n.reread(f, changes); err != nil {
				return nil, err
			}
		}
	}

	// Identity of the annotations written by hand during the turn.
	var changes []Change
	for i, d := range f.Docs {
		out, err := review.Assign(d.Bytes, 0, len(d.Bytes), owner, gen)
		if err != nil {
			return nil, fmt.Errorf("%s : %w", n.P.Files[i], err)
		}
		if string(out) != string(d.Bytes) {
			res.Identified += len(review.Read(out).IDs()) - len(review.Read(d.Bytes).IDs())
			changes = append(changes, Change{File: i, Before: d.Bytes, After: out})
		}
	}
	if len(changes) > 0 {
		if err := n.Commit(owner, "identify", "", "", changes); err != nil {
			return nil, err
		}
		res.Files += len(changes)
		var err error
		if f, err = n.reread(f, changes); err != nil {
			return nil, err
		}
	}

	// The copy of the files handed over, and the turn event.
	info := &journal.TurnInfo{Number: h.Turn(), Note: note, Counts: map[string]int{}, Reclaimed: reclaimed}
	// The inventory of the files that exist now: a file of a later scope
	// that is not in it was created afterwards (D-38).
	if inv, ok := project.Inventory(n.P.Root, n.P.Files); ok {
		info.Existing, info.Complete = inv, true
	}
	for i, d := range f.Docs {
		if f.Errs[i] != nil {
			return nil, fmt.Errorf("%w : %s", ErrFiles, n.P.Files[i])
		}
		if _, err := journal.SaveSnapshot(n.P.State(), d.Bytes); err != nil {
			return nil, err
		}
		info.Files = append(info.Files, journal.FileHash{Path: n.P.Files[i], Hash: d.Hash})
		for _, a := range f.Annot[i] {
			info.Counts[a.Kind]++
			if a.ID != "" {
				info.IDs = append(info.IDs, a.ID)
			}
			for _, m := range a.Messages {
				if m.Reply && m.ID != "" {
					info.IDs = append(info.IDs, m.ID)
				}
			}
		}
	}
	// The copies must survive a power cut before the event that names them
	// (P5.4): a copy kept by an earlier attempt whose flush failed is made
	// durable here too.
	// Last check before the turn event: no file changed since it was read
	// and copied (an editor still writing after « glose done »).
	if _, err := n.reread(f, nil); err != nil {
		return nil, err
	}
	// The entry of snapshots/ in .glose/ is flushed too.
	for _, dir := range []string{filepath.Join(n.P.State(), journal.SnapshotDir), n.P.State()} {
		if err := store.SyncDir(dir); err != nil {
			return nil, fmt.Errorf("copies de remise non synchronisées sur le disque : %w", err)
		}
	}
	undo := func() {}
	if then != nil {
		u, err := then(h.Turn() + 1)
		if err != nil {
			return nil, err
		}
		if u != nil {
			undo = u
		}
	}
	evs, err := n.J.Append(journal.Event{Type: journal.Turn, Actor: owner, TurnEv: info})
	if err != nil {
		undo()
		return nil, err
	}
	res.Event = evs[0]
	if reclaimed {
		res.Event.Actor = actor
	}
	return res, nil
}

// Preview returns what « glose done » would refuse now, without writing
// anything: the anomalies of the agent's changes (for « glose check »).
func (n *Navette) Preview() ([]Problem, error) {
	h, err := n.History()
	if err != nil {
		return nil, err
	}
	if h.Hand() != review.Agent {
		return nil, nil
	}
	sc, err := n.Scope(h)
	if err != nil {
		return nil, err
	}
	if !sameFiles(sc.Files, n.P.Files) {
		return New(sc).Preview()
	}
	f := n.ReadFiles()
	for i, err := range f.Errs {
		if err != nil {
			return nil, fmt.Errorf("%w : %s : %v", ErrFiles, n.P.Files[i], err)
		}
	}
	_, _, err = n.track(f, h, n.Gen(f, h))
	var te *TrackError
	if errors.As(err, &te) {
		return te.Problems, nil
	}
	return nil, err
}

func sameFiles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ErrChangedDuringHandover is returned when a file changes on the disk
// while a handover runs (an editor of the agent still writing).
var ErrChangedDuringHandover = errors.New("un fichier a changé pendant la remise")

// reread reads the files again after the writes of a handover and checks
// that each one is exactly what is expected: what Glose wrote for the files
// it changed, what was read before for the others. A file changed meanwhile
// by another program would otherwise become the copy of the handover without
// being converted (D-38): the handover is refused, to be run again.
func (n *Navette) reread(before *Files, changes []Change) (*Files, error) {
	want := make([]string, len(before.Docs))
	for i, d := range before.Docs {
		want[i] = d.Hash
	}
	for _, c := range changes {
		want[c.File] = store.Hash(c.After)
	}
	f := n.ReadFiles()
	for i, d := range f.Docs {
		if f.Errs[i] != nil || d.Hash != want[i] {
			return nil, fmt.Errorf("%w : %s ; les modifications déjà converties restent des propositions, relancez la remise", ErrChangedDuringHandover, n.P.Files[i])
		}
	}
	return f, nil
}
