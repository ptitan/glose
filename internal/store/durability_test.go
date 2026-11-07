package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errFlush = errors.New("synchronisation simulée en échec")

// failOn makes the flushes of the paths for which match is true fail.
func failOn(t *testing.T, match func(path string) bool) {
	t.Helper()
	t.Cleanup(SetSyncFailure(func(p string) error {
		if match(p) {
			return errFlush
		}
		return nil
	}))
}

func isTemp(p string) bool { return strings.HasSuffix(p, ".tmp") }

// P5.4 : un fichier temporaire qui ne peut être écrit sur le disque n'est
// jamais mis à la place de l'original ; rien n'est laissé derrière.
func TestFailedFileFlushKeepsTheOriginal(t *testing.T) {
	dir, path := setup(t)
	failOn(t, isTemp)
	if _, err := WriteIfUnchanged(path, Hash(original), []byte("nouveau")); !errors.Is(err, errFlush) {
		t.Fatalf("WriteIfUnchanged : %v", err)
	}
	state := filepath.Join(dir, "etat.json")
	os.WriteFile(state, []byte("ancien"), 0o600)
	if err := WriteAtomic(state, []byte("nouveau"), 0o600); !errors.Is(err, errFlush) {
		t.Fatalf("WriteAtomic : %v", err)
	}
	if err := CreateAtomic(filepath.Join(dir, "neuf.json"), []byte("x"), 0o600); !errors.Is(err, errFlush) {
		t.Fatalf("CreateAtomic : %v", err)
	}
	if !bytes.Equal(content(t, path), original) || string(content(t, state)) != "ancien" {
		t.Fatal("original modifié")
	}
	if _, err := os.Stat(filepath.Join(dir, "neuf.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fichier créé malgré l'échec")
	}
	noTemporaryFiles(t, dir)
}

// P5.4 : quand seul le dossier ne peut être synchronisé, le nouveau contenu
// est en place et l'appelant le sait (ErrNotSynced).
func TestFailedDirectoryFlushIsReported(t *testing.T) {
	dir, path := setup(t)
	failOn(t, func(p string) bool { return p == dir })
	h, err := WriteIfUnchanged(path, Hash(original), []byte("nouveau"))
	if !errors.Is(err, ErrNotSynced) || h != Hash([]byte("nouveau")) || string(content(t, path)) != "nouveau" {
		t.Fatalf("WriteIfUnchanged : %q, %v", h, err)
	}
	state := filepath.Join(dir, "etat.json")
	if err := WriteAtomic(state, []byte("a"), 0o600); !errors.Is(err, ErrNotSynced) || string(content(t, state)) != "a" {
		t.Fatalf("WriteAtomic : %v", err)
	}
	created := filepath.Join(dir, "neuf.json")
	if err := CreateAtomic(created, []byte("b"), 0o600); !errors.Is(err, ErrNotSynced) || string(content(t, created)) != "b" {
		t.Fatalf("CreateAtomic : %v", err)
	}
	noTemporaryFiles(t, dir)
}

// P5.4 : chaque écriture synchronise le fichier puis son dossier.
func TestEveryWriteFlushesFileAndDirectory(t *testing.T) {
	dir, path := setup(t)
	var flushed []string
	t.Cleanup(SetSyncFailure(func(p string) error { flushed = append(flushed, p); return nil }))
	check := func(what string) {
		t.Helper()
		if len(flushed) != 2 || !isTemp(flushed[0]) || flushed[1] != dir {
			t.Errorf("%s : synchronisations %q", what, flushed)
		}
		flushed = nil
	}
	WriteIfUnchanged(path, Hash(original), []byte("x"))
	check("WriteIfUnchanged")
	WriteAtomic(filepath.Join(dir, "a.json"), []byte("x"), 0o600)
	check("WriteAtomic")
	CreateAtomic(filepath.Join(dir, "b.json"), []byte("x"), 0o600)
	check("CreateAtomic")
}

// P5.4 : le journal créé est inscrit durablement dans son dossier ; si
// cela échoue, une nouvelle tentative le refait tant qu'il est vide.
func TestJournalCreationFlushesItsDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")
	fail := true
	t.Cleanup(SetSyncFailure(func(p string) error {
		if p == dir && fail {
			return errFlush
		}
		return nil
	}))
	if _, err := OpenJournal(path); !errors.Is(err, ErrNotSynced) {
		t.Fatalf("première ouverture : %v", err)
	}
	fail = false
	var flushed []string
	t.Cleanup(SetSyncFailure(func(p string) error { flushed = append(flushed, p); return nil }))
	f, err := OpenJournal(path)
	if err != nil || len(flushed) != 2 || flushed[0] != dir || flushed[1] != filepath.Dir(dir) {
		t.Fatalf("nouvelle tentative : %v, %q", err, flushed)
	}
	f.WriteString("{}\n")
	f.Close()
	flushed = nil
	if f, err := OpenJournal(path); err != nil || len(flushed) != 0 {
		t.Fatalf("journal non vide : %v, %q", err, flushed)
	} else {
		f.Close()
	}
}

// Revue de Codex (P5.4) : un dossier créé pour un fichier est inscrit
// durablement dans son parent, du plus haut au plus bas.
func TestNewDirectoriesAreFlushedInTheirParents(t *testing.T) {
	root := t.TempDir()
	var flushed []string
	t.Cleanup(SetSyncFailure(func(p string) error {
		if !isTemp(p) {
			flushed = append(flushed, p)
		}
		return nil
	}))
	state := filepath.Join(root, ".glose", "snapshots")
	if err := WriteAtomic(filepath.Join(state, "abc"), []byte("copie"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{root, filepath.Join(root, ".glose"), state}
	if strings.Join(flushed, "|") != strings.Join(want, "|") {
		t.Fatalf("synchronisations %q, attendu %q", flushed, want)
	}
	// Le parent ne peut être synchronisé : le fichier est écrit, l'appelant
	// le sait.
	t.Cleanup(SetSyncFailure(func(p string) error {
		if p == root {
			return errFlush
		}
		return nil
	}))
	other := filepath.Join(root, "autre", "x.json")
	if err := CreateAtomic(other, []byte("x"), 0o600); !errors.Is(err, ErrNotSynced) || string(content(t, other)) != "x" {
		t.Fatalf("CreateAtomic : %v", err)
	}
}
