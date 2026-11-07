package sourcepos

import (
	"errors"
	"testing"
)

// The expected offsets reproduce what pandoc 3.1.3 reports for the same
// texts; these tests do not need pandoc.
func TestOffset(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		pos    Pos
		offset int
		exact  bool
	}{
		{"début", "abc", Pos{1, 1}, 0, true},
		{"accent précomposé (2 octets, 1 colonne)", "é z", Pos{1, 3}, 3, true},
		{"emoji (4 octets, 1 colonne)", "😀 z", Pos{1, 3}, 5, true},
		{"idéogrammes", "漢字 z", Pos{1, 4}, 7, true},
		{"tabulation en colonne 2 : suite en colonne 5", "a\tb", Pos{1, 5}, 2, true},
		{"tabulation en colonne 3 : suite en colonne 5", "ab\tc", Pos{1, 5}, 3, true},
		{"position à l'intérieur d'une tabulation", "a\tb", Pos{1, 3}, 1, false},
		{"citation suivie d'une tabulation", ">\tab", Pos{1, 5}, 2, true},
		{"marque d'ordre des octets ignorée", "\ufeffab z", Pos{1, 1}, 3, true},
		{"marque d'ordre des octets, plus loin", "\ufeffab z", Pos{1, 4}, 6, true},
		{"ligne 2 après LF", "ab\ncd", Pos{2, 2}, 4, true},
		{"ligne 2 après CRLF", "ab\r\ncd z", Pos{2, 4}, 7, true},
		{"\\r isolé : ni ligne ni colonne", "ab\rcd z", Pos{1, 6}, 6, true},
		{"début de ligne vide finale", "ab\n", Pos{2, 1}, 3, true},
		// D-29: pandoc composes (NFC); the columns follow its NFC form.
		{"accent combinant : exact ensuite", "e\u0301 z", Pos{1, 3}, 4, true},
		{"accent combinant : exact avant", "a e\u0301", Pos{1, 3}, 2, true},
		{"jamo hangeul : exact ensuite", "\u1100\u1161 z", Pos{1, 3}, 7, true},
		{"U+0958 dédoublé par NFC : exact ensuite", "\u0958\u0958 x x", Pos{1, 6}, 7, true},
		{"U+0958 : inexact à l'intérieur", "\u0958 x", Pos{1, 2}, 0, false},
		{"accent combinant non composable : une colonne", "q\u0301 z", Pos{1, 4}, 4, true},
		{"caractère non attribué : inexact ensuite", "\u0378 z", Pos{1, 3}, 3, false},
		{"emoji avec liant : exact", "\U0001F469\u200d\U0001F4BB z", Pos{1, 5}, 12, true},
	}
	for _, c := range cases {
		x, err := NewIndex([]byte(c.src))
		if err != nil {
			t.Fatalf("%s : %v", c.name, err)
		}
		off, exact, err := x.Offset(c.pos)
		if err != nil || off != c.offset || exact != c.exact {
			t.Errorf("%s : Offset(%v) = %d, %v, %v ; attendu %d, %v", c.name, c.pos, off, exact, err, c.offset, c.exact)
		}
	}
}

func TestOffsetOutsideTheText(t *testing.T) {
	x, _ := NewIndex([]byte("ab\ncd"))
	for _, p := range []Pos{{3, 1}, {1, 4}, {2, 4}} {
		if _, _, err := x.Offset(p); err == nil {
			t.Errorf("Offset(%v) : erreur attendue", p)
		}
	}
}

func TestSpanOverAComposedPiece(t *testing.T) {
	// pandoc renders "가" at 1:1-1:2; its source is two code points (jamo):
	// the range is exact, the word will be « transformed » (D-29).
	x, _ := NewIndex([]byte("\u1100\u1161 z"))
	s, e, exact, err := x.Span(Range{Pos{1, 1}, Pos{1, 2}})
	if err != nil || !exact || s != 0 || e != 6 {
		t.Fatalf("Span = [%d, %d), exact = %v, err = %v ; [0, 6) exact attendu", s, e, exact, err)
	}
}

func TestOffsetRejectsInvalidPositions(t *testing.T) {
	x, _ := NewIndex([]byte("ab"))
	for _, p := range []Pos{{0, 1}, {1, 0}, {-1, 1}} {
		if _, _, err := x.Offset(p); err == nil {
			t.Errorf("Offset(%v) : erreur attendue", p)
		}
	}
}

func TestNewIndexRejectsInvalidUTF8(t *testing.T) {
	if _, err := NewIndex([]byte("a\xffb")); !errors.Is(err, ErrNotUTF8) {
		t.Fatalf("erreur = %v ; ErrNotUTF8 attendu", err)
	}
}
