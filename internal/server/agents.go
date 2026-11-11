package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ptitan/glose/internal/agentrun"
	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/web"
)

// link creates a code to bind a session of an agent to this server (P10.2,
// D-49), and the instruction to paste in that session, ready as it is
// (P10.10): it names the agent of the text, else the default one.
func (s *Server) link(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	l, err := navette.NewLink(c.P.State(), time.Now())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "write", "code de liaison non écrit : "+err.Error())
		return
	}
	name := "agent"
	if cfg, err := agents.Load(); err == nil {
		if a, ok := textAgent(cfg, c.P); ok {
			name = a.Name
		}
	}
	busy := ""
	if a := navette.LaunchedAgent(c.P.State()); a != nil {
		busy = a.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": l.Code, "created": l.Created, "expires": l.Expires, "root": c.P.Root,
		"name": name, "busy": busy, "command": JoinCommand(l.Code, name), "instruction": JoinInstruction(l.Code, c.P.Root, name)})
}

// JoinCommand is the command that binds an agent with code, under name.
func JoinCommand(code, name string) string {
	return fmt.Sprintf(`glose join %s --agent %q`, code, name)
}

// JoinInstruction is the instruction pasted in the session of the agent: the
// command, and that it waits (P10.12).
func JoinInstruction(code, root, name string) string {
	cmd := JoinCommand(code, name)
	return fmt.Sprintf("Mène la navette Glose de ce projet. Dans le dossier %s, lance `%s`. "+
		"Cette commande attend que l'auteur te remette la main : si elle se termine par « délai écoulé », relance-la telle quelle, autant de fois qu'il le faut. "+
		"Quand elle te rend la main, suis les consignes qu'elle affiche (réponds, propose, puis `glose done`), puis relance la même commande pour attendre le tour suivant. "+
		"Si tu connais l'identifiant de ton modèle et que la commande ne l'affiche pas, ajoute-lui `--model` suivi de cet identifiant.", root, cmd)
}

// ---------- Settings of the agents (P10.3, « glose set ») ----------

// agentRoutes registers the page and the routes of the settings of the
// agents: on the server of a project, and on the settings server of
// « glose set » outside any project.
func (s *Server) agentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /set", s.setPage)
	mux.HandleFunc("GET /api/agents", s.getAgents)
	mux.HandleFunc("POST /api/agents", s.saveAgents)
	mux.HandleFunc("POST /api/agents/detect", s.detectAgents)
	mux.HandleFunc("GET /api/agents/models", s.agentModels)
	mux.HandleFunc("GET /api/agents/tools", s.agentTools)
	mux.HandleFunc("POST /api/agents/try", s.tryAgent)
}

// runRoutes are the routes of the agent launched by Glose (P10.5), on the
// server of a project only.
func (s *Server) runRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks/preview", s.taskPreview)
	mux.HandleFunc("GET /api/agent/run", s.agentRun)
	mux.HandleFunc("GET /api/agent/log", s.agentLog)
	mux.HandleFunc("POST /api/agent/stop", s.agentStop)
	mux.HandleFunc("POST /api/agent/launch", s.agentLaunch)
}

// textAgent returns the alias that answers for the text: the one asked by
// glose.json if it exists on this machine, else the default one.
func textAgent(c agents.Config, p *project.Project) (agents.Alias, bool) {
	if p != nil && p.Agent.Alias != "" {
		if a, ok := c.Alias(p.Agent.Alias); ok {
			return a, true
		}
	}
	return c.Alias(c.Default)
}

