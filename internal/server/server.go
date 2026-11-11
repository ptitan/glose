// Package server is the local review server (D-07, D-22): it serves the
// interface and the rendered text, and turns the author's intentions into
// edits of the files. It holds no logic of its own on marks: it calls
// sourcemap, patch and store.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/agentrun"
	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/edit"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/notes"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/render"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/store"
	"github.com/ptitan/glose/web"
)

// Server serves one project.
type Server struct {
	// P, R and N are the project, its renderer and its navette. They are
	// replaced when the files of the project change (a chapter created by
	// the agent, a profile changed, D-38): handlers read them through proj,
	// rend and nav. A request whose file index meets a replaced list is
	// refused by the hash check of every write.
	P     *project.Project
	R     *render.Renderer
	N     *navette.Navette
	curMu sync.RWMutex
	gen   int64 // generation of the list of files, raised when it changes
	// refreshing is set while a background refresh runs; lastRefresh is
	// when the last one started (refreshEvery apart at least).
	refreshing  atomic.Bool
	lastRefresh atomic.Int64
	Token       string
	Port        int
	// Stop is closed when a client asks the server to stop.
	Stop     chan struct{}
	stopOnce sync.Once
	// progressMu serialises the writes of .glose/progression.json.
	progressMu sync.Mutex
	// undoMu guards undos: the state of each file before the last decisions
	// of the author, to undo them (see undecide).
	undoMu sync.Mutex
	undos  []undoEntry
	// handMu guards hand: whose hand it is at a sequence number of the
	// journal, so that the poll of the interface reads the presence of the
	// agent (P10.1) without reading the whole journal each time.
	handMu sync.Mutex
	hand   handCache
	// trying is set while a dry run of an agent runs (P10.4).
	trying atomic.Bool
	// runner launches the agent at the handover (P10.5).
	runner    agentrun.Runner
	launchErr string
	// orch is the turn with several recipients in progress (P10.6).
	orchMu sync.Mutex
	orch   *orchestra
}

// handCache is the hand and the time of the last handover at seq.
type handCache struct {
	ok          bool
	gen, seq    int64
	hand, since string
	turn        int
}

// handAt returns the hand and the time of the last handover when the last
// event of the journal is seq.
func (s *Server) handAt(c current, seq int64) (hand, since string, turn int) {
	s.handMu.Lock()
	defer s.handMu.Unlock()
	if s.hand.ok && s.hand.seq == seq && s.hand.gen == c.Gen {
		return s.hand.hand, s.hand.since, s.hand.turn
	}
	h, err := c.N.History()
	if err != nil {
		return "", "", 0
	}
	hand, turn = h.Hand(), h.Turn()
	if l := h.Last(); l != nil {
		since = l.Time
	}
	s.hand = handCache{ok: true, gen: c.Gen, seq: seq, hand: hand, since: since, turn: turn}
	return hand, since, turn
}

// New returns a server for p, listening on port with token.
func New(p *project.Project, port int, token string) *Server {
	return &Server{P: p, R: newRenderer(p), N: navette.New(p), Token: token, Port: port, Stop: make(chan struct{})}
}

