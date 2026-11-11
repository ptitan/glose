package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ptitan/glose/internal/navette"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/trust"
)

const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const port = 4242
const host = "127.0.0.1:4242"

func needPandoc(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent : test sauté")
	}
}

func newServer(t *testing.T, files map[string]string) (*Server, string) {
	t.Helper()
	// Never the author's configuration (agents, approvals D-64): a test that
	// sets its own before keeps it.
	if os.Getenv("GLOSE_CONFIG_DIR") == "" {
		t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	}
	dir := t.TempDir()
	// As the project names it (links, short names of Windows).
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(p, port, token), dir
}

type reqOpt func(*http.Request)

func withHost(h string) reqOpt      { return func(r *http.Request) { r.Host = h } }
func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func noAuth() reqOpt                { return func(r *http.Request) { r.Header.Del("X-Glose-Token") } }

func do(t *testing.T, s *Server, method, path string, body any, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = host
	r.Header.Set("X-Glose-Token", token)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestSecurity(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte.\n", "img/ok.png": "png", "notes.txt": "secret"})
	cases := []struct {
		name   string
		w      *httptest.ResponseRecorder
		status int
	}{
		{"hôte étranger", do(t, s, "GET", "/api/ping", nil, withHost("evil.example:4242")), http.StatusMisdirectedRequest},
		{"hôte sur un autre port", do(t, s, "GET", "/api/ping", nil, withHost("127.0.0.1:80")), http.StatusMisdirectedRequest},
		{"sans jeton", do(t, s, "GET", "/api/doc", nil, noAuth()), http.StatusUnauthorized},
		{"interface sans jeton (publique)", do(t, s, "GET", "/", nil, noAuth()), http.StatusOK},
		{"script sans jeton (public)", do(t, s, "GET", "/static/app.js", nil, noAuth()), http.StatusOK},
		{"feuille du projet sans jeton", do(t, s, "GET", "/project.css", nil, noAuth()), http.StatusUnauthorized},
		{"feuille du projet avec jeton", do(t, s, "GET", "/project.css?t="+token, nil, noAuth()), http.StatusOK},
		{"image sans jeton", do(t, s, "GET", "/img/ok.png", nil, noAuth()), http.StatusUnauthorized},
		{"image, jeton dans la requête", do(t, s, "GET", "/img/ok.png?t="+token, nil, noAuth()), http.StatusOK},
		{"API, jeton dans la requête refusé", do(t, s, "GET", "/api/doc?t="+token, nil, noAuth()), http.StatusUnauthorized},
		{"arrêt sans jeton", do(t, s, "POST", "/api/stop", map[string]any{}, noAuth()), http.StatusUnauthorized},
		{"mauvais jeton", do(t, s, "GET", "/api/ping", nil, withHeader("X-Glose-Token", strings.Repeat("0", 64))), http.StatusUnauthorized},
		{"avec jeton", do(t, s, "GET", "/api/ping", nil), http.StatusOK},
		{"localhost accepté", do(t, s, "GET", "/api/ping", nil, withHost("localhost:4242")), http.StatusOK},
		{"origine étrangère", do(t, s, "POST", "/api/stop", map[string]any{}, withHeader("Origin", "http://evil.example")), http.StatusForbidden},
		{"autre site", do(t, s, "POST", "/api/withdraw", map[string]any{}, withHeader("Sec-Fetch-Site", "cross-site")), http.StatusForbidden},
		{"pas du JSON", do(t, s, "POST", "/api/withdraw", nil, withHeader("Content-Type", "text/plain")), http.StatusUnsupportedMediaType},
		{"champ inconnu", do(t, s, "POST", "/api/withdraw", map[string]any{"x": 1}), http.StatusBadRequest},
		{"image permise", do(t, s, "GET", "/img/ok.png", nil), http.StatusOK},
		{"fichier non image", do(t, s, "GET", "/notes.txt", nil), http.StatusNotFound},
		{"source Markdown", do(t, s, "GET", "/a.md", nil), http.StatusNotFound},
		{"remontée (chemin nettoyé, redirigé)", do(t, s, "GET", "/../../etc/passwd", nil), http.StatusTemporaryRedirect},
		{"chemin absolu", do(t, s, "GET", "/etc/passwd", nil), http.StatusNotFound},
		{"remontée encodée", do(t, s, "GET", "/img/..%2f..%2fetc%2fpasswd", nil), http.StatusNotFound},
		{"état du serveur", do(t, s, "GET", "/.glose/server.json", nil), http.StatusNotFound},
	}
	for _, c := range cases {
		if c.w.Code != c.status {
			t.Errorf("%s : %d, attendu %d (%s)", c.name, c.w.Code, c.status, c.w.Body.String())
		}
	}
	w := do(t, s, "GET", "/api/ping", nil)
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP : %q", csp)
	}
	// No cookie at all: a cookie of 127.0.0.1 would reach every other port.
	for _, w := range []*httptest.ResponseRecorder{do(t, s, "GET", "/", nil), do(t, s, "GET", "/api/doc", nil)} {
		if len(w.Result().Cookies()) != 0 {
			t.Fatalf("cookie posé : %v", w.Result().Cookies())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".glose")); err == nil {
		t.Log("(état créé par une requête : attendu seulement pour une écriture)")
	}
}

type docResp struct {
	HTML        string            `json:"html"`
	Annotations []json.RawMessage `json:"annotations"`
	Files       []struct {
		Index int    `json:"index"`
		Hash  string `json:"hash"`
	} `json:"files"`
}

func getDoc(t *testing.T, s *Server) docResp {
	t.Helper()
	w := do(t, s, "GET", "/api/doc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("doc : %d %s", w.Code, w.Body.String())
	}
	var d docResp
	json.Unmarshal(w.Body.Bytes(), &d)
	return d
}

