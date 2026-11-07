package server

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/review"
)

// D-38 : un fichier créé après le démarrage du serveur (chapitre écrit par
// l'agent) entre dans le projet affiché ; ses propositions sont visibles et
// se décident dans l'interface, sans relancer Glose.
func TestServerSeesAFileCreatedAfterStart(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte a.\n"})
	if _, err := s.N.Handoff(review.Author, "Écris la suite."); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Suite écrite par l'agent.\n"), 0o644)
	if _, err := s.cur().N.Handoff(review.Agent, "Suite."); err != nil {
		t.Fatal(err)
	}
	s.refresh() // ce que fait l'interrogation suivante de l'interface, en arrière-plan
	var hs struct{ Files []struct{ Index int } }
	w := do(t, s, "GET", "/api/hashes", nil)
	json.Unmarshal(w.Body.Bytes(), &hs)
	if len(hs.Files) != 2 {
		t.Fatalf("fichiers après création : %s", w.Body.String())
	}
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent : rendu non vérifié")
	}
	w = do(t, s, "GET", "/api/doc", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data-path=\"b.md\"`) && !strings.Contains(w.Body.String(), "b.md") {
		t.Fatalf("b.md absent du rendu : %d", w.Code)
	}
	var doc struct {
		Annotations []struct {
			File     int
			Kind, By string
		}
	}
	json.Unmarshal(w.Body.Bytes(), &doc)
	found := false
	for _, a := range doc.Annotations {
		if a.File == 1 && a.Kind == "addition" && a.By == "agent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("proposition d'ajout de b.md absente : %+v", doc.Annotations)
	}
}

// Les requêtes en cours pendant un changement de la liste des fichiers ne
// partagent pas d'état sans protection (go test -race), et ne paniquent pas.
func TestRefreshDuringRequests(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b.\n"})
	var wg sync.WaitGroup
	for k := 0; k < 4; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				switch (i + k) % 3 {
				case 0:
					do(t, s, "GET", "/api/hashes", nil)
				case 1:
					do(t, s, "GET", "/api/state", nil)
				case 2:
					do(t, s, "GET", "/api/block?file=1&hash=x&at=0", nil)
				}
			}
		}(k)
	}
	for i := 0; i < 10; i++ {
		name := filepath.Join(dir, "c.md")
		if i%2 == 0 {
			os.WriteFile(name, []byte("Texte c.\n"), 0o644)
		} else {
			os.Remove(name)
		}
		s.refresh()
		do(t, s, "GET", "/api/hashes", nil)
	}
	wg.Wait()
}

// Revue de Codex : deux fichiers identiques échangés dans le profil ont les
// mêmes empreintes ; une requête faite sur l'ancienne liste est refusée
// (« files ») au lieu d'écrire dans l'autre fichier.
func TestRequestOnAnOldListOfFilesIsRefused(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent")
	}
	same := "Texte identique.\n"
	s, dir := newServer(t, map[string]string{"a.md": same, "b.md": same, "glose.json": `{"files":["a.md","b.md"]}`})
	gen := strconv.FormatInt(s.cur().Gen, 10)
	os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"files":["b.md","a.md"]}`), 0o644)
	s.refresh() // le serveur relit la liste
	if s.cur().Gen == 0 {
		t.Fatal("liste non relue")
	}
	hash := s.cur().N.ReadFiles().Docs[0].Hash
	w := do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "passage",
		"start": 0, "end": 5, "text": "Note"}, withHeader(filesHeader, gen))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"files"`) {
		t.Fatalf("requête sur l'ancienne liste : %d %s", w.Code, w.Body.String())
	}
	for _, f := range []string{"a.md", "b.md"} {
		if b, _ := os.ReadFile(filepath.Join(dir, f)); string(b) != same {
			t.Fatalf("%s modifié : %q", f, b)
		}
	}
	// Avec la génération actuelle, la même requête passe.
	w = do(t, s, "POST", "/api/comment", map[string]any{"file": 0, "hash": hash, "mode": "passage",
		"start": 0, "end": 5, "text": "Note"}, withHeader(filesHeader, strconv.FormatInt(s.cur().Gen, 10)))
	if w.Code != http.StatusOK {
		t.Fatalf("requête sur la liste actuelle : %d %s", w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.md")); !strings.Contains(string(b), "{>>Note<<}") {
		t.Fatalf("commentaire écrit ailleurs que dans b.md (premier de la nouvelle liste) : %q", b)
	}
}

// Le rechargement se fait en arrière-plan, espacé (un gros dossier sur un
// disque lent ne doit pas ralentir chaque interrogation) ; l'interrogation
// suivante en voit le résultat.
func TestRefreshRunsInTheBackgroundAndIsSpaced(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte a.\n"})
	defer func(d time.Duration) { refreshEvery = d }(refreshEvery)
	refreshEvery = time.Hour
	s.lastRefresh.Store(time.Now().UnixNano())
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("b\n"), 0o644)
	do(t, s, "GET", "/api/hashes", nil)
	time.Sleep(50 * time.Millisecond)
	if len(s.cur().P.Files) != 1 {
		t.Fatal("rechargement avant son délai")
	}
	refreshEvery = 0
	deadline := time.Now().Add(5 * time.Second)
	for len(s.cur().P.Files) != 2 {
		if time.Now().After(deadline) {
			t.Fatal("fichier créé jamais vu")
		}
		do(t, s, "GET", "/api/hashes", nil)
		time.Sleep(10 * time.Millisecond)
	}
}