// newRenderer returns the renderer of the server, with its disk cache
// (P6.4): a project reopened unchanged is not read by pandoc again.
func newRenderer(p *project.Project) *render.Renderer {
	r := render.New(p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.EnableDiskCache(ctx)
	return r
}

// current is a consistent view of the project, its renderer and navette.
type current struct {
	P   *project.Project
	R   *render.Renderer
	N   *navette.Navette
	Gen int64
}

// cur returns the current project, renderer and navette, taken together:
// a handler uses one view from start to end.
func (s *Server) cur() current {
	s.curMu.RLock()
	defer s.curMu.RUnlock()
	return current{s.P, s.R, s.N, s.gen}
}

// filesHeader carries the generation of the list of files that the
// interface displays: a request that names a file by its index is refused
// if the list has changed since (two identical files swapped in the profile
// have the same hash: the index alone could name the other one).
const filesHeader = "X-Glose-Files"

// curFor returns the current view for a request that names a file by its
// index, or writes 409 « files » if the interface displays another list.
func (s *Server) curFor(w http.ResponseWriter, r *http.Request) (current, bool) {
	c := s.cur()
	if v := r.Header.Get(filesHeader); v != "" && v != strconv.FormatInt(c.Gen, 10) {
		jsonError(w, http.StatusConflict, "files", "la liste des fichiers du projet a changé : l'affichage est actualisé, recommencez")
		return c, false
	}
	return c, true
}

// refreshEvery spaces the readings of the project: walking a big tree on a
// slow disk (/mnt/c from WSL: half a second for a large book) must not run at
// every poll of the interface (2 s).
var refreshEvery = 10 * time.Second

// refreshSoon starts a refresh in the background if none runs and the last
// one is old enough; the next poll sees its result.
func (s *Server) refreshSoon() {
	now := time.Now().UnixNano()
	if now-s.lastRefresh.Load() < int64(refreshEvery) || !s.refreshing.CompareAndSwap(false, true) {
		return
	}
	s.lastRefresh.Store(now)
	go func() {
		defer s.refreshing.Store(false)
		s.refresh()
	}()
}

// refresh reads the project again and takes the new list of files, title,
// filter or stylesheet if they changed. It keeps the current project if the
// new one cannot be read (a profile being edited).
func (s *Server) refresh() {
	cur := s.cur().P
	p, err := project.Load(cur.Source)
	if err != nil || (sameStrings(p.Files, cur.Files) && p.Title == cur.Title && p.Filter == cur.Filter && p.CSS == cur.CSS &&
		sameStrings(p.Resources, cur.Resources) && p.Pandoc == cur.Pandoc && p.PerFile == cur.PerFile && reflect.DeepEqual(p.Tasks, cur.Tasks)) {
		return
	}
	s.curMu.Lock()
	s.P, s.R, s.N = p, newRenderer(p), navette.New(p)
	s.gen++
	s.curMu.Unlock()
	// The decisions kept for undo name files by index: forgotten.
	s.undoMu.Lock()
	s.undos = nil
	s.undoMu.Unlock()
}

func sameStrings(a, b []string) bool {
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

// Handler returns the HTTP handler of the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /static/{name}", s.static)
	mux.HandleFunc("GET /static/polices/{name}", s.font)
	mux.HandleFunc("GET /project.css", s.projectCSS)
	mux.HandleFunc("GET /api/ping", s.ping)
	mux.HandleFunc("GET /api/since", s.since)
	mux.HandleFunc("GET /api/doc", s.doc)
	mux.HandleFunc("GET /api/hashes", s.hashes)
	mux.HandleFunc("POST /api/comment", s.comment)
	mux.HandleFunc("POST /api/withdraw", s.withdraw)
	mux.HandleFunc("POST /api/reply", s.reply)
	mux.HandleFunc("POST /api/amend", s.amend)
	mux.HandleFunc("GET /api/block", s.blockText)
	mux.HandleFunc("GET /api/passage", s.passageText)
	mux.HandleFunc("POST /api/edit", s.editBlock)
	mux.HandleFunc("POST /api/preview", s.preview)
	mux.HandleFunc("POST /api/decide", s.decide)
	mux.HandleFunc("POST /api/typo", s.typo)
	mux.HandleFunc("POST /api/undecide", s.undecide)
	mux.HandleFunc("POST /api/revise", s.revise)
	mux.HandleFunc("POST /api/fix", s.applyFix)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/log", s.log)
	mux.HandleFunc("GET /api/resolve", s.resolve)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("POST /api/handoff", s.handoff)
	mux.HandleFunc("POST /api/reclaim", s.reclaim)
	mux.HandleFunc("POST /api/link", s.link)
	s.agentRoutes(mux)
	s.homeRoutes(mux)
	s.runRoutes(mux)
	mux.HandleFunc("GET /api/progress", s.getProgress)
	mux.HandleFunc("POST /api/progress", s.setProgress)
	mux.HandleFunc("GET /api/drafts", s.getDrafts)
	mux.HandleFunc("POST /api/drafts", s.setDraft)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("POST /api/settings", s.setSettings)
	mux.HandleFunc("POST /api/stop", s.stop)
	mux.HandleFunc("GET /", s.resource)
	return s.guard(mux)
}

// guard enforces D-07 on every request: known Host, valid session, same
// origin for changes, and restrictive headers.
func (s *Server) guard(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"127.0.0.1:" + strconv.Itoa(s.Port): true,
		"localhost:" + strconv.Itoa(s.Port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if !allowed[r.Host] {
			http.Error(w, "hôte refusé", http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && !allowed[strings.TrimPrefix(o, "http://")] {
				jsonError(w, http.StatusForbidden, "origin", "origine refusée")
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				jsonError(w, http.StatusForbidden, "origin", "requête d'un autre site refusée")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				jsonError(w, http.StatusUnsupportedMediaType, "content-type", "JSON attendu")
				return
			}
		}
		if !public(r) && !s.authorized(r) {
			jsonError(w, http.StatusUnauthorized, "token", "session absente ou périmée : ouvrez l'adresse affichée par « glose open »")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// public reports whether a request may be served without the token: the
// interface itself (its code is not secret). The text, the annotations and
// the images of the project always require it.
func public(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	return p == "/" || p == "/set" || p == "/home" || strings.HasPrefix(p, "/static/")
}

func (s *Server) tokenOK(t string) bool {
	return len(t) == len(s.Token) && subtle.ConstantTimeCompare([]byte(t), []byte(s.Token)) == 1
}

// authorized checks the token: in the X-Glose-Token header (requests of the
// interface and of the command line), or in the query for an image, which
// the browser loads without headers. There is no cookie: a cookie of
// 127.0.0.1 would be sent to every other port of the machine.
func (s *Server) authorized(r *http.Request) bool {
	if s.tokenOK(r.Header.Get("X-Glose-Token")) {
		return true
	}
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(r.URL.Path, "/") && !strings.HasPrefix(r.URL.Path, "/api/") &&
		s.tokenOK(r.URL.Query().Get("t"))
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(web.Files, "index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}

var staticTypes = map[string]string{"app.js": "text/javascript; charset=utf-8", "visuel.js": "text/javascript; charset=utf-8",
	"accueil.js":         "text/javascript; charset=utf-8",
	"brouillons.js":      "text/javascript; charset=utf-8",
	"donnees.js":         "text/javascript; charset=utf-8",
	"reglages.js":        "text/javascript; charset=utf-8",
	"navette.js":         "text/javascript; charset=utf-8",
	"interface.js":       "text/javascript; charset=utf-8",
	"historique.js":      "text/javascript; charset=utf-8",
	"edition.js":         "text/javascript; charset=utf-8",
	"export.js":          "text/javascript; charset=utf-8",
	"espace.js":          "text/javascript; charset=utf-8",
	"themes.js":          "text/javascript; charset=utf-8",
	"themes.css":         "text/css; charset=utf-8",
	"export.css":         "text/css; charset=utf-8",
	"saisie.js":          "text/javascript; charset=utf-8",
	"colonne.js":         "text/javascript; charset=utf-8",
	"marge.js":           "text/javascript; charset=utf-8",
	"selection.js":       "text/javascript; charset=utf-8",
	"navigation.js":      "text/javascript; charset=utf-8",
	"recherche.js":       "text/javascript; charset=utf-8",
	"archives.js":        "text/javascript; charset=utf-8",
	"agents.js":          "text/javascript; charset=utf-8",
	"reglages-agents.js": "text/javascript; charset=utf-8",
	"icones.js":          "text/javascript; charset=utf-8",
	"style.css":          "text/css; charset=utf-8"}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctype, ok := staticTypes[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(web.Files, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Write(b)
}

// font serves an embedded font of the interface (D-53).
func (s *Server) font(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !strings.HasSuffix(name, ".woff2") {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(web.Files, "polices/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	w.Write(b)
}

func (s *Server) projectCSS(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	if c.P.CSS == "" {
		return
	}
	b, err := os.ReadFile(c.P.CSS)
	if err != nil {
		return
	}
	w.Write(b)
}

func (s *Server) resource(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	path, ctype, err := c.P.Resource(strings.TrimPrefix(r.URL.Path, "/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", ctype)
	if ctype == "image/svg+xml" {
		// A served SVG must not run scripts of its own.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	}
	io.Copy(w, f)
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "root": c.P.Root, "source": c.P.Source, "pid": os.Getpid(), "build": buildAtStart})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	s.stopOnce.Do(func() { close(s.Stop) })
}

func (s *Server) hashes(w http.ResponseWriter, r *http.Request) {
	s.refreshSoon()
	c := s.cur()
	files := []map[string]any{}
	for i := range c.P.Files {
		d, err := store.Read(c.P.Path(i))
		h := ""
		if err == nil {
			h = d.Hash
		}
		files = append(files, map[string]any{"index": i, "hash": h})
	}
	// The sequence number of the last event of the journal: the interface
	// reloads the state of the exchange when it changes (a handover without
	// any change of the files, for instance).
	seq, _ := c.N.J.LastSeq()
	// The presence of the agent changes without any change of the files or
	// of the journal (P10.1): it comes with each poll.
	hand, since, turn := s.handAt(c, seq)
	out := map[string]any{"files": files, "seq": seq, "filesGen": c.Gen, "agent": c.N.Agent(hand, since)}
	ai := s.agentInfoOf(c, turn)
	out["tasks"] = ai.Tasks
	out["agentsConfigured"], out["textAgent"], out["textRequires"], out["run"] = ai.AgentsConfigured, ai.TextAgent, ai.TextRequires, ai.Run
	out["passes"], out["aliases"], out["link"], out["owner"] = ai.Passes, ai.Aliases, ai.Link, ai.Owner
	writeJSON(w, http.StatusOK, out)
}

// docResponse is the rendered project, with the state of the exchange.
type docResponse struct {
	TaskLabels map[string]missionLabel `json:"taskLabels"`
	*render.Doc
	State    navette.State `json:"state"`
	Seq      int64         `json:"seq"`
	FilesGen int64         `json:"filesGen"` // generation of the list of files (filesHeader)
	agentInfo
}

func (s *Server) doc(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	view := projection.Current
	switch r.URL.Query().Get("view") {
	case "", "current":
	case "proposed":
		view = projection.Proposed
	default:
		jsonError(w, http.StatusBadRequest, "view", "vue inconnue")
		return
	}
	// The part of the project to render (P6.8): "" for all of it, a file,
	// a directory ending with « / », or render.AutoPart.
	d, err := c.R.RenderPart(ctx, view, r.URL.Query().Get("part"))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "render", err.Error())
		return
	}
	st, h, files, err := c.N.State()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "journal", err.Error())
		return
	}
	// Rendering runs without the write lock (pandoc may take time). A newer
	// state must not announce a handover alongside a pre-handover document.
	// Refuse that mixed snapshot; the disk watcher will request a fresh one.
	for _, f := range d.Files {
		if f.Index < 0 || f.Index >= len(files.Docs) || f.Hash != files.Docs[f.Index].Hash {
			jsonError(w, http.StatusConflict, "stale-render", "le texte a changé pendant le rendu : l'affichage doit être actualisé")
			return
		}
	}
	navette.Annotate(d.Annotations, h)
	var seq int64
	if len(h.Events) > 0 {
		seq = h.Events[len(h.Events)-1].Seq
	}
	writeJSON(w, http.StatusOK, docResponse{Doc: d, State: st, Seq: seq, FilesGen: c.Gen, TaskLabels: missionLabels(h), agentInfo: s.agentInfoOf(c, st.Turn)})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	st, _, _, err := c.N.State()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "journal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// log gives the history of the exchange, part by part (the tab « Navette »;
// the same as « glose log --json »).
func (s *Server) log(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	h, err := c.N.History()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "journal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": h.Log(), "hand": h.Hand()})
}

type handoffRequest struct {
	TaskHash      string   `json:"taskHash"`
	Task          string   `json:"task"`
	Files         []string `json:"files"`
	ConfirmAccess bool     `json:"confirmAccess"`
	Note          string   `json:"note"`
	Force         bool     `json:"force"` // reclaim: without converting the agent's changes
	// Launch: launch the agent of the text (P10.5); absent, it is launched
	// if one is configured and none listens.
	Launch *bool `json:"launch"`
}

// handoff ends the author's turn (SPEC F13): the button « Remettre à l'agent ».
func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	var req handoffRequest
	if !decode(w, r, &req) {
		return
	}
	var task *journal.TaskInfo
	if req.Task != "" {
		var err error
		task, err = s.resolveTask(c, req.Task, req.Files)
		if err != nil {
			jsonError(w, 400, "task", err.Error())
			return
		}
		if req.TaskHash != taskHash(task) {
			jsonError(w, 409, "task-changed", "la consigne ou ses accès ont changé : choisissez-la de nouveau ; votre note est conservée")
			return
		}
		if !req.ConfirmAccess {
			jsonError(w, 400, "access", "confirmez les accès et leurs limites avant le lancement")
			return
		}
		yes := true
		req.Launch = &yes
	}
	s.handover(w, req, func(note string) (*navette.Handover, error) {
		// What Glose launches is decided first; the plan of passes and the
		// owner of the turn are written in the transaction of the handover,
		// under the lock of the project: a bound session that waits sees
		// them as soon as it can take the hand (P10.6, P10.9).
		next := s.beforeHandoff(c, req.Launch)
		if task != nil {
			cfg, _ := agents.Load()
			next = launchPlan{cfg: cfg, single: &task.Agent}
		}
		ho, err := c.N.HandoffTaskThen(note, task, func(turn int) (func(), error) { return s.prepareTurn(c, &next, turn) })
		if err == nil {
			s.afterHandoff(c, next)
		}
		return ho, err
	})
}

// reclaim lets the author take the hand back while the agent has it.
func (s *Server) reclaim(w http.ResponseWriter, r *http.Request) {
	c := s.cur()
	var req handoffRequest
	if !decode(w, r, &req) {
		return
	}
	s.handover(w, req, func(note string) (*navette.Handover, error) { return c.N.Reclaim(note, req.Force) })
}

func (s *Server) handover(w http.ResponseWriter, req handoffRequest, run func(string) (*navette.Handover, error)) {
	note := strings.TrimSpace(req.Note)
	if len(note) > maxNote || !utf8.ValidString(note) {
		jsonError(w, http.StatusBadRequest, "text", "note trop longue ou mal encodée")
		return
	}
	ho, err := run(note)
	var te *navette.TrackError
	switch {
	case errors.As(err, &te):
		var problems []string
		for _, p := range te.Problems {
			problems = append(problems, p.String())
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "track", "problems": problems,
			"message": "Les modifications de l'agent ne peuvent pas être converties en propositions."})
		return
	case errors.Is(err, navette.ErrAgentTurn), errors.Is(err, navette.ErrNotYourTurn):
		jsonError(w, http.StatusConflict, "turn", err.Error())
		return
	case err != nil:
		jsonError(w, http.StatusInternalServerError, "handoff", err.Error())
		return
	}
	out := map[string]any{"ok": true, "turn": ho.Event.TurnEv.Number, "identified": ho.Identified,
		"converted": ho.Converted, "files": ho.Files}
	if st, ok := s.runner.Status(s.cur().P.State()); ok {
		out["run"] = st
	}
	if msg := s.lastLaunchError(); msg != "" {
		out["launchError"] = msg
	}
	writeJSON(w, http.StatusOK, out)
}

// commentRequest is the intention sent by the interface.
type commentRequest struct {
	// Recipient is the short name of the agent that must answer (D-49,
	// P10.6); "" for the default one. The key « to » is taken by /api/edit.
	Recipient string `json:"recipient"`
	File      int    `json:"file"`
	Hash      string `json:"hash"`
	Mode      string `json:"mode"` // passage, point, paragraph
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Text      string `json:"text"`
}

// maxNote bounds the length of a note, in bytes.
const maxNote = 20000

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	var req commentRequest
	if !decode(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || len(text) > maxNote || !utf8.ValidString(text) {
		jsonError(w, http.StatusBadRequest, "text", "remarque vide, trop longue ou mal encodée")
		return
	}
	r, ok := withRecipient(w, r, req.Recipient)
	if !ok {
		return
	}
	s.edit(w, r, req.File, req.Hash, "comment", "", func(src []byte, m *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		var span critic.Span
		switch req.Mode {
		case "passage":
			res, err := m.Check(src, req.Start, req.End)
			if err != nil {
				return patch.Edit{}, fallback(m, req.Start, err)
			}
			if res.Widened {
				return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "widen",
					message: "La sélection coupe un élément de mise en forme (gras, lien, formule…). Le passage doit être élargi.",
					extra:   map[string]any{"start": res.Start, "end": res.End, "excerpt": string(src[res.Start:res.End])}}
			}
			span = critic.Span{Start: res.Start, End: res.End}
		case "point":
			if err := m.CheckPoint(src, req.Start); err != nil {
				return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "boundary", message: err.Error()}
			}
			span = critic.Span{Start: req.Start, End: req.Start}
		case "paragraph":
			at, err := m.BlockEnd(req.Start)
			if err != nil {
				return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "boundary", message: err.Error()}
			}
			span = critic.Span{Start: at, End: at}
		default:
			return patch.Edit{}, &apiError{status: http.StatusBadRequest, code: "mode", message: "mode inconnu"}
		}
		e, err := review.Comment(src, span, text, review.Author, gen)
		if err != nil {
			if req.Mode == "passage" {
				return patch.Edit{}, fallback(m, span.Start, err)
			}
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		return e, nil
	})
}

type withdrawRequest struct {
	File  int    `json:"file"`
	Hash  string `json:"hash"`
	Start int    `json:"start"`
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	var req withdrawRequest
	if !decode(w, r, &req) {
		return
	}
	s.edit(w, r, req.File, req.Hash, "withdraw", navette.Withdraw, func(src []byte, _ *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		a, marks, err := patch.Find(src, req.Start)
		if err != nil {
			// Not an annotation: perhaps a reply inside a thread (undo of a reply).
			if e, err := review.RemoveReply(src, req.Start); err == nil {
				return e, nil
			}
			return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "annotation introuvable"}
		}
		// Until the identity of annotations (D-19), the interface may only
		// withdraw discussions, never a proposal: deciding is P2 (D-14).
		if k := marks[a.Main].Kind; k != critic.Comment && k != critic.Highlight {
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "kind",
				message: "seuls les commentaires peuvent être retirés pour l'instant"}
		}
		e, err := patch.Withdraw(src, marks, a)
		if err != nil {
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		return e, nil
	})
}