// A slow render may finish after an agent write. Do not combine its old hash
// with a newer exchange state (which could already give the author the hand).
func TestDocChangedDuringRender(t *testing.T) {
	needPandoc(t)
	s, dir := newServer(t, map[string]string{"a.md": "Avant le rendu.\r\n"})
	filter := filepath.Join(dir, "change-during-render.lua")
	// An adversarial project filter deterministically changes the source after
	// it has been read but before the HTTP handler reads the exchange state.
	if err := os.WriteFile(filter, []byte(`function Pandoc(doc)
  local f = assert(io.open("a.md", "wb"))
  f:write("Après le rendu.\r\n")
  f:close()
  return doc
end
`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.P.Filter = filter
	approveFilter(t, s)
	w := do(t, s, "GET", "/api/doc", nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "stale-render") {
		t.Fatalf("rendu périmé présenté comme actuel : %d %s", w.Code, w.Body.String())
	}
	s.P.Filter = ""
	d := getDoc(t, s)
	if !strings.Contains(d.HTML, "Après") || d.Files[0].Hash == "" {
		t.Fatalf("nouveau rendu non récupéré : %+v", d)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "a.md")); err != nil || string(b) != "Après le rendu.\r\n" {
		t.Fatalf("l'écriture extérieure a été écrasée : %q (%v)", b, err)
	}
}

var wordRe = regexp.MustCompile(`<span data-g="(\d+):(\d+),(\d+)">([^<\s][^<]*)</span>`)

type word struct{ f, s, e int }

func words(html string) []word {
	var out []word
	for _, m := range wordRe.FindAllStringSubmatch(html, -1) {
		f, _ := strconv.Atoi(m[1])
		s, _ := strconv.Atoi(m[2])
		e, _ := strconv.Atoi(m[3])
		out = append(out, word{f, s, e})
	}
	return out
}

// findRenderedWord locates a unique certified word in the original bytes, not
// a substring of another word, a comment or a randomly generated identifier.
// Never strip metadata before searching: that would shift the source offsets.
func findRenderedWord(html, src string, file int, text string) (word, error) {
	var found word
	count := 0
	for _, w := range words(html) {
		if w.f != file {
			continue
		}
		if w.s < 0 || w.e <= w.s || w.e > len(src) {
			return word{}, fmt.Errorf("mot rendu hors de la source : %+v", w)
		}
		if src[w.s:w.e] == text {
			found = w
			count++
		}
	}
	if count != 1 {
		return word{}, fmt.Errorf("mot %q : %d occurrences certifiées dans le fichier %d, une attendue", text, count, file)
	}
	return found, nil
}

func TestFindRenderedWord(t *testing.T) {
	src := "commun {>>un<<}{>>glose:v1 id=aunaaa by=author<<} - un"
	at := strings.LastIndex(src, "un")
	span := fmt.Sprintf(`<span data-g="0:%d,%d">un</span>`, at, at+2)
	for _, c := range []struct {
		name, src, html string
		want            int
		fail            bool
	}{
		{"metadata_and_substring", src, `<span data-g="0:0,6">commun</span>` + span, at, false},
		{"other_file", src, `<span data-g="1:0,2">un</span>` + span, at, false},
		{"transformed_word", src, `<span data-g="0:4,6,t">un</span>` + span, at, false},
		{"missing", src, `<span data-g="0:0,6">commun</span>`, 0, true},
		{"ambiguous", "un un", `<span data-g="0:0,2">un</span><span data-g="0:3,5">un</span>`, 0, true},
		{"invalid_bounds", "un", `<span data-g="0:0,20">un</span>`, 0, true},
		// Positions are byte offsets even when a multibyte character precedes them.
		{"utf8", "é un", `<span data-g="0:3,5">un</span>`, 3, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, err := findRenderedWord(c.html, c.src, 0, "un")
			if (err != nil) != c.fail || err == nil && (w.s != c.want || w.e != c.want+2) {
				t.Fatalf("mot : %+v, erreur : %v", w, err)
			}
		})
	}
}

func TestCommentFlow(t *testing.T) {
	needPandoc(t)
	src := "# Titre\n\nUn **mot important** dans une phrase ordinaire.\n\nSecond paragraphe ici.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	d := getDoc(t, s)
	hash := d.Files[0].Hash
	at := func(x string) int { return strings.Index(src, x) }
	end := func(x string) int { return strings.Index(src, x) + len(x) }

	// Passage that cuts the bold: the server proposes a widened passage.
	w := do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "passage",
		"start": at("important"), "end": end("dans"), "text": "Note"})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusUnprocessableEntity || resp["error"] != "widen" || resp["excerpt"] != "**mot important** dans" {
		t.Fatalf("élargissement : %d %v", w.Code, resp)
	}
	// The widened passage is accepted.
	w = do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "passage",
		"start": resp["start"], "end": resp["end"], "text": "Note"})
	if w.Code != http.StatusOK {
		t.Fatalf("passage élargi : %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "{>>Note<<}{>>glose:v1 id=") || !strings.Contains(noMeta(string(got)), "Un {==**mot important** dans==}{>>Note<<} une") {
		t.Fatalf("fichier : %q", got)
	}
	// A stale hash is a conflict, and nothing is written.
	w = do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "paragraph",
		"start": at("Second"), "end": at("Second"), "text": "x"})
	if w.Code != http.StatusConflict {
		t.Fatalf("conflit : %d", w.Code)
	}
	// Withdraw restores the original bytes.
	w = do(t, s, "POST", "/api/withdraw", map[string]any{"file": 0, "hash": resp["hash"], "start": resp["start"]})
	if w.Code != http.StatusOK {
		t.Fatalf("retrait : %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(path); noMeta(string(got)) != src {
		t.Fatalf("après retrait : %q", got)
	}
	// Paragraph comment: at the end of the paragraph.
	d = getDoc(t, s)
	w = do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": d.Files[0].Hash, "mode": "paragraph",
		"start": at("Second"), "end": at("Second"), "text": "Fin"})
	if got, _ := os.ReadFile(path); w.Code != http.StatusOK || !strings.Contains(noMeta(string(got)), "ici.{>>Fin<<}\n") {
		t.Fatalf("paragraphe : %d %q", w.Code, got)
	}
	// Proposals cannot be withdrawn from the interface (D-14).
	os.WriteFile(path, []byte("a {++b++} c\n"), 0o644)
	d = getDoc(t, s)
	w = do(t, s, "POST", "/api/withdraw", map[string]any{"file": 0, "hash": d.Files[0].Hash, "start": 2})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("retrait d'une proposition : %d", w.Code)
	}
}

// TestManyComments puts many comments at random places of a copy of the
// project given by GLOSE_ESSAI (P1.5), then checks that, marks removed, every
// file is byte for byte the original. Skipped without that variable.
func TestManyComments(t *testing.T) {
	needPandoc(t)
	from := os.Getenv("GLOSE_ESSAI")
	if from == "" {
		t.Skip("GLOSE_ESSAI non défini")
	}
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-a", from+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copie : %v %s", err, out)
	}
	os.RemoveAll(filepath.Join(dir, ".glose"))
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(p, port, token)
	originals := map[int][]byte{} // view Current of each file before the comments
	issuesBefore := map[int]int{}
	before := 0
	for i := range p.Files {
		src, _ := os.ReadFile(p.Path(i))
		originals[i] = projection.Project(src, projection.Current).Text
		marks, issues := critic.Parse(src)
		before += len(critic.Group(marks))
		issuesBefore[i] = len(issues)
	}
	rng := rand.New(rand.NewSource(20261106))
	want := 20
	if n, err := strconv.Atoi(os.Getenv("GLOSE_ESSAI_N")); err == nil {
		want = n
	}
	done, refused := 0, map[string]int{}
	for attempt := 0; done < want && attempt < want*5; attempt++ {
		d := getDoc(t, s)
		ws := words(d.HTML)
		a := ws[rng.Intn(len(ws))]
		b := a
		if k := rng.Intn(6); k > 0 {
			for _, x := range ws {
				if x.f == a.f && x.s > a.e && rng.Intn(3) == 0 {
					b = x
					if k--; k == 0 {
						break
					}
				}
				if x.f == a.f && x.s > a.e+200 {
					break
				}
			}
		}
		mode := []string{"passage", "passage", "passage", "paragraph", "point"}[rng.Intn(5)]
		start, end := a.s, b.e
		if mode == "point" {
			start, end = a.e, a.e
		}
		body := map[string]any{"file": a.f, "hash": d.Files[a.f].Hash, "mode": mode, "start": start, "end": end,
			"text": "Essai " + strconv.Itoa(attempt) + " : « remarque » avec accents, été, 😀."}
		w := do(t, s, "POST", "/api/comment", body)
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		if w.Code == http.StatusUnprocessableEntity && resp["error"] == "widen" {
			body["start"], body["end"] = resp["start"], resp["end"]
			w = do(t, s, "POST", "/api/comment", body)
			json.Unmarshal(w.Body.Bytes(), &resp)
		}
		if w.Code != http.StatusOK {
			refused[resp["error"].(string)]++
			continue
		}
		done++
	}
	t.Logf("%d commentaires posés ; refus : %v", done, refused)
	if done < want {
		t.Fatalf("seulement %d commentaires posés sur %d", done, want)
	}
	total := 0
	for i := range p.Files {
		cur, _ := os.ReadFile(p.Path(i))
		marks, issues := critic.Parse(cur)
		if len(issues) != issuesBefore[i] {
			t.Errorf("%s : marques mal formées %v", p.Files[i], issues)
		}
		total += len(critic.Group(marks))
		if view := projection.Project(cur, projection.Current).Text; !bytes.Equal(view, originals[i]) {
			t.Errorf("%s : une fois les marques retirées, le fichier diffère de l'original", p.Files[i])
		}
	}
	if total != before+done {
		t.Errorf("%d annotations dans les fichiers, %d avant et %d posées", total, before, done)
	}
}

