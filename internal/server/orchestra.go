package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ptitan/glose/internal/agentrun"
	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
)

// Turns with several recipients (P10.6, D-49). At the handover, the
// annotations that the agents must answer are given to their recipient (to=,
// else the agent of the text); Glose launches one agent per recipient, one
// after the other, then hands the turn back itself. A session bound to Glose
// that listens answers the annotations without recipient, and those
// addressed to its name, at the same time. The routing is fixed in the plan
// (.glose/passes.json) by identifiers of annotations.

// group is the annotations given to one recipient.
type group struct {
	key   string // short name addressed explicitly, "" for none
	ids   []string
	count int
}

// toAnswer reports whether the agents must answer a: an annotation of the
// author, or a thread whose last message is the author's (a reply to a
// proposal of the agent, which may readdress it).
func toAnswer(a render.Annotation) bool {
	if len(a.Partners) > 0 && !a.Primary {
		return false
	}
	if a.By != review.Agent {
		return true
	}
	n := len(a.Messages)
	return n > 0 && a.Messages[n-1].Reply && a.Messages[n-1].By == review.Author
}

// recipients groups what the agents must answer by explicit recipient (a
// short name known here), "" for none (or one unknown on this machine).
func recipients(c current, cfg agents.Config) []group {
	by := map[string]*group{}
	f := c.N.ReadFiles()
	for i := range c.P.Files {
		if f.Errs[i] != nil {
			continue
		}
		for _, a := range f.Annot[i] {
			if !toAnswer(a) {
				continue
			}
			key := ""
			if a.To != "" {
				if x, ok := cfg.Alias(a.To); ok {
					key = x.Name
				}
			}
			g := by[strings.ToLower(key)]
			if g == nil {
				g = &group{key: key}
				by[strings.ToLower(key)] = g
			}
			g.count++
			if a.ID != "" {
				g.ids = append(g.ids, a.ID)
			}
		}
	}
	var out []group
	for _, g := range by {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// plan gives each group to an agent: the bound session (linked) takes the
// annotations without recipient and those addressed to its name; the
// others go to one pass per short name, the agent of the text (text) taking
// those without recipient when no session is bound. The agent of the text
// comes first.
func plan(groups []group, cfg agents.Config, text agents.Alias, linked string) []navette.Pass {
	byName := map[string]*navette.Pass{}
	var order []string
	add := func(a agents.Alias, g group) {
		k := strings.ToLower(a.Name)
		p := byName[k]
		if p == nil {
			p = &navette.Pass{Alias: a.Name, Agent: a.Agent, Model: a.Model, State: navette.PassWaiting, IDs: []string{}}
			byName[k] = p
			order = append(order, k)
		}
		p.Count += g.count
		p.IDs = append(p.IDs, g.ids...)
	}
	for _, g := range groups {
		if g.count == 0 {
			continue
		}
		if linked != "" && (g.key == "" || strings.EqualFold(g.key, linked)) {
			continue
		}
		a := text
		if g.key != "" {
			a, _ = cfg.Alias(g.key)
		}
		add(a, g)
	}
	sort.SliceStable(order, func(i, j int) bool {
		ti, tj := order[i] == strings.ToLower(text.Name), order[j] == strings.ToLower(text.Name)
		if ti != tj {
			return ti
		}
		return order[i] < order[j]
	})
	var out []navette.Pass
	for _, k := range order {
		out = append(out, *byName[k])
	}
	return out
}

func newPlanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// orchestra is the turn with several recipients in progress.
type orchestra struct {
	mu     sync.Mutex
	cancel chan struct{} // closed by the author (Arrêter)
	closed bool
}

func (o *orchestra) stop() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.closed = true
		close(o.cancel)
	}
}

func (o *orchestra) cancelled() bool {
	select {
	case <-o.cancel:
		return true
	default:
		return false
	}
}

// orchestrate runs the passes of the plan in the background. A previous
// orchestration is stopped first.
func (s *Server) orchestrate(c current, cfg agents.Config, ps *navette.Passes) {
	o := &orchestra{cancel: make(chan struct{})}
	s.orchMu.Lock()
	if s.orch != nil {
		s.orch.stop()
	}
	s.orch = o
	s.orchMu.Unlock()
	go s.runPasses(c, cfg, o, ps.ID, ps.Turn)
}

// resumePasses takes up, after a restart of the server, the plan of the
// turn in progress: the pass that was running is awaited (its agent, an
// orphan, may still work), the next ones are launched, then the hand is
// handed back.
func (s *Server) resumePasses() {
	c := s.cur()
	ps := navette.ReadPasses(c.P.State())
	if ps == nil || !s.stillTurn(ps.Turn) {
		return
	}
	cfg, err := agents.Load()
	if err != nil {
		return
	}
	s.orchestrate(c, cfg, ps)
}

// stillTurn reports whether the hand is still at the agent, in turn.
func (s *Server) stillTurn(turn int) bool {
	h, err := s.cur().N.History()
	return err == nil && h.Hand() == review.Agent && h.Turn() == turn
}