// edit runs one change of the author through the exchange (package navette):
// under the write lock, with the turn and the hash checked, the edit
// computed, then recorded in the journal and written. kind and decision name
// the operation in the journal.
func (s *Server) edit(w http.ResponseWriter, r *http.Request, file int, hash, kind, decision string,
	compute func(src []byte, m *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError)) {
	s.editAt(w, r, file, hash, nil, kind, decision, compute)
}

// editAt is edit; if at is not nil, the range it holds after compute is
// returned as "start" and "end" (the place of a reply, the rewriting itself
// without its linked notes), instead of the range of the edit.
func (s *Server) editAt(w http.ResponseWriter, r *http.Request, file int, hash string, at *critic.Span, kind, decision string,
	compute func(src []byte, m *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError)) {
	s.editWith(w, r, file, hash, at, kind, decision, func(src []byte, m *sourcemap.Map, gen *review.Gen, _ *navette.History) (patch.Edit, *apiError) {
		return compute(src, m, gen)
	})
}

// editWith is editAt, with the history of the exchange, read under the lock,
// passed to compute.
func (s *Server) editWith(w http.ResponseWriter, r *http.Request, file int, hash string, at *critic.Span, kind, decision string,
	compute func(src []byte, m *sourcemap.Map, gen *review.Gen, h *navette.History) (patch.Edit, *apiError)) {
	s.editSpec(w, r, file, hash, at, &navette.Spec{Kind: kind, Decision: decision}, compute)
}