func TestEditAndDecide(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "GET", "/api/block?file=0&hash="+hash+"&at=3", nil)
	var blk map[string]any
	json.Unmarshal(w.Body.Bytes(), &blk)
	if w.Code != http.StatusOK || blk["text"] != "Le chat dort." || blk["start"] != 0.0 || blk["end"] != 13.0 {
		t.Fatalf("bloc : %d %v", w.Code, blk)
	}
	w = do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "at": 3, "start": 0, "end": 13, "text": "Le chien dort bien."})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || noMeta(read()) != "Le {~~chat~>chien~~} dort{++ bien++}.\n\nFin.\n" {
		t.Fatalf("édition : %d %v %q", w.Code, resp, read())
	}
	// A block with marks cannot be rewritten.
	h2 := resp["hash"].(string)
	if w := do(t, s, "GET", "/api/block?file=0&hash="+h2+"&at=0", nil); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bloc annoté : %d", w.Code)
	}
	// Undo the rewriting: reject every proposal of the range.
	end := int(resp["end"].(float64))
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": h2, "start": 0, "end": end, "decision": "reject"})
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || noMeta(read()) != src {
		t.Fatalf("tout rejeter : %d %q", w.Code, read())
	}
	// Rewrite again, then accept one proposal and reject the other.
	w = do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": resp["hash"], "at": 3, "start": 0, "end": 13, "text": "Le chien dort bien."})
	json.Unmarshal(w.Body.Bytes(), &resp)
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": resp["hash"], "start": 3, "decision": "accept"})
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || noMeta(read()) != "Le chien dort{++ bien++}.\n\nFin.\n" {
		t.Fatalf("accepter : %d %q", w.Code, read())
	}
	at := strings.Index(read(), "{++")
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": resp["hash"], "start": at, "decision": "reject"})
	if w.Code != http.StatusOK || noMeta(read()) != "Le chien dort.\n\nFin.\n" {
		t.Fatalf("rejeter : %d %q", w.Code, read())
	}
	// Comments are not decided this way.
	os.WriteFile(path, []byte("a {>>b<<} c\n"), 0o644)
	h := getDoc(t, s).Files[0].Hash
	if w := do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": h, "start": 2, "decision": "accept"}); w.Code != http.StatusNotFound {
		t.Fatalf("décider un commentaire : %d", w.Code)
	}
}

func TestSettings(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	s, _ := newServer(t, map[string]string{"a.md": "x\n"})
	var st Settings
	json.Unmarshal(do(t, s, "GET", "/api/settings", nil).Body.Bytes(), &st)
	if st != DefaultSettings {
		t.Fatalf("défaut : %+v", st)
	}
	want := Settings{Font: "sans", Size: 20, LineHeight: 1.8, Justify: true, Theme: "dark", TextWidth: 50}
	if w := do(t, s, "POST", "/api/settings", want); w.Code != http.StatusOK {
		t.Fatalf("enregistrement : %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(do(t, s, "GET", "/api/settings", nil).Body.Bytes(), &st)
	if st != want {
		t.Fatalf("relu : %+v", st)
	}
	bad := want
	bad.Size = 300
	if w := do(t, s, "POST", "/api/settings", bad); w.Code != http.StatusBadRequest {
		t.Fatalf("hors limites : %d", w.Code)
	}
}

func TestEditABlockThatStartsWithALabel(t *testing.T) {
	needPandoc(t)
	src := "{#p-un} **Titre.** Le chat dort.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	hash := getDoc(t, s).Files[0].Hash
	at := strings.Index(src, "Le")
	w := do(t, s, "GET", "/api/block?file=0&hash="+hash+"&at="+strconv.Itoa(at), nil)
	var blk map[string]any
	json.Unmarshal(w.Body.Bytes(), &blk)
	if w.Code != http.StatusOK || blk["text"] != strings.TrimSpace(src) {
		t.Fatalf("bloc : %d %v", w.Code, blk)
	}
	w = do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "at": at, "start": blk["start"], "end": blk["end"],
		"text": "{#p-un} **Titre.** Le chien dort."})
	got, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if w.Code != http.StatusOK || noMeta(string(got)) != "{#p-un} **Titre.** Le {~~chat~>chien~~} dort.\n" {
		t.Fatalf("édition : %d %s %q", w.Code, w.Body.String(), got)
	}
}

func TestReplyAndUndo(t *testing.T) {
	needPandoc(t)
	src := "Un {==passage==}{>>question<<} et {~~a~>b~~} fin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "POST", "/api/reply", map[string]any{"file": 0, "hash": hash, "start": 3, "text": "réponse"})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || noMeta(read()) != "Un {==passage==}{>>question<<}{>>réponse<<} et {~~a~>b~~} fin.\n" {
		t.Fatalf("réponse : %d %q", w.Code, read())
	}
	// Undo: the reply alone is removed.
	w = do(t, s, "POST", "/api/withdraw", map[string]any{"file": 0, "hash": resp["hash"], "start": resp["start"]})
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || noMeta(read()) != src {
		t.Fatalf("retrait de la réponse : %d %q", w.Code, read())
	}
	// A proposal can be discussed too.
	at := strings.Index(read(), "{~~")
	if w := do(t, s, "POST", "/api/reply", map[string]any{"file": 0, "hash": resp["hash"], "start": at, "text": "pourquoi ?"}); w.Code != http.StatusOK ||
		!strings.Contains(noMeta(read()), "{~~a~>b~~}{>>pourquoi ?<<}") {
		t.Fatalf("réponse à une proposition : %d %q", w.Code, read())
	}
}

func TestSuggestAPassage(t *testing.T) {
	needPandoc(t)
	src := "Un **mot important** dans une phrase ordinaire.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	hash := getDoc(t, s).Files[0].Hash
	at, end := strings.Index(src, "important"), strings.Index(src, "dans")+len("dans")
	w := do(t, s, "GET", "/api/passage?file=0&hash="+hash+"&start="+strconv.Itoa(at)+"&end="+strconv.Itoa(end), nil)
	var p map[string]any
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != http.StatusOK || p["widened"] != true || p["text"] != "**mot important** dans" {
		t.Fatalf("passage : %d %v", w.Code, p)
	}
	w = do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "mode": "passage", "start": p["start"], "end": p["end"],
		"text": "**mot essentiel** dans", "note": "Qu'en penses-tu ?"})
	got, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if w.Code != http.StatusOK || noMeta(string(got)) != "Un **mot {~~important~>essentiel~~}{>>Qu'en penses-tu ?<<}** dans une phrase ordinaire.\n" {
		t.Fatalf("suggestion : %d %s %q", w.Code, w.Body.String(), got)
	}
}

var metaRe = regexp.MustCompile(`\{>>glose:v1[^<]*<<\}`)

// noMeta removes the technical comments (identities, D-19) of a text.
func noMeta(s string) string { return metaRe.ReplaceAllString(s, "") }

