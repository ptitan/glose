package sourcepos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// expectedOdd lists, per corpus file, the words and spaces that must NOT be
// certified, with their status. Every other word or space must be certified.
// Keys are "line:col rendered-text".
var expectedOdd = map[string]map[string]Status{
	"plain.md": {},
	"livre.md": {},
	"crlf.md":  {},
	"bom.md":   {},
	"typo.md": {
		"1:2 ’": Transformed, "1:56 –": Transformed, "1:62 —": Transformed, "1:69 …": Transformed,
	},
	"difficile.md": {
		// entities and escapes: exact range, different content
		"1:10 &": Transformed, "1:26 \u00a0": Transformed,
		"3:9 *": Transformed, "3:22 _": Transformed,
		// decomposed accent and Hangul jamo: pandoc composes them (NFC); the
		// range is exact, the content differs; the rest of the line is
		// certified (D-29, nfc.go)
		"5:11 été": Transformed, "7:9 가": Transformed,
		// line 9 (variation selector, sequence joined by U+200D): certified
		// since the selectors are known stable under NFC (P3.4)
		// lone '\r' removed by pandoc
		"11:8 abcd": Transformed,
		// U+0958 is split in two by NFC: the two code points of pandoc have no
		// exact place inside it; the first "x" is certified on its own bytes,
		// not on those of the second one (review of 2026-11-06)
		"17:12 \u0915": Approximate, "17:13 \u093c": Approximate,
		"17:14 \u0915": Approximate, "17:15 \u093c": Approximate,
	},
}

func TestCorpusAgainstPandoc(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent : test sauté")
	}
	for file, odd := range expectedOdd {
		t.Run(file, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata", "corpus", file))
			if err != nil {
				t.Fatal(err)
			}
			x, err := NewIndex(src)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := Run(context.Background(), "pandoc", src)
			if err != nil {
				t.Fatal(err)
			}
			nodes, err := Nodes(tree)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			words := 0
			for _, l := range Locate(x, nodes) {
				if l.Kind != KindStr && l.Kind != KindSpace {
					continue
				}
				words++
				if l.Status == Certified && string(src[l.Start:l.End]) != l.Text && l.Kind == KindStr {
					t.Errorf("certifié à tort : %v %q", l.Ranges, l.Text)
				}
				if l.Status == Certified {
					continue
				}
				key := keyOf(l)
				want, listed := odd[key]
				switch {
				case !listed:
					t.Errorf("%s : %s inattendu", key, l.Status)
				case want != l.Status:
					t.Errorf("%s : %s, attendu %s", key, l.Status, want)
				}
				seen[key] = true
			}
			for key := range odd {
				if !seen[key] {
					t.Errorf("%s : attendu non certifié, mais certifié ou absent", key)
				}
			}
			if words == 0 {
				t.Fatal("aucun mot positionné : pandoc n'a pas produit de positions")
			}
		})
	}
}

func keyOf(l Located) string {
	return strconv.Itoa(l.Ranges[0].Start.Line) + ":" + strconv.Itoa(l.Ranges[0].Start.Col) + " " + l.Text
}