// editSpec is editWith, for the operation that spec describes; compute may
// complete spec (navette.Run).
func (s *Server) editSpec(w http.ResponseWriter, r *http.Request, file int, hash string, at *critic.Span, spec *navette.Spec,
	compute func(src []byte, m *sourcemap.Map, gen *review.Gen, h *navette.History) (patch.Edit, *apiError)) {
	c, ok := s.curFor(w, r)
	if !ok {
		return
	}
	if file < 0 || file >= len(c.P.Files) {
		jsonError(w, http.StatusBadRequest, "file", "fichier inconnu")
		return
	}
	if hash == "" {
		jsonError(w, http.StatusBadRequest, "hash", "empreinte du fichier absente")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	var apiErr *apiError
	res, err := c.N.Run(review.Author, file, hash, spec, func(src []byte, gen *review.Gen, h *navette.History) (patch.Edit, error) {
		m, err := c.R.Map(ctx, file, store.Doc{Bytes: src, Hash: store.Hash(src)})
		if err != nil {
			apiErr = &apiError{status: http.StatusUnprocessableEntity, code: "render", message: err.Error()}
			return patch.Edit{}, apiErr
		}
		e, ae := compute(src, m, gen, h)
		if ae != nil {
			apiErr = ae
			return patch.Edit{}, ae
		}
		// The agent that must answer what this edit creates (D-49).
		if to := recipientOf(r); to != "" {
			addressed, err := review.Address(src, e, to)
			if err != nil {
				apiErr = &apiError{status: http.StatusUnprocessableEntity, code: "recipient", message: err.Error()}
				return patch.Edit{}, apiErr
			}
			e = addressed
		}
		return e, nil
	})
	switch {
	case apiErr != nil:
		apiErr.write(w)
		return
	case errors.Is(err, navette.ErrConflict):
		jsonError(w, http.StatusConflict, "conflict", "le fichier a changé sur le disque depuis l'affichage")
		return
	case errors.Is(err, navette.ErrAgentTurn):
		jsonError(w, http.StatusConflict, "turn", "C'est le tour de l'agent : attendez qu'il remette le sien, ou reprenez la main.")
		return
	case errors.Is(err, store.ErrLocked), errors.Is(err, context.DeadlineExceeded):
		jsonError(w, http.StatusServiceUnavailable, "lock", "le projet est occupé par une autre écriture")
		return
	case err != nil:
		jsonError(w, http.StatusInternalServerError, "write", err.Error())
		return
	}
	start, end := res.Edit.Start, res.Edit.Start+len(res.Edit.Insert)
	if at != nil {
		start = at.Start
		if at.End > at.Start {
			end = at.End
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hash": res.Hash, "start": start, "end": end})
}

// fallback answers a refused passage comment with the end of its paragraph,
// where the interface may place the comment instead, quoting the passage
// (SPEC F6).
func fallback(m *sourcemap.Map, at int, err error) *apiError {
	e := &apiError{status: http.StatusUnprocessableEntity, code: "fallback", message: message(err)}
	if end, berr := m.BlockEnd(at); berr == nil {
		e.extra = map[string]any{"at": end}
	}
	return e
}

// message gives the French message of an error of patch or sourcemap.
func message(err error) string {
	switch {
	case errors.Is(err, patch.ErrOverlap):
		return "le passage touche une annotation existante"
	case errors.Is(err, patch.ErrAdjacent):
		return "une annotation existante est collée à cet endroit"
	case errors.Is(err, patch.ErrDelimiter):
		return "le texte contient une suite de signes réservée aux annotations ({++, {>>, <<}…)"
	case errors.Is(err, patch.ErrNeighbour):
		return "une annotation mal formée se trouve dans le voisinage : corrigez-la d'abord dans le fichier"
	case errors.Is(err, patch.ErrNested):
		return "l'annotation contient une annotation imbriquée : corrigez-la dans le fichier"
	case errors.Is(err, patch.ErrArrow):
		return "le texte supprimé contient « ~> » : il ne peut pas devenir un remplacement ; rejetez la proposition et proposez de nouveau"
	case errors.Is(err, navette.ErrDirectMarks):
		return "la modification toucherait une annotation existante : décidez-la d'abord, ou ajoutez une suggestion"
	case errors.Is(err, patch.ErrKind):
		return "seule une proposition (ajout, suppression, remplacement) se corrige"
	}
	return err.Error()
}

type apiError struct {
	status  int
	code    string
	message string
	extra   map[string]any
}

func (e *apiError) Error() string { return e.message }

func (e *apiError) write(w http.ResponseWriter) {
	body := map[string]any{"error": e.code, "message": e.message}
	for k, v := range e.extra {
		body[k] = v
	}
	writeJSON(w, e.status, body)
}

func jsonError(w http.ResponseWriter, status int, code, msg string) {
	(&apiError{status: status, code: code, message: msg}).write(w)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		jsonError(w, http.StatusBadRequest, "json", fmt.Sprintf("requête illisible : %v", err))
		return false
	}
	return true
}

// Listen opens a listener on 127.0.0.1 (D-07), on a free port.
func Listen() (net.Listener, int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return l, l.Addr().(*net.TCPAddr).Port, nil
}

// blockText gives the source of the block that contains offset at, for the
// editor of the interface (SPEC F7, F8).
func (s *Server) blockText(w http.ResponseWriter, r *http.Request) {
	c, ok := s.curFor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	file, err1 := strconv.Atoi(q.Get("file"))
	at, err2 := strconv.Atoi(q.Get("at"))
	if err1 != nil || err2 != nil || file < 0 || file >= len(c.P.Files) {
		jsonError(w, http.StatusBadRequest, "query", "requête invalide")
		return
	}
	d, err := store.Read(c.P.Path(file))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read", err.Error())
		return
	}
	if d.Hash != q.Get("hash") {
		jsonError(w, http.StatusConflict, "conflict", "le fichier a changé sur le disque depuis l'affichage")
		return
	}
	m, err := c.R.Map(r.Context(), file, d)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "render", err.Error())
		return
	}
	start, end, err := m.BlockRange(d.Bytes, at)
	if q.Get("to") != "" {
		// A structure (table, list, quotation, several paragraphs): the whole
		// lines from the word at to the word that ends at to.
		to, err2 := strconv.Atoi(q.Get("to"))
		if err2 != nil {
			jsonError(w, http.StatusBadRequest, "query", "requête invalide")
			return
		}
		start, end, err = m.LinesRange(d.Bytes, at, to)
	}
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "boundary", err.Error())
		return
	}
	marks, _ := critic.Parse(d.Bytes)
	for _, mk := range marks {
		if mk.Span.Start < end && mk.Span.End > start {
			jsonError(w, http.StatusUnprocessableEntity, "marked", edit.ErrMarked.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"start": start, "end": end, "text": string(d.Bytes[start:end])})
}

