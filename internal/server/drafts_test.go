package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

const draftID = "00112233445566778899aabbccddeeff"

func sampleDraft(src string) *Draft {
	return &Draft{Kind: "edit", Path: "a.md", Hash: store.Hash([]byte(src)), Mode: "block", Start: 0, End: len(src),
		Original: src, Text: "Autre texte é 👩🏽‍💻", Note: "Message pour l'agent", Editor: "source", Whole: true}
}

func saveDraft(t *testing.T, s *Server, req draftRequest, code int) draftReply {
	t.Helper()
	w := do(t, s, "POST", "/api/drafts", req)
	if w.Code != code {
		t.Fatalf("brouillon : %d %s (attendu %d)", w.Code, w.Body.String(), code)
	}
	var rep draftReply
	if code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
			t.Fatal(err)
		}
	}
	return rep
}

func TestDraftPersistenceAndFidelity(t *testing.T) {
	for _, src := range []string{"Texte\n", "\ufeffTexte\r\nSuite\nFin\r\n"} {
		t.Run(store.Hash([]byte(src))[:6], func(t *testing.T) {
			s, dir := newServer(t, map[string]string{"a.md": src})
			// A real history exists: no draft operation may append to it.
			if _, err := s.N.Handoff(review.Author, "Remise"); err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(dir, ".glose", "journal.jsonl")
			journal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			assertUnchanged := func() {
				t.Helper()
				got, _ := os.ReadFile(filepath.Join(dir, "a.md"))
				if !bytes.Equal(got, []byte(src)) {
					t.Fatalf("manuscrit modifié : %q", got)
				}
				got, _ = os.ReadFile(journalPath)
				if !bytes.Equal(got, journal) {
					t.Fatal("journal modifié par un brouillon")
				}
			}
			d := sampleDraft(src)
			rep := saveDraft(t, s, draftRequest{ID: draftID, Draft: d}, http.StatusOK)
			assertUnchanged()
			// Recreate the server, with another token and another port.
			s = New(s.P, port+1, "autre-session")
			w := do(t, s, "GET", "/api/drafts", nil, withHost("127.0.0.1:4243"), withHeader("X-Glose-Token", "autre-session"))
			var list struct{ Drafts []draftReply }
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Drafts) != 1 || list.Drafts[0].Draft.Text != d.Text {
				t.Fatalf("récupération : %s (%v)", w.Body.String(), err)
			}
			s.Port, s.Token = port, token // remaining requests use the standard helper
			// A changed/missing target must not prevent preserving the input.
			// Its old hash is kept; the UI will refuse automatic reapplication.
			d.Text = ""
			d.Sending = true
			rep = saveDraft(t, s, draftRequest{ID: draftID, Hash: rep.Hash, Draft: d}, http.StatusOK)
			if rep.Draft.Hash != store.Hash([]byte(src)) || !rep.Draft.Sending || rep.Draft.Text != "" {
				t.Fatal("suppression proposée ou contexte perdu")
			}
			assertUnchanged()
			saveDraft(t, s, draftRequest{ID: draftID, Hash: rep.Hash, Remove: true}, http.StatusOK)
			assertUnchanged()
			w = do(t, s, "GET", "/api/drafts", nil)
			if strings.Contains(w.Body.String(), d.Note) || !strings.Contains(w.Body.String(), `"drafts":[]`) {
				t.Fatalf("brouillon supprimé encore exposé : %s", w.Body.String())
			}
			b, _ := os.ReadFile(filepath.Join(dir, ".glose", "brouillons", draftID+".json"))
			if bytes.Contains(b, []byte(d.Note)) || !bytes.Contains(b, []byte(`"deleted":true`)) {
				t.Fatal("la suppression doit garder une sentinelle sans la saisie")
			}
		})
	}
}

func TestDraftConflictsAndRetries(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte"})
	d := sampleDraft("Texte")
	initial := draftRequest{ID: draftID, Draft: d}
	one := saveDraft(t, s, initial, http.StatusOK)
	// Retrying after a lost response returns the same version, not a second draft.
	if two := saveDraft(t, s, initial, http.StatusOK); two.Hash != one.Hash {
		t.Fatal("réessai non idempotent")
	}
	d.Text = "Modification du premier onglet"
	two := saveDraft(t, s, draftRequest{ID: draftID, Hash: one.Hash, Draft: d}, http.StatusOK)
	d.Text = "Modification du second onglet"
	saveDraft(t, s, draftRequest{ID: draftID, Hash: one.Hash, Draft: d}, http.StatusConflict)
	saveDraft(t, s, draftRequest{ID: draftID, Hash: one.Hash, Remove: true}, http.StatusConflict)
	deleted := saveDraft(t, s, draftRequest{ID: draftID, Hash: two.Hash, Remove: true}, http.StatusOK)
	saveDraft(t, s, draftRequest{ID: draftID, Hash: two.Hash, Remove: true}, http.StatusOK)
	// Neither an old update nor a delayed creation can resurrect a tombstone.
	saveDraft(t, s, draftRequest{ID: draftID, Hash: two.Hash, Draft: d}, http.StatusConflict)
	saveDraft(t, s, draftRequest{ID: draftID, Draft: d}, http.StatusConflict)
	if got, _ := store.Read(filepath.Join(dir, ".glose", "brouillons", draftID+".json")); got.Hash != deleted.Hash {
		t.Fatal("sentinelle modifiée par un réessai refusé")
	}
}

