package projection

import (
	"bytes"
	"context"
	"os/exec"
	"testing"

	"github.com/ptitan/glose/internal/sourcepos"
)

// TestWholeChain runs the full P0 chain on an annotated text: projection,
// pandoc, positions, and back to the source. Every certified word of every
// view must lead back to source bytes identical to the word.
func TestWholeChain(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent : test sauté")
	}
	src := []byte("# Première partie — La technique\r\n\r\n" +
		"{#p-x} **Fonctionnement.** Le juge {~~rejette~>casse~~}{>>pourquoi ?<<} le " +
		"{++nouveau ++}pourvoi (*supra*, n° [](#p-y)) {--très --}vite.\r\n\r\n" +
		"> **À retenir** — {==Aucune étape==}{>>à vérifier<<} ne vérifie l’état du droit.\r\n\r\n" +
		"Terme\r\n:   Définition {++ajoutée++} ici.\r\n")
	for _, v := range []View{Current, Proposed, Review} {
		p := Project(src, v)
		x, err := sourcepos.NewIndex(p.Text)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := sourcepos.Run(context.Background(), "pandoc", p.Text)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := sourcepos.Nodes(tree)
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for _, l := range sourcepos.Locate(x, nodes) {
			if l.Kind != sourcepos.KindStr || l.Status != sourcepos.Certified {
				continue
			}
			s, segS, ok1 := p.SourceStart(l.Start)
			e, segE, ok2 := p.SourceEnd(l.End)
			if !ok1 || !ok2 {
				t.Fatalf("vue %s : %q hors de la projection", v, l.Text)
			}
			if segS.Out != segE.Out {
				// The word straddles two segments (for instance "rejettecasse"
				// in the Review view): the renderer splits it; nothing to
				// compare as a whole here.
				continue
			}
			if !bytes.Equal(src[s:e], []byte(l.Text)) {
				t.Fatalf("vue %s : %q ramené à la source donne %q", v, l.Text, src[s:e])
			}
			checked++
		}
		if checked < 20 {
			t.Fatalf("vue %s : seulement %d mots contrôlés", v, checked)
		}
	}
}
