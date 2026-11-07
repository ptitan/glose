package server

import (
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