func TestHandoffAndTurns(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "passage", "start": 3, "end": 7, "text": "Lequel ?"})
	if w.Code != http.StatusOK {
		t.Fatalf("commentaire : %d %s", w.Code, w.Body.String())
	}
	var st struct {
		Turn     int            `json:"turn"`
		Hand     string         `json:"hand"`
		Statuses map[string]int `json:"statuses"`
	}
	json.Unmarshal(do(t, s, "GET", "/api/state", nil).Body.Bytes(), &st)
	if st.Turn != 1 || st.Hand != "author" || st.Statuses["new"] != 1 {
		t.Fatalf("état : %+v", st)
	}
	if w := do(t, s, "POST", "/api/handoff", map[string]any{"note": "À toi."}); w.Code != http.StatusOK {
		t.Fatalf("remise : %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(do(t, s, "GET", "/api/state", nil).Body.Bytes(), &st)
	if st.Turn != 2 || st.Hand != "agent" || st.Statuses["open"] != 1 {
		t.Fatalf("état après remise : %+v", st)
	}
	// The author does not write while the agent has the hand.
	b, _ := os.ReadFile(path)
	h2 := getDoc(t, s).Files[0].Hash
	w = do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": h2, "mode": "paragraph", "start": strings.Index(string(b), "Fin"), "end": strings.Index(string(b), "Fin"), "text": "x"})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusConflict || resp["error"] != "turn" {
		t.Fatalf("écriture pendant le tour de l'agent : %d %v", w.Code, resp)
	}
	if after, _ := os.ReadFile(path); string(after) != string(b) {
		t.Fatal("fichier modifié")
	}
	if w := do(t, s, "POST", "/api/handoff", map[string]any{"note": ""}); w.Code != http.StatusConflict {
		t.Fatalf("seconde remise : %d", w.Code)
	}
	// The agent changes the text freely; the author takes the hand back:
	// the change becomes a proposal.
	os.WriteFile(path, []byte(strings.Replace(string(b), "Fin.", "Fin du texte.", 1)), 0o644)
	w = do(t, s, "POST", "/api/reclaim", map[string]any{"note": ""})
	json.Unmarshal(w.Body.Bytes(), &resp)
	if got, _ := os.ReadFile(path); w.Code != http.StatusOK || resp["converted"] != 1.0 {
		t.Logf("%s", got)
		t.Fatalf("reprise : %d %v", w.Code, resp)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(noMeta(string(got)), "Fin{++ du texte++}.") {
		t.Fatalf("%q", got)
	}
	d := getDoc(t, s)
	if len(d.Annotations) != 2 {
		t.Fatalf("%d annotations", len(d.Annotations))
	}
	// The tab « Navette » (P4.1): one part per hand, grouped in rounds.
	var lg struct {
		Hand  string
		Turns []navette.TurnLog
	}
	json.Unmarshal(do(t, s, "GET", "/api/log", nil).Body.Bytes(), &lg)
	if lg.Hand != "author" || len(lg.Turns) != 3 {
		t.Fatalf("historique : %+v", lg)
	}
	t1, t2, t3 := lg.Turns[0], lg.Turns[1], lg.Turns[2]
	if t1.Round != 1 || t1.Actor != "author" || t1.Note != "À toi." || len(t1.Ops) == 0 || t1.Ops[0].Kind != "comment" ||
		t2.Round != 1 || t2.Actor != "agent" || !t2.Reclaimed || t3.Round != 2 || t3.Actor != "" {
		t.Fatalf("historique : %+v", lg)
	}
}

// Aperçu d'une réécriture (rien n'est écrit), puis enregistrement d'une note
// de bas de page saisie « ^[…] ».
func TestPreviewAndFootnote(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	hash := getDoc(t, s).Files[0].Hash
	req := map[string]any{"file": 0, "hash": hash, "at": 3, "start": 0, "end": 13, "text": "Le chien dort^[Depuis midi.]."}
	w := do(t, s, "POST", "/api/preview", req)
	var p struct {
		HTML    string           `json:"html"`
		Changes []map[string]any `json:"changes"`
	}
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != http.StatusOK || !strings.Contains(p.HTML, "midi") || !strings.Contains(p.HTML, "chien") || len(p.Changes) != 2 || p.Changes[1]["kind"] != "note" {
		t.Fatalf("aperçu : %d %s", w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(path); string(b) != src {
		t.Fatal("l'aperçu a écrit")
	}
	w = do(t, s, "POST", "/api/edit", req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	b, _ := os.ReadFile(path)
	if w.Code != http.StatusOK || noMeta(string(b)) != "Le {~~chat~>chien~~} dort{++[^1]++}.\n\nFin.\n{++\n[^1]: Depuis midi.\n++}" {
		t.Fatalf("enregistrement : %d %q", w.Code, b)
	}
	// One card: the definition is linked to the reference.
	var d struct {
		Annotations []struct {
			Kind     string `json:"kind"`
			Primary  bool   `json:"primary"`
			Partners []int  `json:"partners"`
			Note     string `json:"note"`
		} `json:"annotations"`
	}
	json.Unmarshal(do(t, s, "GET", "/api/doc", nil).Body.Bytes(), &d)
	if len(d.Annotations) != 3 || !d.Annotations[1].Primary || d.Annotations[1].Note != "Depuis midi." || len(d.Annotations[2].Partners) != 1 {
		t.Fatalf("%+v", d.Annotations)
	}
	// Undo (the range of the rewriting): the note goes with it.
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": resp["hash"], "start": resp["start"], "end": resp["end"], "decision": "reject"})
	if b, _ := os.ReadFile(path); w.Code != http.StatusOK || string(b) != src {
		t.Fatalf("annuler : %d %q", w.Code, b)
	}
}

// P3.2: the author corrects a proposal of hers three times; it stays one mark,
// with its identity and its thread; the journal keeps each previous version.
// The proposals of the agent are not corrected by the author.
func TestReviseOwnProposal(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "at": 3, "start": 0, "end": 13, "text": "Le chien dort."})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK {
		t.Fatalf("édition : %d %s", w.Code, w.Body.String())
	}
	type ann struct {
		ID       string            `json:"id"`
		Messages []json.RawMessage `json:"messages"`
	}
	first := func() (ann, int) {
		d := getDoc(t, s)
		var a ann
		if len(d.Annotations) > 0 {
			json.Unmarshal(d.Annotations[0], &a)
		}
		return a, len(d.Annotations)
	}
	a0, _ := first()
	id := a0.ID
	// A reply in its thread, then three corrections (the last one with a message).
	w = do(t, s, "POST", "/api/reply", map[string]any{"file": 0, "hash": resp["hash"], "start": 3, "text": "à revoir"})
	json.Unmarshal(w.Body.Bytes(), &resp)
	for i, text := range []string{"loup", "renard", "lynx"} {
		body := map[string]any{"file": 0, "hash": resp["hash"], "start": 3, "text": text}
		if i == 2 {
			body["note"] = "Dernière version."
		}
		w = do(t, s, "POST", "/api/revise", body)
		json.Unmarshal(w.Body.Bytes(), &resp)
		if w.Code != http.StatusOK || resp["start"] != 3.0 {
			t.Fatalf("correction %d : %d %s", i, w.Code, w.Body.String())
		}
	}
	if got := noMeta(read()); got != "Le {~~chat~>lynx~~}{>>à revoir<<}{>>Dernière version.<<} dort.\n\nFin.\n" {
		t.Fatalf("après trois corrections : %q", got)
	}
	if a, n := first(); n != 1 || a.ID != id || id == "" || len(a.Messages) != 2 {
		t.Fatalf("annotation : %d %+v", n, a)
	}
	// The journal keeps the three previous versions.
	j, _ := os.ReadFile(filepath.Join(dir, ".glose", "journal.jsonl"))
	for _, old := range []string{`"new":"chien"`, `"new":"loup"`, `"new":"renard"`} {
		if !strings.Contains(string(j), old) || !strings.Contains(string(j), `"revised"`) {
			t.Fatalf("version précédente %s absente du journal :\n%s", old, j)
		}
	}
	// Refusals: a correction that changes nothing; an agent's proposal.
	if w := do(t, s, "POST", "/api/revise", map[string]any{"file": 0, "hash": resp["hash"], "start": 3, "text": "lynx"}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("correction identique : %d", w.Code)
	}
	os.WriteFile(path, []byte("Le {~~chat~>chien~~}{>>glose:v1 id=k3fz9a by=agent<<} dort.\n"), 0o644)
	h := getDoc(t, s).Files[0].Hash
	if w := do(t, s, "POST", "/api/revise", map[string]any{"file": 0, "hash": h, "start": 3, "text": "loup"}); w.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(w.Body.String(), "vos propres propositions") {
		t.Fatalf("proposition de l'agent : %d %s", w.Code, w.Body.String())
	}
	// A proposal written by hand, without identity, receives one.
	os.WriteFile(path, []byte("Le {++petit ++}chat dort.\n"), 0o644)
	h = getDoc(t, s).Files[0].Hash
	w = do(t, s, "POST", "/api/revise", map[string]any{"file": 0, "hash": h, "start": 3, "text": "grand "})
	if got := read(); w.Code != http.StatusOK || noMeta(got) != "Le {++grand ++}chat dort.\n" || !strings.Contains(got, "by=author") {
		t.Fatalf("proposition écrite à la main : %d %q", w.Code, got)
	}
}

// P3.3: a structure (table, list, several paragraphs) is rewritten as whole
// lines, from its first word to its last; each change becomes a proposal at
// its place: a new row or item starts its line.
func TestRewriteAStructure(t *testing.T) {
	needPandoc(t)
	for _, c := range []struct{ name, prefix string }{
		{"sans_collision", ""},
		// A valid metadata identifier contains the word sought in the list.
		// A search in the whole source must not select it instead of the text.
		{"identifiant_un", "{>>Repère<<}{>>glose:v1 id=aunaaa by=author<<}\n\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := c.prefix + "Avant.\n\n| Nom | Valeur |\n|-----|--------|\n| a   | 1      |\n| c   | 3      |\n\n- un\n- trois\n\nFin.\n"
			s, dir := newServer(t, map[string]string{"a.md": src})
			path := filepath.Join(dir, "a.md")
			read := func() string {
				t.Helper()
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			word := func(d docResp, text string) (int, int) {
				t.Helper()
				w, err := findRenderedWord(d.HTML, read(), 0, text)
				if err != nil {
					t.Fatal(err)
				}
				return w.s, w.e
			}
			rewrite := func(first, last, text string) {
				t.Helper()
				d := getDoc(t, s)
				hash := d.Files[0].Hash
				at, _ := word(d, first)
				_, to := word(d, last)
				w := do(t, s, "GET", "/api/block?file=0&hash="+hash+"&at="+strconv.Itoa(at)+"&to="+strconv.Itoa(to), nil)
				var blk map[string]any
				json.Unmarshal(w.Body.Bytes(), &blk)
				if w.Code != http.StatusOK {
					t.Fatalf("structure %s…%s : %d %s", first, last, w.Code, w.Body.String())
				}
				old := blk["text"].(string)
				w = do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "mode": "lines", "at": at, "to": to,
					"start": blk["start"], "end": blk["end"], "text": strings.Replace(old, text[:strings.Index(text, "=>")], text[strings.Index(text, "=>")+2:], 1)})
				if w.Code != http.StatusOK {
					t.Fatalf("réécriture %s…%s : %d %s", first, last, w.Code, w.Body.String())
				}
			}
			// The table, from its header to its last cell: the whole lines.
			d := getDoc(t, s)
			hash := d.Files[0].Hash
			at, _ := word(d, "Nom")
			_, to := word(d, "3")
			w := do(t, s, "GET", "/api/block?file=0&hash="+hash+"&at="+strconv.Itoa(at)+"&to="+strconv.Itoa(to), nil)
			var blk map[string]any
			json.Unmarshal(w.Body.Bytes(), &blk)
			if w.Code != http.StatusOK || blk["text"] != "| Nom | Valeur |\n|-----|--------|\n| a   | 1      |\n| c   | 3      |" {
				t.Fatalf("tableau : %d %v", w.Code, blk)
			}
			// Two rows in one rewriting: one in the middle, one at the end.
			rewrite("Nom", "3", "| a   | 1      |\n| c   | 3      |=>| a   | 1      |\n| b   | 2      |\n| c   | 3      |\n| d   | 4      |")
			rewrite("un", "trois", "- un\n=>- un\n- deux\n")
			if got := noMeta(read()); got != noMeta(c.prefix)+"Avant.\n\n| Nom | Valeur |\n|-----|--------|\n| a   | 1      |\n{++| b   | 2      |\n++}| c   | 3      |\n{++| d   | 4      |\n++}\n"+
				"- un\n{++- deux\n++}- trois\n\nFin.\n" {
				t.Fatalf("propositions :\n%s", got)
			}
			// Bounds that are not words are refused.
			hash = getDoc(t, s).Files[0].Hash
			if w := do(t, s, "GET", "/api/block?file=0&hash="+hash+"&at=1&to=4", nil); w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("bornes hors des mots : %d", w.Code)
			}
		})
	}
}