type editRequest struct {
	Recipient string `json:"recipient"` // see commentRequest
	File      int    `json:"file"`
	Hash      string `json:"hash"`
	Mode      string `json:"mode"`  // block (default), passage, or lines (a structure, whole lines)
	At        int    `json:"at"`    // block: a word of the block (it may start with an absorbed attribute); lines: the first word
	To        int    `json:"to"`    // lines, insert: the end of the last word
	Where     string `json:"where"` // insert: before or after the element
	Tight     bool   `json:"tight"` // insert: one line ending (a list item), not a blank line
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Text      string `json:"text"`
	Note      string `json:"note"`  // optional message attached to the proposal
	Whole     bool   `json:"whole"` // one proposal for the whole rewriting (D-32)
	// Direct applies the rewriting to the text at once, without proposals
	// (D-57): the author's own change, which the agent does not discuss.
	Direct bool `json:"direct"`
}

// editBlock records the rewriting of a block as proposals.
func (s *Server) editBlock(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	if !decode(w, r, &req) {
		return
	}
	note := strings.TrimSpace(req.Note)
	if len(req.Text) > 1<<20 || !utf8.ValidString(req.Text) || len(note) > maxNote || !utf8.ValidString(note) {
		jsonError(w, http.StatusBadRequest, "text", "texte trop long ou mal encodé")
		return
	}
	if req.Direct {
		s.editDirect(w, r, req, note)
		return
	}
	r, ok := withRecipient(w, r, req.Recipient)
	if !ok {
		return
	}
	var main critic.Span
	s.editAt(w, r, req.File, req.Hash, &main, "propose", "", func(src []byte, m *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		e, rng, apiErr := proposeEdit(req, note, src, m, gen)
		main = rng
		return e, apiErr
	})
}

