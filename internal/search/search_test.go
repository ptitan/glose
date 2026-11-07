package search

import (
	"bytes"
	"testing"

	"github.com/ptitan/glose/internal/projection"
)

func TestProjectedSearchPreservesSourcePositions(t *testing.T) {
	src := []byte("\xef\xbb\xbfCafé é 👩🏽‍💻.\r\n{~~ancien~>NOUVEAU~~}{>>secret<<}\r\nȺ puis Ⱥ.\n")
	before := bytes.Clone(src)
	for _, tc := range []struct {
		q, text string
		view    projection.View
		n       int
	}{
		{"café", "Café", projection.Current, 1},
		{"ancien", "ancien", projection.Current, 1},
		{"nouveau", "NOUVEAU", projection.Proposed, 1},
		{"nouveau", "", projection.Current, 0},
		{"secret", "", projection.Current, 0},
		{"ⱥ", "Ⱥ", projection.Current, 2},
		{"👩🏽‍💻", "👩🏽‍💻", projection.Current, 1},
	} {
		m, n := Find(src, tc.q, tc.view, false, 10)
		if n != tc.n || len(m) != n {
			t.Fatalf("%q : %v %d", tc.q, m, n)
		}
		for _, x := range m {
			if string(src[x.Start:x.End]) != tc.text {
				t.Fatalf("position : %+v", x)
			}
		}
	}
	if !bytes.Equal(src, before) {
		t.Fatal("source modifiée")
	}
	if m, n := Find(src, "Ⱥ", 0, true, 1); len(m) != 1 || n != 2 {
		t.Fatalf("limite : %v %d", m, n)
	}
	if _, n := Find(src, "café", 0, true, 10); n != 0 {
		t.Fatal("casse ignorée")
	}
}