// Adding an element before or after another (F8 ter): one addition, at the
// edge of the element's lines, separated by a blank line (a line ending for a
// list item); CRLF files keep their line endings.
func TestInsertAnElement(t *testing.T) {
	needPandoc(t)
	for _, c := range []struct {
		src, first, last, where, text string
		tight                         bool
		want                          string
	}{
		{"Un.\n\nDeux.\n", "Un.", "Un.", "after", "Nouveau.", false, "Un.\n\n{++Nouveau.\n\n++}Deux.\n"},
		{"Un.\n", "Un.", "Un.", "after", "Fin.", false, "Un.{++\n\nFin.++}\n"},
		{"Un.\n\nDeux.\n", "Deux.", "Deux.", "before", "| A | B |\n|---|---|\n| 1 | 2 |\n", false, "Un.\n\n{++| A | B |\n|---|---|\n| 1 | 2 |\n\n++}Deux.\n"},
		{"- a\n- b\n", "a", "a", "after", "- x", true, "- a\n{++- x\n++}- b\n"},
		{"Un.\r\n\r\nDeux.\r\n", "Un.", "Un.", "after", "Trois.", false, "Un.\r\n\r\n{++Trois.\r\n\r\n++}Deux.\r\n"},
		// The line of the element starts with the end of an addition: the new
		// element goes after it and its thread.
		{"{++Ajout.\n\n++}{>>pourquoi<<}Deux.\n", "Deux.", "Deux.", "before", "Avant.", false,
			"{++Ajout.\n\n++}{>>pourquoi<<}{++Avant.\n\n++}Deux.\n"},
	} {
		s, dir := newServer(t, map[string]string{"a.md": c.src})
		hash := getDoc(t, s).Files[0].Hash
		at := strings.Index(c.src, c.first)
		to := strings.Index(c.src, c.last) + len(c.last)
		w := do(t, s, "POST", "/api/edit", map[string]any{"file": 0, "hash": hash, "mode": "insert", "where": c.where, "tight": c.tight,
			"at": at, "to": to, "text": c.text})
		got, _ := os.ReadFile(filepath.Join(dir, "a.md"))
		if w.Code != http.StatusOK || noMeta(string(got)) != c.want {
			t.Errorf("%q %s : %d %s\n%q\nattendu\n%q", c.src, c.where, w.Code, w.Body.String(), noMeta(string(got)), c.want)
		}
	}
}