// editDirect applies a rewriting of the author to the text at once (D-57):
// the proposals it would make are computed as for a suggestion, then
// accepted in the same operation, so that no mark of them is ever written.
// The journal keeps a copy of each change (decision « direct »). A message
// for the agent asks for an answer: it goes with a suggestion, not with a
// direct change. The change is undone like a decision (undecide).
func (s *Server) editDirect(w http.ResponseWriter, r *http.Request, req editRequest, note string) {
	if note != "" || req.Recipient != "" {
		jsonError(w, http.StatusBadRequest, "note",
			"une modification directe ne s'adresse pas à l'agent : retirez le message, ou ajoutez une suggestion")
		return
	}
	spec := &navette.Spec{Kind: navette.Direct}
	s.editSpec(w, r, req.File, req.Hash, nil, spec, func(src []byte, m *sourcemap.Map, gen *review.Gen, _ *navette.History) (patch.Edit, *apiError) {
		e, _, apiErr := proposeEdit(req, "", src, m, gen)
		if apiErr != nil {
			return patch.Edit{}, apiErr
		}
		out, copies, err := navette.Impose(src, e.Apply(src))
		if err != nil {
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		spec.Direct = copies
		final := review.Diff(src, out)
		ids := make([]string, 0, len(copies))
		for _, c := range copies {
			ids = append(ids, c.ID)
		}
		s.rememberDirect(req.File, src, final, ids)
		return final, nil
	})
}

// preview computes the proposals of a rewriting without writing anything, and
// renders the result (view « Proposed » of the rewritten passage, with the
// notes it calls): the « Aperçu » of the editor.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	c, ok := s.curFor(w, r)
	if !ok {
		return
	}
	var req editRequest
	if !decode(w, r, &req) {
		return
	}
	if req.File < 0 || req.File >= len(c.P.Files) || len(req.Text) > 1<<20 || !utf8.ValidString(req.Text) {
		jsonError(w, http.StatusBadRequest, "text", "requête invalide")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	d, err := store.Read(c.P.Path(req.File))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read", err.Error())
		return
	}
	if d.Hash != req.Hash {
		jsonError(w, http.StatusConflict, "conflict", "le fichier a changé sur le disque depuis l'affichage")
		return
	}
	m, err := c.R.Map(ctx, req.File, d)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "render", err.Error())
		return
	}
	e, main, apiErr := proposeEdit(req, "", d.Bytes, m, review.NewGen(nil))
	if apiErr != nil {
		apiErr.write(w)
		return
	}
	out := e.Apply(d.Bytes)
	// The changes, as the author will decide them.
	changes := []map[string]any{}
	marks, _ := critic.Parse(out)
	links := notes.Links(out)
	for _, mk := range marks {
		if mk.Kind == critic.Comment || mk.Span.Start < e.Start || mk.Span.End > e.Start+len(e.Insert) {
			continue
		}
		if l, ok := links[mk.Span.Start]; ok { // a note: one change
			if l.Primary {
				changes = append(changes, map[string]any{"kind": "note", "text": l.Note, "deleted": mk.Kind == critic.Deletion})
			}
			continue
		}
		c := map[string]any{"kind": render.KindName(mk.Kind), "text": string(out[mk.Text.Start:mk.Text.End])}
		if mk.Kind == critic.Substitution {
			c["new"] = string(out[mk.New.Start:mk.New.End])
		}
		changes = append(changes, c)
	}
	// The rewritten passage as it will read, and the notes it calls.
	md := projection.Project(out[main.Start:main.End], projection.Proposed).Text
	whole := projection.Project(out, projection.Proposed).Text
	var labels []string
	for l := range notes.Refs(md) {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	for _, l := range labels {
		if sp, ok := notes.Definition(whole, l); ok {
			md = append(append(md, "\n\n"...), bytes.TrimSpace(whole[sp.Start:sp.End])...)
		}
	}
	html, err := c.R.Preview(ctx, req.File, md)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "render", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": html, "changes": changes})
}

type decideRequest struct {
	Via      string `json:"via,omitempty"`
	File     int    `json:"file"`
	Hash     string `json:"hash"`
	Start    int    `json:"start"`
	End      int    `json:"end"` // > Start: every proposal inside [Start, End)
	Decision string `json:"decision"`
}

// decide accepts or rejects a proposal, or every proposal of a range (to undo
// a rewriting, or to decide a whole file, SPEC F12), or resolves a discussion.
// Only the interface, that is the author, decides (D-14): the command line
// offers no such command.
func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var req decideRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Via != "" && req.Via != "Typo" {
		jsonError(w, 400, "via", "signature de lot inconnue")
		return
	}
	switch req.Decision {
	case navette.Accept, navette.Reject:
		s.edit(w, r, req.File, req.Hash, "decide", req.Decision, func(src []byte, _ *sourcemap.Map, _ *review.Gen) (patch.Edit, *apiError) {
			e, err := navette.DecideVia(src, req.Start, req.End, req.Decision, req.Via)
			if err == nil {
				s.remember(req.File, src, e)
			}
			if errors.Is(err, navette.ErrNoProposal) {
				return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "proposition introuvable"}
			}
			if err != nil {
				return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
			}
			return e, nil
		})
	case navette.Resolve:
		s.edit(w, r, req.File, req.Hash, "resolve", navette.Resolve, func(src []byte, _ *sourcemap.Map, _ *review.Gen) (patch.Edit, *apiError) {
			e, err := navette.ResolveAt(src, req.Start)
			if err == nil {
				s.remember(req.File, src, e)
			}
			if errors.Is(err, review.ErrNotFound) {
				return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "annotation introuvable"}
			}
			if err != nil {
				return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
			}
			return e, nil
		})
	default:
		jsonError(w, http.StatusBadRequest, "decision", "décision inconnue")
	}
}

type reviseRequest struct {
	File  int    `json:"file"`
	Hash  string `json:"hash"`
	Start int    `json:"start"` // start of the proposal
	Text  string `json:"text"`  // the text it proposes now
	Note  string `json:"note"`  // optional message, added to its thread
}

