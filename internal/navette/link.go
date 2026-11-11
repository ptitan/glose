package navette

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// Link binds afterwards a session of an agent to a Glose opened by hand
// (P10.2, D-49): the interface asks for a short code, the agent runs
// « glose join CODE ». The code protects nothing (everything is local): it
// avoids binding the wrong session or the wrong project. One code at a time,
// in .glose/link.json, since the command of the agent is another process.
type Link struct {
	Code    string `json:"code"`
	Created string `json:"created"`
	Expires string `json:"expires"`
	// Used is the name of the agent that took the code (P10.12): the same
	// agent may run « glose join » with it again, as long as no new code is
	// created, to wait for each turn; no other may.
	Used   string `json:"used,omitempty"`
	UsedAt string `json:"usedAt,omitempty"`
	// Session identifies the session that took the code, when the agent
	// gives one (SessionKey): another session of the same name is refused.
	Session string `json:"session,omitempty"`
}

// SessionKey identifies the session of the agent that runs this command,
// when the agent exposes one (OpenCode: OPENCODE_SESSION_ID); "" else.
func SessionKey() string {
	for _, k := range []string{"OPENCODE_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CODEX_SESSION_ID"} {
		if v := os.Getenv(k); v != "" {
			return k + "=" + v
		}
	}
	return ""
}

// LinkFile is the name of the file of the code in the state directory.
const LinkFile = "link.json"

// LinkTTL is how long a code stays valid.
const LinkTTL = 10 * time.Minute

// linkAlphabet avoids the characters that are easily confused (0, O, 1, I, L).
const linkAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// Errors of « glose join ».
var (
	ErrNoLink      = errors.New("aucun code de liaison en cours pour ce projet : demandez-en un dans Glose (menu de la navette, « Lier un agent »)")
	ErrLinkWrong   = errors.New("ce code de liaison ne correspond pas à celui de ce projet : vérifiez le dossier et le code, ou demandez-en un nouveau")
	ErrLinkExpired = errors.New("ce code de liaison a expiré (10 minutes) : demandez-en un nouveau dans Glose")
	ErrLinkUsed    = errors.New("ce code de liaison a déjà servi à un autre agent : demandez-en un nouveau dans Glose")
)

// linkLock serialises the creation and the use of the codes, across
// processes: a code serves once, and never removes the next one.
const linkLock = "link.lock"

func lockLink(dir string) (*store.Lock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.WaitLockNamed(ctx, dir, linkLock)
}

// NewLink creates a code, valid LinkTTL from now, which replaces the
// previous one.
func NewLink(dir string, now time.Time) (Link, error) {
	l, err := lockLink(dir)
	if err != nil {
		return Link{}, err
	}
	defer l.Unlock()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return Link{}, err
	}
	code := make([]byte, 0, 7)
	for i, c := range b {
		if i == 4 {
			code = append(code, '-')
		}
		code = append(code, linkAlphabet[int(c)%len(linkAlphabet)])
	}
	k := Link{Code: string(code), Created: now.UTC().Format(time.RFC3339Nano), Expires: now.Add(LinkTTL).UTC().Format(time.RFC3339Nano)}
	data, _ := json.MarshalIndent(k, "", "  ")
	if err := store.WriteAtomic(filepath.Join(dir, LinkFile), data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return Link{}, err
	}
	return k, nil
}

// normalCode puts a typed code in the form of the file: capitals, without
// spaces or dashes.
func normalCode(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(s)))
}

// UseLink checks code against the code of the project and its deadline,
// and records that the agent name took it: a code serves one agent. The same
// agent may use it again later (it waits for each turn with the same
// command), until a new code is created; the deadline only bounds the first
// use. It returns whether the code had already been taken by name.
func UseLink(dir, code, name, session string, now time.Time) (again bool, err error) {
	lk, err := lockLink(dir)
	if err != nil {
		return false, err
	}
	defer lk.Unlock()
	path := filepath.Join(dir, LinkFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, ErrNoLink
	}
	if err != nil {
		return false, err
	}
	var l Link
	if json.Unmarshal(b, &l) != nil || l.Code == "" {
		return false, ErrNoLink
	}
	if normalCode(code) != normalCode(l.Code) {
		return false, ErrLinkWrong
	}
	if l.Used != "" {
		if l.Used == name && (l.Session == "" || l.Session == session) {
			return true, nil
		}
		return false, ErrLinkUsed
	}
	exp, err := time.Parse(time.RFC3339Nano, l.Expires)
	if err != nil || !now.Before(exp) {
		store.Remove(path)
		return false, ErrLinkExpired
	}
	l.Used, l.UsedAt, l.Session = name, now.UTC().Format(time.RFC3339Nano), session
	data, _ := json.MarshalIndent(l, "", "  ")
	if err := store.WriteAtomic(path, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return false, err
	}
	return false, nil
}

// ReadLink returns the code in progress, or nil.
func ReadLink(dir string) *Link {
	b, err := os.ReadFile(filepath.Join(dir, LinkFile))
	if err != nil {
		return nil
	}
	var l Link
	if json.Unmarshal(b, &l) != nil || l.Code == "" {
		return nil
	}
	return &l
}

// StillLinked reports whether code is still the code of the project, taken
// by name: a new code ends the waits of the old one.
func StillLinked(dir, code, name string) bool {
	l := ReadLink(dir)
	return l != nil && normalCode(l.Code) == normalCode(code) && l.Used == name
}

// Owner of a turn of the agent (audit of P10.12): who works it, an agent
// launched by Glose or a bound session, recorded before the hand is visible
// to anyone (by Glose, in the transaction of the handover; by « glose join »,
// under the lock of the project): never both. Not normative.
type Owner struct {
	Turn int    `json:"turn"`
	Kind string `json:"kind"` // launch or link
	Name string `json:"name"`
	// ID: the code of the binding (link), or the reservation (launch).
	ID string `json:"id"`
}