// The headings of the files displayed are kept with the progress, for the
// overview of the projects (P11.3): merged file by file, a file outside the
// project left out, and without effect on the marks of the sections.
func TestProgressHeadings(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "# Titre\n\nTexte.\n", "b.md": "# Autre\n"})
	path := filepath.Join(dir, ".glose", "progression.json")
	before, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#gl-titre", "title": "Titre", "status": "relue"})
	if w := do(t, s, "POST", "/api/progress", map[string]any{"headings": map[string]any{"a.md": []string{"gl-titre", "gl-suite"}}}); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := do(t, s, "POST", "/api/progress", map[string]any{"headings": map[string]any{"b.md": []string{}}}); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var pr Progress
	json.Unmarshal(do(t, s, "GET", "/api/progress", nil).Body.Bytes(), &pr)
	if len(pr.Headings["a.md"]) != 2 || pr.Headings["b.md"] == nil || pr.Sections["a.md#gl-titre"].Status != "relue" {
		t.Fatalf("%+v", pr)
	}
	// Unchanged headings write nothing.
	saved, _ := os.ReadFile(path)
	os.Chtimes(path, time.Unix(1000, 0), time.Unix(1000, 0))
	do(t, s, "POST", "/api/progress", map[string]any{"headings": map[string]any{"a.md": []string{"gl-titre", "gl-suite"}}})
	if fi, _ := os.Stat(path); fi.ModTime().Unix() != 1000 {
		t.Fatal("réécrit sans changement")
	}
	// A file outside the project is left out, without stopping the others.
	if w := do(t, s, "POST", "/api/progress", map[string]any{"headings": map[string]any{"../dehors.md": []string{"x"}, "b.md": []string{"gl-autre"}}}); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	pr = Progress{}
	json.Unmarshal(do(t, s, "GET", "/api/progress", nil).Body.Bytes(), &pr)
	if _, ok := pr.Headings["../dehors.md"]; ok || len(pr.Headings["b.md"]) != 1 {
		t.Fatalf("%+v", pr)
	}
	saved, _ = os.ReadFile(path)
	if w := do(t, s, "POST", "/api/progress", map[string]any{"headings": map[string]any{"a.md": []string{strings.Repeat("x", 1001)}}}); w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, saved) {
		t.Fatalf("modifié par un refus : %s", got)
	}
	// Marking a section keeps the headings.
	do(t, s, "POST", "/api/progress", map[string]any{"key": "b.md#gl-autre", "title": "Autre", "status": "en-cours"})
	pr = Progress{}
	json.Unmarshal(do(t, s, "GET", "/api/progress", nil).Body.Bytes(), &pr)
	if len(pr.Headings["b.md"]) != 1 {
		t.Fatalf("%+v", pr)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "a.md")); !bytes.Equal(after, before) {
		t.Fatal("texte touché")
	}
}

// The progress of the review: a section marked « relue », then « en cours »,
// then cleared; kept in .glose/progression.json, never in the text.
func TestProgress(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "# Titre\n\nTexte.\n"})
	var pr Progress
	json.Unmarshal(do(t, s, "GET", "/api/progress", nil).Body.Bytes(), &pr)
	if len(pr.Sections) != 0 {
		t.Fatalf("progression initiale : %+v", pr)
	}
	for _, st := range []string{"relue", "en-cours"} {
		w := do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#gl-titre", "title": "Titre", "status": st})
		json.Unmarshal(w.Body.Bytes(), &pr)
		if w.Code != http.StatusOK || pr.Sections["a.md#gl-titre"].Status != st {
			t.Fatalf("%s : %d %s", st, w.Code, w.Body.String())
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".glose", "progression.json"))
	if !strings.Contains(string(b), `"en-cours"`) {
		t.Fatalf("fichier : %s", b)
	}
	if src, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(src) != "# Titre\n\nTexte.\n" {
		t.Fatal("le texte a changé")
	}
	do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#gl-titre", "status": ""})
	pr = Progress{} // json.Unmarshal would merge into the map already there
	json.Unmarshal(do(t, s, "GET", "/api/progress", nil).Body.Bytes(), &pr)
	if len(pr.Sections) != 0 {
		t.Fatalf("après effacement : %+v", pr)
	}
	if w := do(t, s, "POST", "/api/progress", map[string]any{"key": "x", "status": "fini"}); w.Code != http.StatusBadRequest {
		t.Fatalf("état inconnu : %d", w.Code)
	}
	// Codex review (phase 4): an unreadable file is reported and kept as it
	// is, never replaced by an empty progress.
	bad := []byte("{\"version\": 1, \"sections\": {\"a.md#x\": {\"status\": \"relue\"},}}\n")
	path := filepath.Join(dir, ".glose", "progression.json")
	os.WriteFile(path, bad, 0o600)
	var rep struct{ Error string }
	if w := do(t, s, "GET", "/api/progress", nil); json.Unmarshal(w.Body.Bytes(), &rep) != nil || rep.Error == "" {
		t.Fatalf("fichier illisible non signalé : %s", w.Body.String())
	}
	if w := do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#y", "status": "relue"}); w.Code != http.StatusConflict {
		t.Fatalf("écriture sur un fichier illisible : %d", w.Code)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, bad) {
		t.Fatalf("fichier remplacé : %s", got)
	}
	// Contre-vérification : une version écrite par un autre programme
	// entre la lecture et l'écriture n'est pas remplacée.
	os.WriteFile(path, []byte(`{"version": 1, "sections": {}}`), 0o600)
	other := []byte(`{"version": 1, "sections": {"a.md#z": {"status": "relue", "time": ""}}}`)
	progressBeforeWrite = func() { os.WriteFile(path, other, 0o600) }
	defer func() { progressBeforeWrite = nil }()
	if w := do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#y", "status": "relue"}); w.Code != http.StatusConflict {
		t.Fatalf("écriture concurrente : %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, other) {
		t.Fatalf("version extérieure remplacée : %s", got)
	}
	// Même chose si le fichier n'existait pas à la lecture.
	os.Remove(path)
	if w := do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#y", "status": "relue"}); w.Code != http.StatusConflict {
		t.Fatalf("création concurrente : %d %s", w.Code, w.Body.String())
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, other) {
		t.Fatalf("fichier créé par un autre programme remplacé : %s", got)
	}
	progressBeforeWrite = nil
	os.Remove(path)
	if w := do(t, s, "POST", "/api/progress", map[string]any{"key": "a.md#y", "status": "relue"}); w.Code != http.StatusOK {
		t.Fatalf("création : %d %s", w.Code, w.Body.String())
	}
}

