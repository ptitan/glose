package navette

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

// P5.4 : si les copies de remise ne peuvent être synchronisées, la remise
// est refusée et aucun événement de tour n'est écrit (le journal ne nomme
// jamais une copie qui pourrait manquer). Une nouvelle tentative réussit,
// et la conversion des modifications de l'agent s'appuie sur ces copies.
func TestHandoffWaitsForDurableCopies(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Un texte à relire.\n"})
	snaps := filepath.Join(w.dir, ".glose", journal.SnapshotDir)
	restore := store.SetSyncFailure(func(p string) error {
		if p == snaps {
			return errors.New("disque indisponible")
		}
		return nil
	})
	if _, err := w.n.Handoff(review.Author, "À toi."); err == nil {
		restore()
		t.Fatalf("remise acceptée sans copies durables : %v", err)
	}
	restore()
	if st, _, _, _ := w.n.State(); st.Hand != review.Author {
		t.Fatalf("la main a changé malgré l'échec : %+v", st)
	}
	var flushed []string
	t.Cleanup(store.SetSyncFailure(func(p string) error { flushed = append(flushed, p); return nil }))
	if _, err := w.n.Handoff(review.Author, "À toi."); err != nil {
		t.Fatal(err)
	}
	synced := false
	for _, p := range flushed {
		if p == snaps {
			synced = true
		}
	}
	if !synced {
		t.Fatalf("dossier des copies non synchronisé : %q", flushed)
	}
	w.write("a.md", "Un texte relu.\n")
	if _, err := w.n.Handoff(review.Agent, "Corrigé."); err != nil {
		t.Fatal(err)
	}
	if got := w.read("a.md"); !strings.Contains(got, "{~~à relire~>relu~~}") && !strings.Contains(got, "{--à relire--}") {
		t.Fatalf("modification de l'agent non convertie : %q", got)
	}
}
