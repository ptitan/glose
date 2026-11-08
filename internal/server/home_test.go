package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/home"
	"github.com/ptitan/glose/internal/project"
)

// homeWorld gives a server, its own registry, and moves the temporary directory
// of the system: the projects made by the test count as ordinary directories.
func homeWorld(t *testing.T) (*Server, string) {
	t.Helper()
	s, _ := newServer(t, map[string]string{"a.md": "Texte.\n"})
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	other := filepath.Join(base, "systeme-tmp")
	os.MkdirAll(other, 0o700)
	t.Setenv("GLOSE_CONFIG_DIR", filepath.Join(base, "config"))
	t.Setenv("TMPDIR", other)
	t.Setenv("TMP", other)
	t.Setenv("TEMP", other)
	return s, base
}

func homeProject(t *testing.T, dir, profile string) string {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("# T\n\nUn {>>mot<<} texte.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "glose.json"), []byte(profile), 0o644)
	if err := home.Register(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func listHome(t *testing.T, s *Server) homeReply {
	t.Helper()
	var rep homeReply
	w := do(t, s, "GET", "/api/accueil", nil)
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rep) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return rep
}

func TestHomeListAndOpen(t *testing.T) {
	s, base := homeWorld(t)
	dir := homeProject(t, filepath.Join(base, "livre"), `{"title":"Livre","project":{"status":"review"}}`)
	gone := homeProject(t, filepath.Join(base, "disparu"), `{}`)
	os.RemoveAll(gone)
	rep := listHome(t, s)
	if len(rep.Projects) != 2 || rep.Totals.Projects != 2 || rep.Totals.Annotations != 1 || rep.Totals.InReview != 1 || rep.Totals.Unreadable != 1 {
		t.Fatalf("%+v", rep)
	}
	const token = "le-jeton-du-projet"
	var started *project.Project
	old := startProject
	startProject = func(p *project.Project) (*State, error) {
		started = p
		return &State{URL: "http://127.0.0.1:1/#t=" + token}, nil
	}
	defer func() { startProject = old }()
	if b, _ := json.Marshal(rep); strings.Contains(string(b), token) {
		t.Fatal("jeton dans la liste")
	}
	w := do(t, s, "POST", "/api/accueil/open", map[string]string{"id": home.ID(dir)})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "#t="+token) || started == nil || started.Root != dir {
		t.Fatalf("%d %s %+v", w.Code, w.Body, started)
	}
	for id, code := range map[string]int{"inconnu": http.StatusNotFound, dir: http.StatusNotFound, home.ID(gone): http.StatusConflict} {
		if w := do(t, s, "POST", "/api/accueil/open", map[string]string{"id": id}); w.Code != code {
			t.Errorf("%s : %d %s", id, w.Code, w.Body)
		}
	}
	// A path is not a field of the request.
	if w := do(t, s, "POST", "/api/accueil/open", map[string]string{"path": dir}); w.Code != http.StatusBadRequest {
		t.Errorf("chemin accepté : %d", w.Code)
	}
}

// Host, Origin, token: the guard of the server covers the overview. The page
// itself is public, as the interface is; what it reads is not.
func TestHomeIsGuarded(t *testing.T) {
	s, base := homeWorld(t)
	dir := homeProject(t, filepath.Join(base, "livre"), `{}`)
	h := s.Handler()
	body := func() *strings.Reader { return strings.NewReader(`{"id":"` + home.ID(dir) + `"}`) }
	run := func(method, path string, set func(*http.Request)) int {
		var r *http.Request
		if method == "POST" {
			r = httptest.NewRequest(method, path, body())
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Host = "127.0.0.1:" + strconv.Itoa(s.Port)
		r.Header.Set("X-Glose-Token", s.Token)
		set(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for _, path := range []string{"/api/accueil/open", "/api/accueil/forget", "/api/accueil/new", "/api/accueil/meta"} {
		if c := run("POST", path, func(r *http.Request) { r.Header.Del("X-Glose-Token") }); c != http.StatusUnauthorized {
			t.Errorf("%s sans jeton : %d", path, c)
		}
		if c := run("POST", path, func(r *http.Request) { r.Header.Set("Origin", "http://exemple.org") }); c != http.StatusForbidden {
			t.Errorf("%s, Origin : %d", path, c)
		}
		if c := run("POST", path, func(r *http.Request) { r.Host = "exemple.org" }); c != http.StatusMisdirectedRequest {
			t.Errorf("%s, Host : %d", path, c)
		}
	}
	if c := run("GET", "/api/accueil", func(r *http.Request) { r.Header.Del("X-Glose-Token") }); c != http.StatusUnauthorized {
		t.Errorf("liste sans jeton : %d", c)
	}
	if c := run("GET", "/home", func(r *http.Request) { r.Header.Del("X-Glose-Token") }); c != http.StatusOK {
		t.Errorf("page : %d", c)
	}
	if entries, _ := home.Load(); len(entries) != 1 {
		t.Fatalf("registre touché : %v", entries)
	}
}

func TestHomeForget(t *testing.T) {
	s, base := homeWorld(t)
	dir := homeProject(t, filepath.Join(base, "livre"), `{}`)
	if w := do(t, s, "POST", "/api/accueil/forget", map[string]string{"id": "inconnu"}); w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
	if w := do(t, s, "POST", "/api/accueil/forget", map[string]string{"id": home.ID(dir)}); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(listHome(t, s).Projects) != 0 {
		t.Fatal("non retiré")
	}
	if _, err := os.Stat(filepath.Join(dir, "glose.json")); err != nil {
		t.Fatal("projet touché")
	}
}

// Only the value concerned changes, and a profile changed since the display is
// refused, without writing.
func TestHomeMeta(t *testing.T) {
	s, base := homeWorld(t)
	profile := "{\r\n  \"title\": \"Livre\"\r\n}\r\n"
	dir := homeProject(t, filepath.Join(base, "livre"), profile)
	p := filepath.Join(dir, "glose.json")
	sum := listHome(t, s).Projects[0]
	v := "review"
	w := do(t, s, "POST", "/api/accueil/meta", map[string]any{"id": sum.ID, "hash": sum.ProfileHash, "status": v})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	want := "{\r\n  \"title\": \"Livre\",\r\n  \"project\": {\r\n    \"status\": \"review\"\r\n  }\r\n}\r\n"
	if b, _ := os.ReadFile(p); string(b) != want {
		t.Fatalf("%q", b)
	}
	if w := do(t, s, "POST", "/api/accueil/meta", map[string]any{"id": sum.ID, "hash": sum.ProfileHash, "status": "done"}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "conflict") {
		t.Fatalf("empreinte périmée : %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(p); string(b) != want {
		t.Fatalf("écrit malgré le conflit : %q", b)
	}
	fresh := listHome(t, s).Projects[0]
	for _, bad := range []map[string]any{{"status": "fini"}, {"deadline": "demain"}, {"nextAction": "deux\nlignes"}} {
		bad["id"], bad["hash"] = sum.ID, fresh.ProfileHash
		if w := do(t, s, "POST", "/api/accueil/meta", bad); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v : %d", bad, w.Code)
		}
	}
	if w := do(t, s, "POST", "/api/accueil/meta", map[string]any{"id": sum.ID, "status": "done"}); w.Code != http.StatusBadRequest {
		t.Errorf("sans empreinte : %d", w.Code)
	}
}

// « Nouveau projet »: the same code as glose init --type, without overwriting.
func TestHomeNew(t *testing.T) {
	s, base := homeWorld(t)
	gabarit := filepath.Join(base, "config", "gabarits", "article")
	os.MkdirAll(gabarit, 0o755)
	os.WriteFile(filepath.Join(gabarit, "plan.md"), []byte("# Plan du gabarit\n"), 0o644)
	if rep := listHome(t, s); len(rep.Templates) != 1 || rep.Templates[0] != "article" {
		t.Fatalf("%+v", rep)
	}
	post := func(path, kind string) (int, string) {
		w := do(t, s, "POST", "/api/accueil/new", map[string]string{"path": path, "type": kind})
		return w.Code, w.Body.String()
	}
	// Refusals.
	absent := filepath.Join(base, "absent")
	if c, b := post(absent, "article"); c != http.StatusUnprocessableEntity || !strings.Contains(b, "absent") {
		t.Errorf("%d %s", c, b)
	}
	if _, err := os.Stat(absent); err == nil {
		t.Error("dossier créé")
	}
	file := filepath.Join(base, "fichier")
	os.WriteFile(file, []byte("x"), 0o644)
	if c, b := post(file, "article"); c != http.StatusUnprocessableEntity || !strings.Contains(b, "not-dir") {
		t.Errorf("%d %s", c, b)
	}
	if c, b := post("relatif", "article"); c != http.StatusUnprocessableEntity {
		t.Errorf("%d %s", c, b)
	}
	exists := filepath.Join(base, "existant")
	os.MkdirAll(exists, 0o755)
	os.WriteFile(filepath.Join(exists, "glose.json"), []byte("{ à la main }"), 0o644)
	if c, b := post(exists, "article"); c != http.StatusConflict || !strings.Contains(b, "exists") {
		t.Errorf("%d %s", c, b)
	}
	if b, _ := os.ReadFile(filepath.Join(exists, "glose.json")); string(b) != "{ à la main }" {
		t.Errorf("profil touché : %s", b)
	}
	dir := filepath.Join(base, "neuf")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# À moi\n"), 0o644)
	if c, b := post(dir, "inconnu"); c != http.StatusUnprocessableEntity || !strings.Contains(b, "template") || !strings.Contains(b, "article") {
		t.Errorf("%d %s", c, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "glose.json")); err == nil {
		t.Error("profil écrit malgré le gabarit inconnu")
	}
	// Created: nothing overwritten, type from the template, registered.
	c, b := post(dir, "article")
	if c != http.StatusOK || !strings.Contains(b, `"registered":true`) || !strings.Contains(b, "existe déjà") {
		t.Fatalf("%d %s", c, b)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "plan.md")); string(got) != "# À moi\n" {
		t.Fatalf("écrasé : %s", got)
	}
	rep := listHome(t, s)
	if len(rep.Projects) != 1 || rep.Projects[0].Meta.Type != "article" {
		t.Fatalf("%+v", rep)
	}
	// Under the home directory; a second creation is refused.
	if c, b := post(dir, "article"); c != http.StatusConflict {
		t.Errorf("%d %s", c, b)
	}
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	sous := filepath.Join(base, "sous-mon-dossier")
	os.MkdirAll(sous, 0o755)
	os.WriteFile(filepath.Join(sous, "a.md"), []byte("T\n"), 0o644)
	if c, b := post("~/sous-mon-dossier", ""); c != http.StatusOK {
		t.Errorf("~ : %d %s", c, b)
	}
}

// A registry that cannot be read is said, and left as it is.
func TestHomeUnreadableRegistry(t *testing.T) {
	s, base := homeWorld(t)
	path := filepath.Join(base, "config", "projets.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{ abîmé"), 0o600)
	rep := listHome(t, s)
	if rep.Error == "" || len(rep.Projects) != 0 {
		t.Fatalf("%+v", rep)
	}
	if w := do(t, s, "POST", "/api/accueil/forget", map[string]string{"id": "x"}); w.Code != http.StatusConflict {
		t.Errorf("%d", w.Code)
	}
	if b, _ := os.ReadFile(path); string(b) != "{ abîmé" {
		t.Errorf("écrasé : %s", b)
	}
}