// A decision of the author can be undone: the proposal comes back with its
// identity and its thread; a decision on a file changed since cannot.
func TestUndoADecision(t *testing.T) {
	needPandoc(t)
	src := "Le {~~chat~>chien~~}{>>glose:v1 id=abcdef by=author<<}{>>Mauvaise idée.<<}{>>glose:v1 id=ghjkmn re=abcdef by=agent<<} dort.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	hash := getDoc(t, s).Files[0].Hash
	w := do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": hash, "start": 3, "decision": "accept"})
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if w.Code != http.StatusOK || read() != "Le chien dort.\n" {
		t.Fatalf("accepter : %d %q", w.Code, read())
	}
	w = do(t, s, "POST", "/api/undecide", map[string]any{"file": 0, "hash": resp["hash"]})
	if w.Code != http.StatusOK || read() != src {
		t.Fatalf("annuler : %d %s %q", w.Code, w.Body.String(), read())
	}
	// The journal holds the proposal open again.
	if _, h, _, err := s.N.State(); err != nil {
		t.Fatal(err)
	} else if _, closed := h.Closed()["abcdef"]; closed {
		t.Fatal("annotation rouverte encore tenue pour close")
	}
	// Once undone, the same decision cannot be undone twice.
	if w := do(t, s, "POST", "/api/undecide", map[string]any{"file": 0, "hash": resp["hash"]}); w.Code != http.StatusConflict {
		t.Fatalf("seconde annulation : %d", w.Code)
	}
	// A decision followed by another change: refused.
	hash = getDoc(t, s).Files[0].Hash
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": hash, "start": 3, "decision": "reject"})
	json.Unmarshal(w.Body.Bytes(), &resp)
	os.WriteFile(path, []byte("Le chat dort bien.\n"), 0o644)
	if w := do(t, s, "POST", "/api/undecide", map[string]any{"file": 0, "hash": resp["hash"]}); w.Code != http.StatusConflict {
		t.Fatalf("annulation après un changement : %d", w.Code)
	}
}

// A correction of the agent attached to a proposal of the author: the author
// takes it over (her proposal now proposes it), or accepts it at once.
func TestApplyAFixOfTheAgent(t *testing.T) {
	needPandoc(t)
	base := "Le principe{++ du fait d'autrui++}{>>glose:v1 id=abcdef by=author<<} est général.\n"
	e, at, err := review.Fix([]byte(base), "abcdef", " du fait des choses", review.Agent, review.NewGen(nil))
	if err != nil {
		t.Fatal(err)
	}
	src := string(e.Apply([]byte(base)))
	if !strings.Contains(src, "fix=1") || !strings.Contains(src, "{>> du fait des choses<<}") {
		t.Fatalf("correction écrite : %q", src)
	}
	for _, accept := range []bool{false, true} {
		s, dir := newServer(t, map[string]string{"a.md": src})
		path := filepath.Join(dir, "a.md")
		hash := getDoc(t, s).Files[0].Hash
		w := do(t, s, "POST", "/api/fix", map[string]any{"file": 0, "hash": hash, "start": strings.Index(src, "{++"), "message": at, "accept": accept})
		got, _ := os.ReadFile(path)
		want := "Le principe{++ du fait des choses++} est général.\n"
		if accept {
			want = "Le principe du fait des choses est général.\n"
		}
		if w.Code != http.StatusOK || noMeta(string(got)) != want {
			t.Fatalf("accept=%v : %d %s\n%q", accept, w.Code, w.Body.String(), got)
		}
		if !accept {
			continue
		}
		// Codex review (phase 4): undone, then redone by the same request
		// (what the interface replays on Ctrl+Y).
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		w = do(t, s, "POST", "/api/undecide", map[string]any{"file": 0, "hash": resp["hash"]})
		json.Unmarshal(w.Body.Bytes(), &resp)
		if got, _ := os.ReadFile(path); w.Code != http.StatusOK || string(got) != src {
			t.Fatalf("annuler : %d %s\n%q", w.Code, w.Body.String(), got)
		}
		w = do(t, s, "POST", "/api/fix", map[string]any{"file": 0, "hash": resp["hash"], "start": strings.Index(src, "{++"), "message": at, "accept": true})
		if got, _ := os.ReadFile(path); w.Code != http.StatusOK || noMeta(string(got)) != want {
			t.Fatalf("rétablir : %d %s\n%q", w.Code, w.Body.String(), got)
		}
	}
	// The agent cannot correct his own proposal this way, nor a comment.
	if _, _, err := review.Fix([]byte(strings.Replace(base, "by=author", "by=agent", 1)), "abcdef", "x", review.Agent, review.NewGen(nil)); err == nil {
		t.Fatal("correction de sa propre proposition acceptée")
	}
}

