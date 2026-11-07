package render

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/store"
)

func withCounter(t *testing.T) (wrapper string, count func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("enveloppe de pandoc en script shell")
	}
	log := filepath.Join(t.TempDir(), "appels")
	wrapper = filepath.Join(t.TempDir(), "pandoc")
	os.WriteFile(wrapper, []byte("#!/bin/sh\necho x >> "+log+"\nexec pandoc \"$@\"\n"), 0o755)
	return wrapper, func() int { b, _ := os.ReadFile(log); return strings.Count(string(b), "x") }
}

// P6.4 : un projet rouvert sans changement n'est pas relu par pandoc ; le
// rendu est identique. Une entrée abîmée est ignorée et recalculée ; un
// fichier modifié est seul relu.
func TestDiskCache(t *testing.T) {
	needPandoc(t)
	wrapper, count := withCounter(t)
	p := mkproject(t, map[string]string{"a.md": "Texte a[^1].\n\n[^1]: Note.\n", "b.md": "Texte *b*.\n", "c.md": "Texte c.\n"})
	p.Pandoc = wrapper
	open := func() (*Renderer, int) {
		r := New(p)
		r.EnableDiskCache(context.Background())
		if r.disk == nil {
			t.Fatal("cache disque non activé")
		}
		return r, count()
	}
	r, n0 := open()
	first, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, n0 = open()
	again, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := count() - n0; n != 0 {
		t.Fatalf("réouverture sans changement : %d appels à pandoc", n)
	}
	if again.HTML != first.HTML {
		t.Fatal("rendu repris du cache différent")
	}
	checkContract(t, p, again)
	if _, err := r.Map(context.Background(), 1, mustRead(t, p.Path(1))); err != nil {
		t.Fatalf("carte reprise du cache : %v", err)
	}

	// Entrées abîmées : un octet changé dans chacune.
	filepath.WalkDir(filepath.Join(p.State(), CacheDir), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(path)
			b[len(b)/2] ^= 0x20
			os.WriteFile(path, b, 0o600)
		}
		return nil
	})
	r, n0 = open()
	damaged, err := r.Render(context.Background())
	if err != nil || damaged.HTML != first.HTML {
		t.Fatalf("rendu avec un cache abîmé : %v", err)
	}
	if count()-n0 != 6 {
		t.Fatalf("cache abîmé : %d appels, 6 attendus", count()-n0)
	}

	// Un fichier modifié : seul lui est relu.
	os.WriteFile(p.Path(2), []byte("Texte c modifié.\n"), 0o644)
	r, n0 = open()
	if _, err := r.Render(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := count() - n0; n != 2 {
		t.Fatalf("un fichier modifié : %d appels, 2 attendus", n)
	}
}

// Le cache reste dans ses bornes : les entrées les plus anciennes partent.
func TestDiskCacheIsBounded(t *testing.T) {
	needPandoc(t)
	defer func(m int64) { cacheMax = m }(cacheMax)
	cacheMax = 1
	files := map[string]string{}
	for _, f := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"} {
		files[f+".md"] = "Texte " + f + ".\n"
	}
	p := mkproject(t, files)
	r := New(p)
	r.EnableDiskCache(context.Background())
	if _, err := r.Render(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	filepath.WalkDir(filepath.Join(p.State(), CacheDir), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	if n >= 22 {
		t.Fatalf("cache non borné : %d entrées", n)
	}
}

func mustRead(t *testing.T, path string) store.Doc {
	t.Helper()
	d, err := store.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
