package sourcepos

import (
	"math/rand"
	"strings"
	"testing"
)

// P6.11 : reprendre au dernier point atteint donne exactement ce que donne
// un calcul repris du début de la ligne, quel que soit l'ordre des
// positions demandées (textes tirés au hasard : accents composés et
// décomposés, tabulations, \r, emoji, caractères inconnus des tables).
func TestOffsetResumeMatchesAFreshComputation(t *testing.T) {
	pieces := []string{"a", "é", "é", "\t", "\r", "😀", "͸", " ", "x", "가", "q́", "क़", "漢"}
	r := rand.New(rand.NewSource(7))
	for n := 0; n < 3000; n++ {
		var b strings.Builder
		for l := 0; l < 1+r.Intn(3); l++ {
			for k := 0; k < r.Intn(40); k++ {
				b.WriteString(pieces[r.Intn(len(pieces))])
			}
			b.WriteString("\n")
		}
		src := []byte(b.String())
		shared, _ := NewIndex(src)
		for q := 0; q < 60; q++ {
			p := Pos{Line: 1 + r.Intn(3), Col: 1 + r.Intn(45)}
			fresh, _ := NewIndex(src)
			o1, e1, err1 := fresh.Offset(p)
			o2, e2, err2 := shared.Offset(p)
			if o1 != o2 || e1 != e2 || (err1 == nil) != (err2 == nil) {
				t.Fatalf("%q, %v : neuf %d %v %v, repris %d %v %v", src, p, o1, e1, err1, o2, e2, err2)
			}
		}
	}
}