func TestDraftSecurityAndCorruption(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte"})
	if w := do(t, s, "GET", "/api/drafts", nil, noAuth()); w.Code != http.StatusUnauthorized {
		t.Fatal("brouillons publics")
	}
	d := sampleDraft("Texte")
	for _, id := range []string{"../a", "", strings.Repeat("f", 33)} {
		saveDraft(t, s, draftRequest{ID: id, Draft: d}, http.StatusBadRequest)
	}
	d.Path = "../a.md"
	saveDraft(t, s, draftRequest{ID: draftID, Draft: d}, http.StatusBadRequest)
	d.Path = "a.md"
	one := saveDraft(t, s, draftRequest{ID: draftID, Draft: d}, http.StatusOK)
	path := filepath.Join(dir, ".glose", "brouillons", draftID+".json")
	bad := []byte("{interruption")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	saveDraft(t, s, draftRequest{ID: draftID, Hash: one.Hash, Draft: d}, http.StatusConflict)
	w := do(t, s, "GET", "/api/drafts", nil)
	if !strings.Contains(w.Body.String(), "illisible") {
		t.Fatal("corruption non signalée")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, bad) {
		t.Fatal("brouillon illisible écrasé")
	}
}

func TestDraftConcurrentTabs(t *testing.T) {
	s, _ := newServer(t, map[string]string{"a.md": "Texte"})
	one := saveDraft(t, s, draftRequest{ID: draftID, Draft: sampleDraft("Texte")}, http.StatusOK)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, text := range []string{"Onglet A", "Onglet B"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			d := sampleDraft("Texte")
			d.Text = text
			codes <- do(t, s, "POST", "/api/drafts", draftRequest{ID: draftID, Hash: one.Hash, Draft: d}).Code
		}(text)
	}
	wg.Wait()
	a, b := <-codes, <-codes
	if a+b != http.StatusOK+http.StatusConflict {
		t.Fatalf("deux écritures concurrentes : %d, %d", a, b)
	}
}

func TestDraftMissingTargetIsKept(t *testing.T) {
	s, _ := newServer(t, map[string]string{"a.md": "Texte"})
	d := sampleDraft("Texte")
	one := saveDraft(t, s, draftRequest{ID: draftID, Draft: d}, http.StatusOK)
	if err := os.Remove(s.P.Path(0)); err != nil {
		t.Fatal(err)
	}
	d.Note = "Le fichier a disparu, pas ma saisie"
	two := saveDraft(t, s, draftRequest{ID: draftID, Hash: one.Hash, Draft: d}, http.StatusOK)
	if two.Draft.Hash != d.Hash {
		t.Fatal("empreinte d'origine modifiée")
	}
	if _, err := os.Stat(s.P.Path(0)); !os.IsNotExist(err) {
		t.Fatal("la sauvegarde du brouillon a recréé le manuscrit")
	}
}

func TestDraftDirectoryCannotEscape(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte"})
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".glose"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(dir, ".glose", "brouillons")); err != nil {
		t.Skip("liens symboliques indisponibles : " + err.Error())
	}
	saveDraft(t, s, draftRequest{ID: draftID, Draft: sampleDraft("Texte")}, http.StatusConflict)
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 0 {
		t.Fatal("écriture hors du projet")
	}
}

// P5.4 : un brouillon écrit dont seul le dossier n'a pu être synchronisé
// est confirmé ; sa version sert à la sauvegarde suivante, sans conflit
// avec sa propre écriture.
func TestDraftWrittenButDirectoryNotSynced(t *testing.T) {
	s, dir := newServer(t, map[string]string{"a.md": "Texte"})
	drafts := filepath.Join(dir, ".glose", "brouillons")
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		drafts = filepath.Join(real, ".glose", "brouillons")
	}
	t.Cleanup(store.SetSyncFailure(func(p string) error {
		if filepath.Clean(p) == drafts {
			return os.ErrPermission
		}
		return nil
	}))
	d := sampleDraft("Texte")
	one := saveDraft(t, s, draftRequest{ID: draftID, Draft: d}, http.StatusOK)
	d.Text = "Suite de la saisie"
	two := saveDraft(t, s, draftRequest{ID: draftID, Hash: one.Hash, Draft: d}, http.StatusOK)
	if got, _ := store.Read(filepath.Join(drafts, draftID+".json")); got.Hash != two.Hash {
		t.Fatal("brouillon sur le disque différent de la version confirmée")
	}
}

// The browser sends « < » as one byte, the record stores it as six
// (<): a draft accepted under the request limit must not become
// unreadable once written. It is refused, and nothing is written.
func TestDraftTooBigOnceEscapedIsRefused(t *testing.T) {
	src := "Texte\n"
	s, dir := newServer(t, map[string]string{"a.md": src})
	d := sampleDraft(src)
	d.Text = strings.Repeat("<", 200_000)
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(draftRequest{ID: draftID, Draft: d}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/drafts", &body)
	r.Host = host
	r.Header.Set("X-Glose-Token", token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("brouillon trop gros : %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".glose", "brouillons", draftID+".json")); !os.IsNotExist(err) {
		t.Fatalf("brouillon écrit malgré le refus : %v", err)
	}
	if rep := do(t, s, "GET", "/api/drafts", nil); strings.Contains(rep.Body.String(), "illisible") {
		t.Fatal(rep.Body.String())
	}
}