// revise corrects one of the author's own proposals, still pending (P3.2):
// the mark stays one, with its identity and its thread; an optional message
// joins the thread.
func (s *Server) revise(w http.ResponseWriter, r *http.Request) {
	var req reviseRequest
	if !decode(w, r, &req) {
		return
	}
	note := strings.TrimSpace(req.Note)
	if len(req.Text) > 1<<20 || !utf8.ValidString(req.Text) || len(note) > maxNote || !utf8.ValidString(note) {
		jsonError(w, http.StatusBadRequest, "text", "texte trop long ou mal encodé")
		return
	}
	var at critic.Span
	s.editAt(w, r, req.File, req.Hash, &at, "revise", "", func(src []byte, _ *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		e, mark, err := review.Revise(src, req.Start, "", req.Text, review.Author, gen)
		switch {
		case errors.Is(err, review.ErrNotFound):
			return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "proposition introuvable"}
		case errors.Is(err, patch.ErrSame):
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "same", message: err.Error()}
		case errors.Is(err, patch.ErrEmpty):
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "empty",
				message: "un ajout ne peut pas devenir vide : rejetez plutôt la proposition"}
		case err != nil:
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		at = mark
		if note == "" {
			return e, nil
		}
		out := e.Apply(src)
		r, _, err := review.Reply(out, mark.Start, "", note, review.Author, gen)
		if err != nil {
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		full := r.Apply(out)
		return review.Diff(src, full).Cover(src, full, e.Start, e.End), nil
	})
}

// undoEntry is the state of a file before a decision of the author, and the
// hash it has just after: undecide puts the text back as it was, provided
// nothing has changed since.
type undoEntry struct {
	file      int
	hashAfter string
	before    []byte
	edit      patch.Edit // the decision itself, to check that undoing it touches nothing else
	direct    []string   // a direct change (D-57): the identifiers of its copies in the journal
}

// maxUndos bounds the decisions kept for undo (the session of the interface).
const maxUndos = 50

// remember keeps the state of the file before a decision. If the decision is
// not written after all (conflict, turn), its entry never matches a hash.
func (s *Server) remember(file int, src []byte, e patch.Edit) {
	s.rememberDirect(file, src, e, nil)
}

// rememberDirect is remember, for a direct change of the author (D-57) when
// ids is not empty: undoing it is recorded as such in the journal.
func (s *Server) rememberDirect(file int, src []byte, e patch.Edit, ids []string) {
	after := e.Apply(src)
	s.undoMu.Lock()
	defer s.undoMu.Unlock()
	s.undos = append(s.undos, undoEntry{file: file, hashAfter: store.Hash(after), before: append([]byte(nil), src...), edit: e, direct: ids})
	if len(s.undos) > maxUndos {
		s.undos = s.undos[len(s.undos)-maxUndos:]
	}
}

type undecideRequest struct {
	File int    `json:"file"`
	Hash string `json:"hash"` // the hash right after the decision
}

// undecide undoes the last decision of the author on a file (accept, reject,
// resolve, or a decision in bulk), if the file has not changed since: the
// annotations come back with their identity and their thread. It is the
// author's own decision that is withdrawn (D-14 untouched: the agent has no
// access to it). The journal records the operation (kind « undo »).
func (s *Server) undecide(w http.ResponseWriter, r *http.Request) {
	c, ok := s.curFor(w, r)
	if !ok {
		return
	}
	var req undecideRequest
	if !decode(w, r, &req) {
		return
	}
	s.undoMu.Lock()
	idx := -1
	for i := len(s.undos) - 1; i >= 0; i-- {
		if s.undos[i].file == req.File && s.undos[i].hashAfter == req.Hash {
			idx = i
			break
		}
	}
	var u undoEntry
	if idx >= 0 {
		u = s.undos[idx]
	}
	s.undoMu.Unlock()
	if idx < 0 {
		jsonError(w, http.StatusConflict, "conflict", "cette décision ne peut plus être annulée : le fichier a changé depuis")
		return
	}
	var at critic.Span
	spec := &navette.Spec{Kind: navette.Undo, Reverted: u.direct}
	s.editSpec(w, r, req.File, req.Hash, &at, spec, func(src []byte, _ *sourcemap.Map, _ *review.Gen, _ *navette.History) (patch.Edit, *apiError) {
		// The inverse of the decision: what it wrote, replaced by what it removed.
		inv := patch.Edit{Start: u.edit.Start, End: u.edit.Start + len(u.edit.Insert), Insert: u.before[u.edit.Start:u.edit.End]}
		if !bytes.Equal(inv.Apply(src), u.before) {
			return patch.Edit{}, &apiError{status: http.StatusConflict, code: "conflict", message: "le fichier a changé depuis cette décision"}
		}
		at = critic.Span{Start: u.edit.Start, End: u.edit.End}
		return inv, nil
	})
	// Written (the file is back as it was): the entry is used up.
	if req.File < 0 || req.File >= len(c.P.Files) {
		return
	}
	if d, err := store.Read(c.P.Path(req.File)); err == nil && d.Hash == store.Hash(u.before) {
		// Searched again: the list may have changed during the write.
		s.undoMu.Lock()
		for i := len(s.undos) - 1; i >= 0; i-- {
			if s.undos[i].file == req.File && s.undos[i].hashAfter == u.hashAfter {
				s.undos = append(s.undos[:i], s.undos[i+1:]...)
				break
			}
		}
		s.undoMu.Unlock()
	}
}

type fixRequest struct {
	File    int    `json:"file"`
	Hash    string `json:"hash"`
	Start   int    `json:"start"`   // start of the proposal
	Message int    `json:"message"` // start of the correction in its thread
	Accept  bool   `json:"accept"`  // accept the proposal so corrected, at once
}