func (s *Server) runPasses(c current, cfg agents.Config, o *orchestra, id string, turn int) {
	state := c.P.State()
	defer func() {
		s.orchMu.Lock()
		if s.orch == o {
			s.orch = nil
		}
		s.orchMu.Unlock()
	}()
	current := func() *navette.Passes {
		p := navette.ReadPasses(state)
		if p == nil || p.ID != id {
			return nil
		}
		return p
	}
	set := func(i int, f func(*navette.Pass)) {
		navette.UpdatePasses(state, func(p *navette.Passes) *navette.Passes {
			if p != nil && p.ID == id && i < len(p.Passes) {
				f(&p.Passes[i])
			}
			return p
		})
	}
	abandon := func() { navette.RemovePlan(state, id) }
	var mcp []string
	for _, q := range c.P.Agent.Requires {
		if name, ok := strings.CutPrefix(q, "mcp:"); ok {
			mcp = append(mcp, name)
		}
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	// stopped reports whether to give up: the author stopped the agents,
	// or took the hand back, or another plan replaced this one.
	stopped := func() bool { return o.cancelled() || !s.stillTurn(turn) || current() == nil }
	for i := 0; ; i++ {
		ps := current()
		if ps == nil || stopped() {
			abandon()
			return
		}
		if i >= len(ps.Passes) {
			break
		}
		p := ps.Passes[i]
		switch p.State {
		case navette.PassRunning:
			// Resumed after a restart: its agent may still work (an orphan).
			for {
				st, ok := s.runner.Status(state)
				if !ok || !st.Running || !st.Orphan {
					break
				}
				select {
				case <-o.cancel:
					s.runner.Stop(state)
					abandon()
					return
				case <-tick.C:
				}
			}
			set(i, func(x *navette.Pass) {
				if x.State == navette.PassRunning {
					x.State, x.Error = navette.PassEnded, "interrompu par un redémarrage du serveur"
				}
			})
			continue
		case navette.PassWaiting:
		default:
			continue
		}
		// Running before it starts: an agent quick to run glose done finds
		// its pass open.
		set(i, func(x *navette.Pass) { x.State = navette.PassRunning })
		ended := make(chan agentrun.Status, 1)
		_, err := s.runner.Start(agentrun.Launch{
			Spec: agentrun.Spec{Agent: p.Agent, Model: p.Model, Dir: c.P.Root, Name: p.Alias, MCP: mcp,
				Env:    []string{navette.PassEnv + "=" + p.Alias},
				Prompt: agentrun.PassPrompt(p.Alias, c.P.Root, c.P.Agent.Instructions, c.P.Agent.Requires, true)},
			State: state, Turn: turn, Max: cfg.Max(),
			Ended: func(st agentrun.Status) { ended <- st },
		})
		if err != nil {
			set(i, func(x *navette.Pass) { x.State, x.Error = navette.PassFailed, err.Error() })
			continue
		}
	wait:
		for {
			select {
			case <-o.cancel:
				s.runner.Stop(state)
				<-ended
				abandon()
				return
			case st := <-ended:
				set(i, func(x *navette.Pass) {
					if x.State == navette.PassRunning {
						x.State = navette.PassEnded
						x.Error = fmt.Sprintf("arrêté sans clore son passage (code %d)", st.ExitCode)
						if st.Error != "" {
							x.Error = st.Error
						}
					}
				})
				break wait
			case <-tick.C:
				if !s.stillTurn(turn) || current() == nil {
					s.runner.Stop(state)
					<-ended
					abandon()
					return
				}
			}
		}
	}
	// The bound session, which answers at the same time.
	for {
		p := current()
		if p == nil || stopped() {
			abandon()
			return
		}
		if p.Linked == "" || p.LinkedDone {
			break
		}
		select {
		case <-o.cancel:
			abandon()
			return
		case <-tick.C:
		}
	}
	p := current()
	if p == nil || stopped() {
		abandon()
		return
	}
	var notes []string
	for _, x := range p.Passes {
		switch {
		case x.State == navette.PassDone && x.Note != "":
			notes = append(notes, x.Alias+" : "+x.Note)
		case x.State == navette.PassFailed || x.State == navette.PassEnded:
			notes = append(notes, x.Alias+" : "+x.Error)
		}
	}
	if p.Linked != "" && p.LinkedNote != "" {
		notes = append(notes, p.Linked+" : "+p.LinkedNote)
	}
	// Only this turn: a turn reclaimed then handed again is not closed.
	_, err := s.cur().N.HandoffTurn(review.Agent, strings.Join(notes, "\n"), turn)
	if err != nil && !errors.Is(err, navette.ErrTurnChanged) {
		s.setLaunchError("remise des passages impossible : " + err.Error())
		navette.UpdatePasses(state, func(q *navette.Passes) *navette.Passes {
			if q != nil && q.ID == id {
				q.Passes = append(q.Passes, navette.Pass{Alias: "Glose", State: navette.PassFailed, Error: "remise impossible : " + err.Error(), IDs: []string{}})
			}
			return q
		})
		return
	}
	abandon()
}