// D-32 : « Une seule proposition » dans le panneau : l'aperçu annonce une
// proposition, l'enregistrement l'écrit, l'acceptation donne la réécriture.
func TestEditAsOneProposal(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	hash := getDoc(t, s).Files[0].Hash
	req := map[string]any{"file": 0, "hash": hash, "at": 3, "start": 0, "end": 13, "text": "Le chien dort bien.", "whole": true}
	var p struct {
		Changes []map[string]any `json:"changes"`
	}
	w := do(t, s, "POST", "/api/preview", req)
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != http.StatusOK || len(p.Changes) != 1 || p.Changes[0]["new"] != "chien dort bien" {
		t.Fatalf("aperçu : %d %s", w.Code, w.Body.String())
	}
	w = do(t, s, "POST", "/api/edit", req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	got, _ := os.ReadFile(path)
	if w.Code != http.StatusOK || noMeta(string(got)) != "Le {~~chat dort~>chien dort bien~~}.\n\nFin.\n" {
		t.Fatalf("édition : %d %v %q", w.Code, resp, got)
	}
	w = do(t, s, "POST", "/api/decide", map[string]any{"file": 0, "hash": resp["hash"], "start": 3, "decision": "accept"})
	if got, _ := os.ReadFile(path); w.Code != http.StatusOK || string(got) != "Le chien dort bien.\n\nFin.\n" {
		t.Fatalf("accepter : %d %q", w.Code, got)
	}
}

// P9.1 : l'auteur modifie son commentaire et sa réponse tant qu'ils n'ont pas
// été remis ; le journal garde la version précédente ; une fois remis, le
// message ne se modifie plus.
func TestAmendOwnMessages(t *testing.T) {
	needPandoc(t)
	src := "Le chat dort.\n\nFin.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	path := filepath.Join(dir, "a.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	var resp map[string]any
	post := func(route string, body map[string]any) int {
		t.Helper()
		w := do(t, s, "POST", route, body)
		resp = map[string]any{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		return w.Code
	}
	hash := getDoc(t, s).Files[0].Hash
	if c := post("/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "passage", "start": 3, "end": 7, "text": "Lequel ?"}); c != http.StatusOK {
		t.Fatalf("commentaire : %d %v", c, resp)
	}
	before := read()
	at := strings.Index(before, "{>>Lequel")
	// Amended: only the text of the remark changes, identity included.
	if c := post("/api/amend", map[string]any{"file": 0, "hash": resp["hash"], "start": at, "text": "Quel chat ?"}); c != http.StatusOK || resp["start"] != float64(at) {
		t.Fatalf("modification : %d %v", c, resp)
	}
	if want := strings.Replace(before, "{>>Lequel ?<<}", "{>>Quel chat ?<<}", 1); read() != want {
		t.Fatalf("après modification : %q, attendu %q", read(), want)
	}
	// The same text again: refused, nothing written.
	if c := post("/api/amend", map[string]any{"file": 0, "hash": resp["hash"], "start": at, "text": "Quel chat ?"}); c != http.StatusUnprocessableEntity || resp["error"] != "same" {
		t.Fatalf("modification identique : %d %v", c, resp)
	}
	// Spaces are kept as given (an undo restores the message exactly), then
	// the message comes back.
	resp["hash"] = getDoc(t, s).Files[0].Hash
	for _, text := range []string{" Quel chat ? ", "Quel chat ?"} {
		if c := post("/api/amend", map[string]any{"file": 0, "hash": resp["hash"], "start": at, "text": text}); c != http.StatusOK ||
			!strings.Contains(read(), "{>>"+text+"<<}") {
			t.Fatalf("modification %q : %d %v %q", text, c, resp, read())
		}
	}
	if c := post("/api/amend", map[string]any{"file": 0, "hash": getDoc(t, s).Files[0].Hash, "start": at, "text": "  "}); c != http.StatusBadRequest {
		t.Fatalf("message vide : %d %v", c, resp)
	}
	// A reply of the author, amended too.
	hash = getDoc(t, s).Files[0].Hash
	if c := post("/api/reply", map[string]any{"file": 0, "hash": hash, "start": 3, "text": "Précision."}); c != http.StatusOK {
		t.Fatalf("réponse : %d %v", c, resp)
	}
	rep := resp["start"]
	if c := post("/api/amend", map[string]any{"file": 0, "hash": resp["hash"], "start": rep, "text": "Autre précision."}); c != http.StatusOK ||
		!strings.Contains(noMeta(read()), "{>>Quel chat ?<<}{>>Autre précision.<<}") {
		t.Fatalf("modification de la réponse : %d %v %q", c, resp, read())
	}
	// The journal keeps both previous versions.
	var lg struct{ Turns []navette.TurnLog }
	json.Unmarshal(do(t, s, "GET", "/api/log", nil).Body.Bytes(), &lg)
	var revised []string
	for _, o := range lg.Turns[0].Ops {
		for _, r := range o.Revised {
			if o.Kind == "amend" && r.Kind == "message" {
				revised = append(revised, r.Text)
			}
		}
	}
	if strings.Join(revised, "|") != "Lequel ?|Quel chat ?| Quel chat ? |Précision." {
		t.Fatalf("versions précédentes : %v", revised)
	}
	// Each message carries its status: new before the handover.
	var d struct {
		Annotations []struct {
			Messages []struct{ Status string } `json:"messages"`
		} `json:"annotations"`
	}
	json.Unmarshal(do(t, s, "GET", "/api/doc", nil).Body.Bytes(), &d)
	if len(d.Annotations) != 1 || len(d.Annotations[0].Messages) != 2 || d.Annotations[0].Messages[1].Status != "new" {
		t.Fatalf("statuts : %+v", d)
	}
	// Handed over, then taken back: the messages are the agent's to read now.
	if c := post("/api/handoff", map[string]any{"note": ""}); c != http.StatusOK {
		t.Fatalf("remise : %d %v", c, resp)
	}
	if c := post("/api/reclaim", map[string]any{"note": ""}); c != http.StatusOK {
		t.Fatalf("reprise : %d %v", c, resp)
	}
	handed := read()
	hash = getDoc(t, s).Files[0].Hash
	if c := post("/api/amend", map[string]any{"file": 0, "hash": hash, "start": at, "text": "Encore ?"}); c != http.StatusUnprocessableEntity || resp["error"] != "handed" {
		t.Fatalf("modification d'un message remis : %d %v", c, resp)
	}
	if read() != handed {
		t.Fatal("fichier modifié par un refus")
	}
	// A message of the agent cannot be amended by the author.
	agent := strings.Replace(handed, "\n\nFin.", "{>>Agent.<<}{>>glose:v1 id=zyxwvu by=agent<<}\n\nFin.", 1)
	os.WriteFile(path, []byte(agent), 0o644)
	hash = getDoc(t, s).Files[0].Hash
	if c := post("/api/amend", map[string]any{"file": 0, "hash": hash, "start": strings.Index(agent, "{>>Agent"), "text": "Moi."}); c != http.StatusUnprocessableEntity || resp["error"] != "refused" {
		t.Fatalf("modification d'un message de l'agent : %d %v", c, resp)
	}
	if read() != agent {
		t.Fatal("fichier modifié par un refus")
	}
}

// Audit A13: a body is one JSON object, nothing after it; nothing is written.
func TestDecodeRefusesTrailingData(t *testing.T) {
	needPandoc(t)
	src := "Un texte.\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	hash := getDoc(t, s).Files[0].Hash
	one := `{"file":0,"hash":"` + hash + `","mode":"passage","start":0,"end":2,"text":"x"}`
	for _, body := range []string{one + ` {"file":0}`, one + ` garbage`} {
		r := httptest.NewRequest("POST", "/api/comment", strings.NewReader(body))
		r.Host = host
		r.Header.Set("X-Glose-Token", token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%q : %d %s", body, w.Code, w.Body)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != src {
		t.Fatalf("écrit : %q", b)
	}
	r := httptest.NewRequest("POST", "/api/comment", strings.NewReader(one+"\n"))
	r.Host = host
	r.Header.Set("X-Glose-Token", token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("objet seul refusé : %d %s", w.Code, w.Body)
	}
}

// approveFilter approves the filter of the test server, as the author would
// (D-64).
func approveFilter(t *testing.T, s *Server) {
	t.Helper()
	if _, err := trust.Approve(s.P.Root, trust.Of(s.P.Root, s.P.Pandoc, s.P.Filter)); err != nil {
		t.Fatal(err)
	}
}

// Audit A12: the same project listens on the same port from one start to the
// next (same origin for the browser); a port taken falls back to a free one.
func TestListenForIsStable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projet")
	l, port, err := ListenFor(root)
	if err != nil {
		t.Fatal(err)
	}
	if port < stablePortBase || port >= stablePortBase+stablePortSpan {
		l.Close()
		t.Skipf("port stable occupé sur cette machine : %d", port)
	}
	// Taken (by the first one): another, free port.
	l2, port2, err := ListenFor(root)
	if err != nil || port2 == port {
		t.Fatalf("%d %v", port2, err)
	}
	l2.Close()
	l.Close()
	l3, port3, err := ListenFor(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l3.Close()
	if port3 != port {
		t.Fatalf("port changé : %d puis %d", port, port3)
	}
}

// P22.1: results by pages of 200.
func TestSearchPagesAndPart(t *testing.T) {
	a := strings.Repeat("Un mot. ", 300) + "\n"
	b := strings.Repeat("Un mot. ", 150) + "\n"
	s, _ := newServer(t, map[string]string{"a.md": a, "ch/b.md": b})
	page := func(q string) (n, total, next int, paths map[string]int) {
		t.Helper()
		w := do(t, s, "GET", "/api/search?q=mot"+q, nil)
		var out struct {
			Matches []struct{ Path string }
			Total   int
			Next    int
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s : %d %s", q, w.Code, w.Body)
		}
		paths = map[string]int{}
		for _, m := range out.Matches {
			paths[m.Path]++
		}
		return len(out.Matches), out.Total, out.Next, paths
	}
	if n, total, next, p := page(""); n != 200 || total != 450 || next != 200 || p["a.md"] != 200 {
		t.Fatalf("page 1 : %d %d %d %v", n, total, next, p)
	}
	if n, _, next, p := page("&offset=200"); n != 200 || next != 400 || p["a.md"] != 100 || p["ch/b.md"] != 100 {
		t.Fatalf("page 2 : %d %d %v", n, next, p)
	}
	if n, _, next, p := page("&offset=400"); n != 50 || next != 0 || p["ch/b.md"] != 50 {
		t.Fatalf("page 3 : %d %d %v", n, next, p)
	}
	if w := do(t, s, "GET", "/api/search?q=mot&offset=-1", nil); w.Code != 400 {
		t.Fatalf("décalage négatif : %d", w.Code)
	}
}