// applyFix applies a correction that the agent attached to a proposal of the
// author (review.Meta.Fix): the proposal now proposes the agent's text (a
// correction of the author, P3.2), the correction message leaves the thread
// (the journal keeps it), and, if asked, the proposal is accepted. It is the
// author who decides (D-14): the agent only suggested.
func (s *Server) applyFix(w http.ResponseWriter, r *http.Request) {
	var req fixRequest
	if !decode(w, r, &req) {
		return
	}
	kind, decision := "revise", ""
	if req.Accept {
		kind, decision = "decide", navette.Accept
	}
	s.edit(w, r, req.File, req.Hash, kind, decision, func(src []byte, _ *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		refused := func(err error) *apiError {
			return &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		d := review.Read(src)
		a, ok := d.At(req.Start)
		if !ok {
			return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "proposition introuvable"}
		}
		var fix *review.Message
		for _, m := range a.Replies() {
			if m.Span.Start == req.Message && m.Fix {
				m := m
				fix = &m
			}
		}
		if fix == nil {
			return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "correction introuvable"}
		}
		text := review.FixText(src[fix.Mark.Text.Start:fix.Mark.Text.End])
		// 1. The correction leaves the thread.
		e1, err := review.RemoveReply(src, fix.Span.Start)
		if err != nil {
			return patch.Edit{}, refused(err)
		}
		out := e1.Apply(src)
		// 2. The proposal proposes the agent's text (already the same: nothing to do).
		if e2, _, err := review.Revise(out, a.Span.Start, "", text, review.Author, gen); err == nil {
			out = e2.Apply(out)
		} else if !errors.Is(err, patch.ErrSame) {
			return patch.Edit{}, refused(err)
		}
		// 3. Accepted, if asked.
		if req.Accept {
			e3, err := navette.DecideRange(out, a.Span.Start, a.Span.Start, navette.Accept)
			if err != nil {
				return patch.Edit{}, refused(err)
			}
			s.remember(req.File, src, review.Diff(src, e3.Apply(out)))
			out = e3.Apply(out)
		}
		return review.Diff(src, out), nil
	})
}

type amendRequest struct {
	File  int    `json:"file"`
	Hash  string `json:"hash"`
	Start int    `json:"start"` // start of the message
	Text  string `json:"text"`
}

// amend rewrites one of the author's own messages (a comment, the remark of
// a highlighted passage, a reply) not handed over to the agent yet (P9.1):
// the message keeps its place, its identity and its thread; the journal keeps
// its previous version. A message already handed over is answered instead.
func (s *Server) amend(w http.ResponseWriter, r *http.Request) {
	var req amendRequest
	if !decode(w, r, &req) {
		return
	}
	// Not trimmed: undoing a change gives back the message exactly as it was,
	// spaces included (« {>> remarque <<} »).
	text := req.Text
	if strings.TrimSpace(text) == "" || len(text) > maxNote || !utf8.ValidString(text) {
		jsonError(w, http.StatusBadRequest, "text", "message vide, trop long ou mal encodé")
		return
	}
	at := critic.Span{Start: req.Start, End: req.Start}
	s.editWith(w, r, req.File, req.Hash, &at, "amend", "", func(src []byte, _ *sourcemap.Map, gen *review.Gen, h *navette.History) (patch.Edit, *apiError) {
		e, m, err := review.Amend(src, req.Start, text, review.Author, gen)
		switch {
		case errors.Is(err, review.ErrNotFound):
			return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "message introuvable"}
		case errors.Is(err, patch.ErrUnchanged):
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "same", message: "le message est inchangé"}
		case err != nil:
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		case m.ID != "" && h.HandedOver()[m.ID]:
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "handed",
				message: "ce message a déjà été remis à l'agent : il ne se modifie plus, répondez dans le fil"}
		}
		return e, nil
	})
}

type replyRequest struct {
	Recipient string `json:"recipient"` // see commentRequest
	File      int    `json:"file"`
	Hash      string `json:"hash"`
	Start     int    `json:"start"` // start of the annotation
	Text      string `json:"text"`
}

// reply adds a comment at the end of the thread of an annotation, whoever
// wrote it.
func (s *Server) reply(w http.ResponseWriter, r *http.Request) {
	var req replyRequest
	if !decode(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || len(text) > maxNote || !utf8.ValidString(text) {
		jsonError(w, http.StatusBadRequest, "text", "réponse vide, trop longue ou mal encodée")
		return
	}
	r, ok := withRecipient(w, r, req.Recipient)
	if !ok {
		return
	}
	var at critic.Span
	s.editAt(w, r, req.File, req.Hash, &at, "reply", "", func(src []byte, _ *sourcemap.Map, gen *review.Gen) (patch.Edit, *apiError) {
		e, pos, err := review.Reply(src, req.Start, "", text, review.Author, gen)
		if errors.Is(err, review.ErrNotFound) {
			return patch.Edit{}, &apiError{status: http.StatusNotFound, code: "not-found", message: "annotation introuvable"}
		}
		if err != nil {
			return patch.Edit{}, &apiError{status: http.StatusUnprocessableEntity, code: "refused", message: message(err)}
		}
		at.Start = pos
		return e, nil
	})
}

// passageText gives the source of a selected passage, for a suggestion
// (SPEC F4 bis). A passage that cuts an inline element is widened, and the
// response says so: the interface shows the widened text before editing.
func (s *Server) passageText(w http.ResponseWriter, r *http.Request) {
	c, ok := s.curFor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	file, err1 := strconv.Atoi(q.Get("file"))
	start, err2 := strconv.Atoi(q.Get("start"))
	end, err3 := strconv.Atoi(q.Get("end"))
	if err1 != nil || err2 != nil || err3 != nil || file < 0 || file >= len(c.P.Files) {
		jsonError(w, http.StatusBadRequest, "query", "requête invalide")
		return
	}
	d, err := store.Read(c.P.Path(file))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "read", err.Error())
		return
	}
	if d.Hash != q.Get("hash") {
		jsonError(w, http.StatusConflict, "conflict", "le fichier a changé sur le disque depuis l'affichage")
		return
	}
	m, err := c.R.Map(r.Context(), file, d)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "render", err.Error())
		return
	}
	res, err := m.Check(d.Bytes, start, end)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "boundary", message(err))
		return
	}
	marks, _ := critic.Parse(d.Bytes)
	for _, mk := range marks {
		if mk.Span.Start < res.End && mk.Span.End > res.Start {
			jsonError(w, http.StatusUnprocessableEntity, "marked", edit.ErrMarked.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"start": res.Start, "end": res.End, "widened": res.Widened,
		"text": string(d.Bytes[res.Start:res.End])})
}

// recipientKey carries the recipient of a request (P10.6) to editWith.
type recipientKey struct{}

// withRecipient checks the short name of the agent that must answer and
// attaches it to the request; it writes 400 if it is invalid.
func withRecipient(w http.ResponseWriter, r *http.Request, name string) (*http.Request, bool) {
	if name == "" {
		return r, true
	}
	if !review.ValidName(name) {
		jsonError(w, http.StatusBadRequest, "recipient", "destinataire invalide : un nom court de glose set")
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), recipientKey{}, name)), true
}

func recipientOf(r *http.Request) string {
	s, _ := r.Context().Value(recipientKey{}).(string)
	return s
}