// agentTools checks the tools required by the text for its agent (P10.4),
// or for the alias asked.
func (s *Server) agentTools(w http.ResponseWriter, r *http.Request) {
	c, err := agents.Load()
	if err != nil {
		jsonError(w, http.StatusConflict, "config", err.Error())
		return
	}
	p := s.cur().P
	a, ok := textAgent(c, p)
	if name := r.URL.Query().Get("alias"); name != "" {
		a, ok = c.Alias(name)
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "alias", "aucun agent configuré pour ce texte (glose set)")
		return
	}
	requires := []string{}
	if p != nil && p.Agent.Requires != nil {
		requires = p.Agent.Requires
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	checks, err := agents.CheckTools(ctx, a.Agent, projectRoot(p), requires)
	out := map[string]any{"alias": a, "requires": requires, "checks": checks}
	if err != nil {
		out["message"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func projectRoot(p *project.Project) string {
	if p == nil {
		return agentrun.TryDir()
	}
	return p.Root
}

// tryRequest names the pair to try: a saved alias, or the agent and model
// of a line of the page not saved yet.
type tryRequest struct {
	Task  *agents.Task `json:"task,omitempty"`
	Name  string       `json:"name"`
	Agent string       `json:"agent"`
	Model string       `json:"model"`
}

// tryAgent runs a dry run of an agent (P10.4, « Essayer »): explicit, since
// it costs tokens; one at a time; two minutes at most.
func (s *Server) tryAgent(w http.ResponseWriter, r *http.Request) {
	var req tryRequest
	if !decode(w, r, &req) {
		return
	}
	if _, ok := agents.KindOf(req.Agent); !ok {
		jsonError(w, http.StatusBadRequest, "agent", "agent inconnu")
		return
	}
	if req.Model != "" {
		if err := agents.CheckModel(req.Model); err != nil {
			jsonError(w, http.StatusBadRequest, "model", err.Error())
			return
		}
	}
	if req.Name == "" || agents.CheckAlias(req.Name) != nil {
		req.Name = "essai"
	}
	if !s.trying.CompareAndSwap(false, true) {
		jsonError(w, http.StatusConflict, "busy", "un essai est déjà en cours")
		return
	}
	defer s.trying.Store(false)
	p := s.cur().P
	var requires, mcp []string
	if p != nil {
		requires = p.Agent.Requires
		for _, q := range requires {
			if name, ok := strings.CutPrefix(q, "mcp:"); ok {
				mcp = append(mcp, name)
			}
		}
	}
	spec := agentrun.Spec{Agent: req.Agent, Model: req.Model, Dir: projectRoot(p), Name: req.Name,
		Prompt: agentrun.TryPrompt(p != nil, requires), MCP: mcp}
	if req.Task != nil {
		if err := agents.CheckTasks([]agents.Task{*req.Task}, false); err != nil {
			jsonError(w, 400, "task", err.Error())
			return
		}
		cfg, err := agents.Load()
		if err != nil {
			jsonError(w, 409, "config", err.Error())
			return
		}
		access := agents.MergeAccess(cfg.Access, req.Task.Access)
		access.MCP = agents.MergeAccess(access, agents.Access{MCP: mcp}).MCP
		for i, d := range access.Dirs {
			if !filepath.IsAbs(d) {
				access.Dirs[i] = filepath.Join(spec.Dir, d)
			}
		}
		spec.Root = spec.Dir
		if req.Task.Workdir != "" {
			dir := req.Task.Workdir
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(spec.Root, dir)
			}
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				jsonError(w, 400, "task", "dossier de lancement introuvable")
				return
			}
			fi, err := os.Stat(resolved)
			if err != nil || !fi.IsDir() {
				jsonError(w, 400, "task", "dossier de lancement introuvable")
				return
			}
			spec.Dir = resolved
		}
		spec.Access = &access
		spec.Prompt += "\nConsigne à vérifier sans l'exécuter : " + req.Task.Name + "\n" + req.Task.Prompt
	}
	res := agentrun.Try(r.Context(), spec)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) setPage(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(web.Files, "reglages-agents.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}

// agentsResponse is the state of the settings of the agents.
type agentsResponse struct {
	AccessWarnings map[string][]string `json:"accessWarnings"`
	TaskIcons      []string            `json:"taskIcons"`
	Config         agents.Config       `json:"config"`
	Hash           string              `json:"hash"` // of agents.json, sent back to save
	Configured     bool                `json:"configured"`
	Detection      agents.Detection    `json:"detection"`
	// Project: the text served, and what its glose.json asks (empty on
	// the settings server).
	Project *projectAgent `json:"project"`
	Error   string        `json:"error,omitempty"`
}

type projectAgent struct {
	Title string               `json:"title"`
	Root  string               `json:"root"`
	Agent project.AgentProfile `json:"agent"`
}

func (s *Server) agentsState(ctx context.Context, d *agents.Detection) agentsResponse {
	out := agentsResponse{Config: agents.Config{Version: 1, Aliases: []agents.Alias{}}}
	c, h, err := agents.LoadWithHash()
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Config, out.Hash, out.Configured = c, h, c.Configured()
	}
	out.TaskIcons = agents.TaskIcons
	out.AccessWarnings = map[string][]string{}
	for _, k := range agents.Kinds {
		out.AccessWarnings[k.ID] = agentrun.AccessWarnings(k.ID, out.Config.Access)
	}
	if d != nil {
		out.Detection = *d
	} else {
		out.Detection = agents.CachedDetection(ctx)
	}
	if p := s.cur().P; p != nil {
		out.Project = &projectAgent{Title: p.Title, Root: p.Root, Agent: p.Agent}
		if out.Project.Agent.Requires == nil {
			out.Project.Agent.Requires = []string{}
		}
	}
	return out
}

func (s *Server) getAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agentsState(r.Context(), nil))
}

