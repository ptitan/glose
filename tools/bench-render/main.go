// Command bench-render measures the rendering of a big synthetic book
// (P6.6): no real text in the repository. It generates a project in a
// temporary directory, then times the first render, the render after one
// file changed (as in the server), and the reopening with the disk cache.
//
//	go run ./tools/bench-render -files 30 -words 8000
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/render"
)

var syllables = []string{"la", "re", "ci", "to", "mé", "an", "di", "pro", "con", "tra", "jur", "is", "ment", "té", "ou", "ain", "ver", "sa", "lo", "nu"}

func word(r *rand.Rand) string {
	var b strings.Builder
	for n := 1 + r.Intn(3); n > 0; n-- {
		b.WriteString(syllables[r.Intn(len(syllables))])
	}
	return b.String()
}

// chapter writes about words words: titles, paragraphs with emphasis and
// footnotes, a list and a table now and then.
func chapter(r *rand.Rand, n, words int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Chapitre %d\n\n", n)
	notes, written := 0, 0
	for written < words {
		if r.Intn(12) == 0 {
			fmt.Fprintf(&b, "## Section %s\n\n", word(r))
		}
		switch r.Intn(10) {
		case 0:
			for i := 0; i < 3; i++ {
				fmt.Fprintf(&b, "- %s %s %s ;\n", word(r), word(r), word(r))
			}
			b.WriteString("\n")
			written += 9
			continue
		case 1:
			b.WriteString("| Article | Objet |\n|---|---|\n")
			for i := 0; i < 3; i++ {
				fmt.Fprintf(&b, "| %d | %s %s |\n", r.Intn(1000), word(r), word(r))
			}
			b.WriteString("\n")
			written += 9
			continue
		}
		size := 40 + r.Intn(80)
		for i := 0; i < size; i++ {
			w := word(r)
			if r.Intn(30) == 0 {
				w = "*" + w + "*"
			}
			b.WriteString(w)
			if r.Intn(50) == 0 {
				notes++
				fmt.Fprintf(&b, "[^n%d]", notes)
			}
			if i < size-1 {
				b.WriteString(" ")
			}
		}
		b.WriteString(".\n\n")
		written += size
	}
	for i := 1; i <= notes; i++ {
		fmt.Fprintf(&b, "[^n%d]: %s %s %s.\n", i, word(r), word(r), word(r))
	}
	return b.String()
}

func main() {
	files := flag.Int("files", 30, "nombre de fichiers")
	words := flag.Int("words", 8000, "mots par fichier")
	keep := flag.Bool("keep", false, "garder le projet généré")
	profile := flag.String("cpuprofile", "", "écrire le profil processeur de Glose dans ce fichier")
	flag.Parse()
	if *profile != "" {
		f, err := os.Create(*profile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	dir, err := os.MkdirTemp("", "glose-bench-")
	if err != nil {
		panic(err)
	}
	if !*keep {
		defer os.RemoveAll(dir)
	}
	r := rand.New(rand.NewSource(1))
	for i := 1; i <= *files; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d.md", i)), []byte(chapter(r, i, *words)), 0o644)
	}
	p, err := project.Load(dir)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	measure := func(what string, rd *render.Renderer) {
		t := time.Now()
		if _, err := rd.Render(ctx); err != nil {
			panic(err)
		}
		fmt.Printf("%-42s %6.2f s\n", what, time.Since(t).Seconds())
	}
	fmt.Printf("Projet : %d fichiers, environ %d mots (%s)\n", *files, *files**words, dir)
	rd := render.New(p)
	rd.EnableDiskCache(ctx)
	measure("premier rendu", rd)
	f := p.Path(*files / 2)
	b, _ := os.ReadFile(f)
	os.WriteFile(f, append(b, []byte("\nUn ajout.\n")...), 0o644)
	measure("après la modification d'un fichier", rd)
	re := render.New(p)
	re.EnableDiskCache(ctx)
	measure("réouverture (cache disque)", re)
}
