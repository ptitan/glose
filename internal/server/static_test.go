package server

import (
	"github.com/ptitan/glose/web"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// P5.7 : chaque script et feuille de style que cite index.html est
// embarqué et servi avec son type. Un fichier ajouté en découpant app.js,
// mais oublié dans web/embed.go ou dans staticTypes, ferait échouer ce test
// au lieu de laisser une interface cassée.
func TestEveryAssetOfTheIndexIsServed(t *testing.T) {
	s, _ := newServer(t, map[string]string{"a.md": "Texte.\n"})
	page := do(t, s, "GET", "/", nil, noAuth()).Body.String()
	refs := regexp.MustCompile(`(?:src|href)="/static/([^"]+)"`).FindAllStringSubmatch(page, -1)
	if len(refs) < 5 {
		t.Fatalf("références trouvées : %v", refs)
	}
	for _, m := range refs {
		w := do(t, s, "GET", "/static/"+m[1], nil, noAuth())
		want := "text/javascript"
		if strings.HasSuffix(m[1], ".css") {
			want = "text/css"
		}
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), want) || w.Body.Len() == 0 {
			t.Errorf("/static/%s : %d %q", m[1], w.Code, w.Header().Get("Content-Type"))
		}
	}
}

// Every font declared by the stylesheet is embedded and served, its licence
// is embedded with it, and nothing in the stylesheet comes from outside.
func TestEveryFontOfTheStylesheetIsServed(t *testing.T) {
	s, _ := newServer(t, map[string]string{"a.md": "Texte.\n"})
	css := do(t, s, "GET", "/static/style.css", nil).Body.String()
	urls := regexp.MustCompile(`url\("([^"]+)"\)`).FindAllStringSubmatch(css, -1)
	if len(urls) < 14 {
		t.Fatalf("%d polices seulement", len(urls))
	}
	for _, m := range urls {
		if !strings.HasPrefix(m[1], "polices/") {
			t.Errorf("ressource hors de l'exécutable : %s", m[1])
			continue
		}
		w := do(t, s, "GET", "/static/"+m[1], nil)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "font/woff2" || w.Body.Len() < 1000 {
			t.Errorf("%s : %d, %s, %d octets", m[1], w.Code, w.Header().Get("Content-Type"), w.Body.Len())
		}
	}
	for _, name := range []string{"polices/LICENCE-Inter.txt", "polices/LICENCE-SourceSerif4.txt", "polices/LICENCE-LibertinusSerif.txt"} {
		if b, err := fs.ReadFile(web.Files, name); err != nil || !strings.Contains(string(b), "SIL OPEN FONT LICENSE") {
			t.Errorf("%s : %v", name, err)
		}
	}
	if w := do(t, s, "GET", "/static/polices/LICENCE-Inter.txt", nil); w.Code != http.StatusNotFound {
		t.Errorf("licence servie comme une police : %d", w.Code)
	}
	if w := do(t, s, "GET", "/static/polices/..%2fstyle.css", nil); w.Code == http.StatusOK && strings.Contains(w.Body.String(), "@font-face") {
		t.Error("sortie du dossier des polices")
	}
}

// Every theme offered by the interface is accepted by the server, and the
// reverse; every generated palette has its identifier in the list.
func TestThemesAgree(t *testing.T) {
	code, err := fs.ReadFile(web.Files, "themes.js")
	if err != nil {
		t.Fatal(err)
	}
	vus := map[string]bool{}
	for _, m := range regexp.MustCompile(`"id": "([a-z-]+)"`).FindAllStringSubmatch(string(code), -1) {
		vus[m[1]] = true
		if !themes[m[1]] {
			t.Errorf("thème refusé par le serveur : %s", m[1])
		}
	}
	for id := range themes {
		if !vus[id] {
			t.Errorf("thème sans entrée dans l'interface : %s", id)
		}
	}
	if len(vus) < 18 {
		t.Errorf("thèmes : %d", len(vus))
	}
}
