package server

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/home"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
	"github.com/ptitan/glose/web"
)

// The overview of the projects (P11.5, D-50): the page, and what it may do.
// It only opens a project, removes it from the registry, says something of it
// or creates one: neither handover nor decision from here. The client names a
// project by its identifier in the registry, never by a path, and the address
// with a token only comes in the answer to its opening.

// homeRoutes registers the overview, on the server of a project as on the
// server of « glose home » outside any project.
func (s *Server) homeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /home", s.homePage)
	mux.HandleFunc("GET /api/accueil", s.homeList)
	mux.HandleFunc("POST /api/accueil/open", s.homeOpen)
	mux.HandleFunc("POST /api/accueil/forget", s.homeForget)
	mux.HandleFunc("POST /api/accueil/meta", s.homeMeta)
	mux.HandleFunc("POST /api/accueil/new", s.homeNew)
}

func (s *Server) homePage(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(web.Files, "accueil.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}

// homeRunning gives the address of a running server, without its token.
func homeRunning(stateDir string) (*home.Server, bool) {
	st, err := Running(stateDir)
	if err != nil {
		return nil, false
	}
	return &home.Server{URL: st.Base(), PID: st.PID}, true
}

// homeReply is what the page shows. Error: the registry cannot be read; it is
// said, and never overwritten.
type homeReply struct {
	Totals      home.Totals    `json:"totals"`
	Projects    []home.Summary `json:"projects"`
	Templates   []string       `json:"templates"`
	TemplateDir string         `json:"templateDir"`
	Error       string         `json:"error,omitempty"`
}

func (s *Server) homeList(w http.ResponseWriter, r *http.Request) {
	rep := homeReply{Projects: []home.Summary{}, Templates: []string{}}
	if t, err := home.Templates(); err == nil {
		rep.Templates = t
	}
	rep.TemplateDir, _ = home.TemplatesPath()
	entries, err := home.Load()
	if err != nil {
		rep.Error = err.Error()
		writeJSON(w, http.StatusOK, rep)
		return
	}
	rep.Projects = home.Summaries(entries, homeRunning)
	rep.Totals = home.Compute(rep.Projects, time.Now())
	writeJSON(w, http.StatusOK, rep)
}

// entryOf finds the project of an identifier of the registry.
func entryOf(w http.ResponseWriter, id string) (home.Entry, bool) {
	entries, err := home.Load()
	if err != nil {
		jsonError(w, http.StatusConflict, "registry", err.Error())
		return home.Entry{}, false
	}
	for _, e := range entries {
		if e.ID() == id {
			return e, true
		}
	}
	jsonError(w, http.StatusNotFound, "unknown", "ce projet n'est pas dans le registre")
	return home.Entry{}, false
}

// startProject starts the server of a project; replaced by the tests.
var startProject = func(p *project.Project) (*State, error) {
	st, _, err := Start(p)
	return st, err
}

func (s *Server) homeOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	e, ok := entryOf(w, req.ID)
	if !ok {
		return
	}
	if _, err := os.Stat(filepath.Join(e.Root, project.ProfileName)); err != nil {
		jsonError(w, http.StatusConflict, "project", "ce projet ne s'ouvre pas : "+project.ProfileName+" est introuvable dans "+e.Root)
		return
	}
	p, err := project.Load(e.Root)
	if err != nil {
		jsonError(w, http.StatusConflict, "project", "ce projet ne s'ouvre pas : "+err.Error())
		return
	}
	st, err := startProject(p)
	if err != nil {
		jsonError(w, http.StatusConflict, "project", "le serveur du projet ne démarre pas : "+err.Error())
		return
	}
	// A failure costs the date of the last opening, nothing else.
	home.Register(e.Root)
	writeJSON(w, http.StatusOK, map[string]string{"url": st.URL})
}

func (s *Server) homeForget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	e, ok := entryOf(w, req.ID)
	if !ok {
		return
	}
	if _, err := home.Forget(e.ID()); err != nil {
		jsonError(w, http.StatusConflict, "registry", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) homeMeta(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string  `json:"id"`
		Hash       string  `json:"hash"`
		Type       *string `json:"type"`
		Status     *string `json:"status"`
		Deadline   *string `json:"deadline"`
		NextAction *string `json:"nextAction"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Hash == "" {
		jsonError(w, http.StatusBadRequest, "hash", "empreinte du profil absente")
		return
	}
	e, ok := entryOf(w, req.ID)
	if !ok {
		return
	}
	var changes []home.MetaChange
	for _, c := range []struct {
		key string
		v   *string
	}{{project.MetaType, req.Type}, {project.MetaStatus, req.Status}, {project.MetaDeadline, req.Deadline}, {project.MetaNextAction, req.NextAction}} {
		if c.v == nil {
			continue
		}
		v := strings.TrimSpace(*c.v)
		if err := project.CheckMeta(c.key, v); err != nil {
			jsonError(w, http.StatusUnprocessableEntity, "meta", err.Error())
			return
		}
		changes = append(changes, home.MetaChange{Key: c.key, Value: v})
	}
	hash, _, err := home.WriteMeta(e.Root, req.Hash, changes)
	if errors.Is(err, store.ErrConflict) {
		jsonError(w, http.StatusConflict, "conflict", project.ProfileName+" a changé depuis l'affichage : rien n'est écrit, votre saisie reste ; rechargez, puis recommencez")
		return
	}
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "meta", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"hash": hash})
}

// expandHome reads the path typed in the form: absolute, or under « ~/ ».
func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		dir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, path[1:])
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("donnez le chemin complet du dossier (/…, ou ~/…)")
	}
	return filepath.Clean(path), nil
}

// homeNew prepares a project (P11.9): the same code as « glose init --type »,
// which never overwrites anything. The directory must exist, and must not be a
// project already.
func (s *Server) homeNew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if !decode(w, r, &req) {
		return
	}
	root, err := expandHome(req.Path)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "path", err.Error())
		return
	}
	fi, err := os.Stat(root)
	switch {
	case errors.Is(err, os.ErrNotExist):
		jsonError(w, http.StatusUnprocessableEntity, "absent", "ce dossier n'existe pas : "+root)
		return
	case err != nil:
		jsonError(w, http.StatusUnprocessableEntity, "path", err.Error())
		return
	case !fi.IsDir():
		jsonError(w, http.StatusUnprocessableEntity, "not-dir", "un fichier porte ce nom, un dossier est attendu : "+root)
		return
	}
	if _, err := os.Lstat(filepath.Join(root, project.ProfileName)); err == nil {
		jsonError(w, http.StatusConflict, "exists", "ce dossier est déjà un projet Glose ; rien n'est écrit. Ouvrez-le par « glose open », il s'inscrit alors ici")
		return
	}
	var out, warn bytes.Buffer
	if err := home.Init(root, "", req.Type, &out, &warn); err != nil {
		code := "init"
		var none *home.ErrNoTemplate
		if errors.As(err, &none) {
			code = "template"
		}
		jsonError(w, http.StatusUnprocessableEntity, code, err.Error())
		return
	}
	real := root
	if x, err := filepath.EvalSymlinks(root); err == nil {
		real = x
	}
	registered := false
	if entries, err := home.Load(); err == nil {
		for _, e := range entries {
			registered = registered || e.Root == real
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": home.ID(real), "root": real, "registered": registered,
		"output": out.String(), "warnings": warn.String()})
}