// Kinds of owners.
const (
	OwnerLaunch = "launch"
	OwnerLink   = "link"
)

const ownerFile = "owner.json"

// ReadOwner returns the owner of turn, or nil.
func ReadOwner(dir string, turn int) *Owner {
	b, err := os.ReadFile(filepath.Join(dir, ownerFile))
	if err != nil {
		return nil
	}
	var o Owner
	if json.Unmarshal(b, &o) != nil || o.Turn != turn || (o.Kind != OwnerLaunch && o.Kind != OwnerLink) {
		return nil
	}
	return &o
}

// SetOwner records o, and returns the function that puts back what was
// there before (a handover whose event could not be written).
func SetOwner(dir string, o Owner) (undo func(), err error) {
	lk, err := lockLink(dir)
	if err != nil {
		return nil, err
	}
	defer lk.Unlock()
	return setOwner(dir, o)
}

func setOwner(dir string, o Owner) (func(), error) {
	path := filepath.Join(dir, ownerFile)
	prev, perr := os.ReadFile(path)
	data, _ := json.MarshalIndent(o, "", "  ")
	if err := store.WriteAtomic(path, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return nil, err
	}
	return func() {
		if perr != nil {
			store.Remove(path)
			return
		}
		store.WriteAtomic(path, prev, 0o600)
	}, nil
}

// TakeTurn gives the turn in progress to the session bound with code under
// name, if it may have it, checked under the lock of the project (the hand,
// the turn, the plan and the owner are those of one moment): the hand at the
// agent; a plan of passes of this turn decides alone (it names the bound
// session or not); otherwise no agent launched by Glose at work, and the
// turn owned by nobody, by this binding, or by an older binding (a code
// replaced since). A turn reserved for an agent launched by Glose stays its
// own, even if it stopped without handing over.
func (n *Navette) TakeTurn(name, code string) (bool, error) {
	lock, err := n.Lock(lockWait)
	if err != nil {
		return false, err
	}
	defer lock.Unlock()
	h, err := n.History()
	if err != nil {
		return false, err
	}
	if h.Hand() != review.Agent || !StillLinked(n.P.State(), code, name) {
		return false, nil
	}
	dir, turn := n.P.State(), h.Turn()
	if ps := ReadPasses(dir); ps != nil && ps.Turn == turn {
		// Its part closed (glose done), the session waits for the next turn.
		return ps.Linked == name && !ps.LinkedDone, nil
	}
	if LaunchedAgent(dir) != nil {
		return false, nil
	}
	lk, err := lockLink(dir)
	if err != nil {
		return false, err
	}
	defer lk.Unlock()
	if o := ReadOwner(dir, turn); o != nil && (o.Kind == OwnerLaunch || (o.ID == normalCode(code) && o.Name != name)) {
		return false, nil
	}
	_, err = setOwner(dir, Owner{Turn: turn, Kind: OwnerLink, Name: name, ID: normalCode(code)})
	return err == nil, err
}

// ReserveLaunch makes an agent launched by Glose, under name, the owner of
// the turn in progress, under the lock of the project (the same moment as
// TakeTurn): refused if a bound session owns it. It returns the owner that
// refused, if any.
func (n *Navette) ReserveLaunch(name string) (*Owner, error) {
	return n.ReserveLaunchThen(name, nil)
}

// ReserveLaunchThen reserves the turn as ReserveLaunch does, then calls start
// with the history read under the lock, still holding the lock of the
// project (audit A2): no handover (the author taking the hand back) can come
// between the reservation and the start of the agent, and what is launched
// is built from the turn that was reserved. start is not called when the
// reservation is refused.
func (n *Navette) ReserveLaunchThen(name string, start func(h *History) error) (*Owner, error) {
	lock, err := n.Lock(lockWait)
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	h, err := n.History()
	if err != nil {
		return nil, err
	}
	if h.Hand() != review.Agent {
		return nil, errors.New("la main n'est pas à l'agent")
	}
	if o, err := n.reserve(h, name); o != nil || err != nil {
		return o, err
	}
	if start != nil {
		return nil, start(h)
	}
	return nil, nil
}

// ErrNotThisTurn means that the hand is no longer at the agent for the turn
// expected (the author took it back, or another turn began).
var ErrNotThisTurn = errors.New("la main n'est plus à l'agent pour ce tour")

// WhileTurn runs f under the lock of the project if the hand is at the agent
// for turn: no handover can come in between (audit A2, for the passes of
// P10.6, which share the turn with a bound session and so do not reserve it).
func (n *Navette) WhileTurn(turn int, f func() error) error {
	lock, err := n.Lock(lockWait)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	h, err := n.History()
	if err != nil {
		return err
	}
	if h.Hand() != review.Agent || h.Turn() != turn {
		return ErrNotThisTurn
	}
	return f()
}

// reserve records the launch under name as the owner of the turn of h, under
// the lock of the links, unless a bound session owns it (returned).
func (n *Navette) reserve(h *History, name string) (*Owner, error) {
	dir := n.P.State()
	lk, err := lockLink(dir)
	if err != nil {
		return nil, err
	}
	defer lk.Unlock()
	if o := ReadOwner(dir, h.Turn()); o != nil && o.Kind == OwnerLink {
		return o, nil
	}
	b := make([]byte, 8)
	rand.Read(b)
	_, err = setOwner(dir, Owner{Turn: h.Turn(), Kind: OwnerLaunch, Name: name, ID: fmt.Sprintf("%x", b)})
	return nil, err
}