func (s *Server) detectAgents(w http.ResponseWriter, r *http.Request) {
	d := agents.Detect(r.Context())
	agents.SaveDetection(d)
	writeJSON(w, http.StatusOK, s.agentsState(r.Context(), &d))
}

func (s *Server) agentModels(w http.ResponseWriter, r *http.Request) {
	m, from, err := agents.Models(r.Context(), r.URL.Query().Get("agent"))
	out := map[string]any{"models": m, "fromAgent": from}
	if m == nil {
		out["models"] = []string{}
	}
	if err != nil {
		out["message"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// saveRequest is the configuration sent by the page, with the hash of the
// file it was read from.
type saveRequest struct {
	agents.Config
	Hash *string `json:"hash"`
}

func (s *Server) saveAgents(w http.ResponseWriter, r *http.Request) {
	var req saveRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Hash == nil {
		jsonError(w, http.StatusBadRequest, "hash", "empreinte des réglages manquante")
		return
	}
	// An unreadable file is never overwritten: the author repairs it first.
	if _, err := agents.Load(); err != nil {
		jsonError(w, http.StatusConflict, "config", err.Error()+" : corrigez-le ou supprimez-le, puis recommencez")
		return
	}
	_, err := agents.SaveIfUnchanged(req.Config, *req.Hash)
	switch {
	case errors.Is(err, agents.ErrConfigChanged):
		jsonError(w, http.StatusConflict, "config-changed", err.Error()+" : rechargez la page pour les voir ; votre saisie reste affichée ici")
		return
	case err != nil:
		jsonError(w, http.StatusBadRequest, "config", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.agentsState(r.Context(), nil))
}

// agentsConfigured tells the interface whether a default agent is recorded
// (« Aucun agent configuré », P10.3).
func agentsConfigured() bool {
	c, err := agents.Load()
	return err == nil && c.Configured()
}

// settingsIdle stops the settings server when its page has made no request
// for that long (it pings every 10 s; a hidden tab may be slowed down to one
// request a minute); settingsFirst leaves time to open it.
var (
	settingsIdle  = 3 * time.Minute
	settingsFirst = 3 * time.Minute
)

// ServeSettings runs the server of « glose set » and « glose home » outside
// any project (P10.3, P11.5): 127.0.0.1, a token, only the settings of the
// agents and the overview of the projects. page is the page to open (/set,
// /home); ready receives its address once it listens. It returns when the page
// has been closed for settingsIdle, or when stop is closed.
func ServeSettings(page string, ready func(url string), stop <-chan struct{}) error {
	ln, port, err := Listen()
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	s := &Server{Token: token, Port: port, Stop: make(chan struct{})}
	mux := http.NewServeMux()
	s.agentRoutes(mux)
	s.homeRoutes(mux)
	mux.HandleFunc("GET /static/{name}", s.static)
	mux.HandleFunc("GET /static/polices/{name}", s.font)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("POST /api/settings", s.setSettings)
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": true})
	})
	mux.HandleFunc("POST /api/stop", s.stop)
	var last atomic.Int64
	last.Store(time.Now().Add(settingsFirst - settingsIdle).UnixNano())
	// Only the requests of the page renew the delay: past the guard (host,
	// origin, token), on the routes it calls.
	h := s.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if t := time.Now().UnixNano(); t > last.Load() {
				last.Store(t)
			}
		}
		mux.ServeHTTP(w, r)
	}))
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: h}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	ready(fmt.Sprintf("http://127.0.0.1:%d%s#t=%s", port, page, token))
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
loop:
	for {
		select {
		case err := <-done:
			return err
		case <-stop:
			break loop
		case <-s.Stop:
			break loop
		case <-tick.C:
			if time.Since(time.Unix(0, last.Load())) > settingsIdle {
				break loop
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// A connection just opened by the browser, still without a request,
	// may hold Shutdown for 5 s: closed then.
	if err := srv.Shutdown(ctx); err != nil {
		srv.Close()
	}
	return nil
}

// ---------- Launch at the handover (P10.5, D-46) ----------

// agentInfo is what the interface needs to know of the agents with each
// poll: configured or not, the agent of the text, the run in progress.
type agentInfo struct {
	Tasks []agents.Task `json:"tasks"`
	// Passes of a turn with several recipients (P10.6), or null.
	Passes *navette.Passes `json:"passes"`
	// Link is the code of « Lier un agent » in progress, and the session
	// that took it (P10.12), or null.
	Link             *navette.Link    `json:"link"`
	Owner            *navette.Owner   `json:"owner"`   // who works the agent's turn in progress
	Aliases          []string         `json:"aliases"` // short names of glose set
	AgentsConfigured bool             `json:"agentsConfigured"`
	TextAgent        *agents.Alias    `json:"textAgent"`
	TextRequires     []string         `json:"textRequires"`
	Run              *agentrun.Status `json:"run"`
}

func (s *Server) agentInfoOf(c current, turn int) agentInfo {
	ai := agentInfo{TextRequires: c.P.Agent.Requires}
	if ai.TextRequires == nil {
		ai.TextRequires = []string{}
	}
	ai.Passes = navette.ReadPasses(c.P.State())
	ai.Link = navette.ReadLink(c.P.State())
	ai.Owner = navette.ReadOwner(c.P.State(), turn)
	ai.Aliases = []string{}
	if cfg, err := agents.Load(); err == nil {
		ai.Tasks = agents.MergeTasks(cfg.Tasks, c.P.Tasks)
		ai.AgentsConfigured = cfg.Configured()
		ai.Aliases = cfg.Names()
		if ai.Aliases == nil {
			ai.Aliases = []string{}
		}
		if a, ok := textAgent(cfg, c.P); ok {
			ai.TextAgent = &a
		}
	}
	if st, ok := s.runner.Status(c.P.State()); ok {
		ai.Run = &st
	}
	return ai
}

// launchErrMu guards Server.launchErr: the reason of the last launch that
// failed, for the answer of the handover.
var launchErrMu sync.Mutex

func (s *Server) setLaunchError(msg string) {
	launchErrMu.Lock()
	s.launchErr = msg
	launchErrMu.Unlock()
}

func (s *Server) lastLaunchError() string {
	launchErrMu.Lock()
	defer launchErrMu.Unlock()
	return s.launchErr
}

// launchPlan is what Glose launches after the author's handover.
type launchPlan struct {
	err    string
	cfg    agents.Config
	none   bool            // nothing to launch
	single *agents.Alias   // one agent (P10.5): its done hands over
	passes *navette.Passes // several recipients (P10.6), written already
	linked string
}

// beforeHandoff decides what to launch after the author's handover: if
// asked, or, when nothing is said, if an agent is configured and none
// listens; one pass per recipient when several agents must answer, or one
// beside a bound session. The plan is written before the handover, so that
// a bound session that waits sees it as soon as it gets the hand.
func (s *Server) beforeHandoff(c current, launch *bool) launchPlan {
	if launch != nil && !*launch {
		return launchPlan{none: true}
	}
	h, err := c.N.History()
	if err != nil {
		return launchPlan{err: err.Error()}
	}
	if h.Hand() != review.Author {
		return launchPlan{none: true} // the handover will be refused
	}
	linked := ""
	if a := c.N.Agent("author", ""); a != nil && a.Listening {
		linked = a.Name
	}
	cfg, err := agents.Load()
	if err != nil {
		return launchPlan{err: err.Error()}
	}
	text, ok := textAgent(cfg, c.P)
	if !ok {
		if launch == nil && linked != "" {
			return launchPlan{none: true}
		}
		return launchPlan{err: "aucun agent configuré (glose set)"}
	}
	passes := plan(recipients(c, cfg), cfg, text, linked)
	switch {
	case len(passes) > 1 || (len(passes) == 1 && linked != ""):
		return launchPlan{cfg: cfg, passes: &navette.Passes{ID: newPlanID(), Passes: passes, Linked: linked}, linked: linked}
	case len(passes) == 1:
		return launchPlan{cfg: cfg, single: &agents.Alias{Name: passes[0].Alias, Agent: passes[0].Agent, Model: passes[0].Model}}
	case launch == nil && linked != "":
		return launchPlan{none: true} // the bound session takes the turn
	}
	return launchPlan{cfg: cfg, single: &text}
}

// prepareTurn writes, in the transaction of the handover (HandoffThen), the
// plan of passes of the turn that begins, or makes the agent that Glose will
// launch its owner: a bound session never takes the hand in the moment
// before the launch, and a handover refused writes nothing (Codex audits).
func (s *Server) prepareTurn(c current, p *launchPlan, turn int) (func(), error) {
	state := c.P.State()
	switch {
	case p.passes != nil:
		p.passes.Turn = turn
		if err := navette.WritePasses(state, p.passes); err != nil {
			return nil, err
		}
		id := p.passes.ID
		return func() { navette.RemovePlan(state, id) }, nil
	case p.single != nil:
		return navette.SetOwner(state, navette.Owner{Turn: turn, Kind: navette.OwnerLaunch, Name: p.single.Name, ID: newPlanID()})
	}
	return nil, nil
}

// afterHandoff launches what beforeHandoff decided. The handover gave their
// identity to the annotations written by hand: the routing is completed.
func (s *Server) afterHandoff(c current, p launchPlan) {
	s.setLaunchError(p.err)
	switch {
	case p.err != "" || p.none:
	case p.passes != nil:
		if text, ok := textAgent(p.cfg, c.P); ok {
			fresh := plan(recipients(c, p.cfg), p.cfg, text, p.linked)
			navette.UpdatePasses(c.P.State(), func(q *navette.Passes) *navette.Passes {
				if q == nil || q.ID != p.passes.ID {
					return q
				}
				for i := range q.Passes {
					for _, f := range fresh {
						if strings.EqualFold(f.Alias, q.Passes[i].Alias) {
							q.Passes[i].IDs, q.Passes[i].Count = f.IDs, f.Count
						}
					}
				}
				return q
			})
		}
		s.orchestrate(c, p.cfg, p.passes)
	case p.single != nil:
		if _, err := s.launchAlias(c, p.cfg, *p.single); err != nil {
			s.setLaunchError(err.Error())
		}
	}
}

// launch starts the agent of the text for the turn in progress.
func (s *Server) launch(c current) (agentrun.Status, error) {
	cfg, err := agents.Load()
	if err != nil {
		return agentrun.Status{}, err
	}
	a, ok := textAgent(cfg, c.P)
	if h, e := c.N.History(); e == nil {
		if l := h.Last(); l != nil && l.Actor == review.Author && l.TurnEv.Task != nil {
			a = l.TurnEv.Task.Agent
			ok = true
		}
	}
	if !ok {
		return agentrun.Status{}, errors.New("aucun agent configuré (glose set)")
	}
	return s.launchAlias(c, cfg, a)
}

// launchAlias starts the agent a for the turn in progress.
func (s *Server) launchAlias(c current, cfg agents.Config, a agents.Alias) (agentrun.Status, error) {
	h, err := c.N.History()
	if err != nil {
		return agentrun.Status{}, err
	}
	if h.Hand() != review.Agent {
		return agentrun.Status{}, errors.New("la main n'est pas à l'agent")
	}
	// A bound session that took this turn keeps it (Codex audits); a turn
	// reserved for a launch (which may have failed) may be launched again.
	// Read and reserved under the lock of the project, as glose join does.
	if o, err := c.N.ReserveLaunch(a.Name); err != nil {
		return agentrun.Status{}, err
	} else if o != nil {
		return agentrun.Status{}, fmt.Errorf("%s (session liée) mène ce tour : rien n'est lancé à côté", o.Name)
	}
	var mcp []string
	for _, q := range c.P.Agent.Requires {
		if name, ok := strings.CutPrefix(q, "mcp:"); ok {
			mcp = append(mcp, name)
		}
	}
	spec := agentrun.Spec{Agent: a.Agent, Model: a.Model, Dir: c.P.Root, Name: a.Name, MCP: mcp, Prompt: agentrun.TurnPrompt(a.Name, c.P.Root, c.P.Agent.Instructions, c.P.Agent.Requires)}
	max := cfg.Max()
	if l := h.Last(); l != nil && l.Actor == review.Author && l.TurnEv.Task != nil {
		t := l.TurnEv.Task
		if t.Agent.Name != a.Name {
			return agentrun.Status{}, fmt.Errorf("ce tour est réservé à %s", t.Agent.Name)
		}
		spec.Root = c.P.Root
		spec.Dir = t.Workdir
		spec.Access = &t.Access
		spec.MCP = t.Access.MCP
		spec.Prompt = agentrun.TaskPrompt(spec.Prompt, c.P.Root, *t)
		max = time.Duration(t.MaxMinutes) * time.Minute
	}
	return s.runner.Start(agentrun.Launch{Spec: spec, State: c.P.State(), Turn: h.Turn(), Max: max})
}

// agentRun gives the run of the agent launched by Glose.
func (s *Server) agentRun(w http.ResponseWriter, r *http.Request) {
	st, ok := s.runner.Status(s.cur().P.State())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"run": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": st})
}

// agentLog gives the end of the output of the agent (« Voir ce qu'il fait »).
func (s *Server) agentLog(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	st, ok := s.runner.Status(c.P.State())
	if !ok {
		jsonError(w, http.StatusNotFound, "run", "aucun agent lancé par Glose")
		return
	}
	text, err := agentrun.Tail(c.P.State(), st.Log, 64<<10)
	if err != nil {
		text = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": st, "text": text})
}

// agentStop stops the agent launched by Glose, and the passes that would
// follow it.
func (s *Server) agentStop(w http.ResponseWriter, r *http.Request) {
	s.orchMu.Lock()
	o := s.orch
	s.orchMu.Unlock()
	if o != nil {
		o.stop()
	}
	if err := s.runner.Stop(s.cur().P.State()); err != nil && o == nil {
		jsonError(w, http.StatusConflict, "run", err.Error())
		return
	}
	s.agentRun(w, r)
}

// agentLaunch launches the agent during its turn (after a failure, or an
// agent that stopped without handing over).
func (s *Server) agentLaunch(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.launch(s.cur())
	switch {
	case errors.Is(err, agentrun.ErrBusy):
		jsonError(w, http.StatusConflict, "busy", err.Error())
	case err != nil:
		jsonError(w, http.StatusConflict, "launch", err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{"run": st})
	}
}
