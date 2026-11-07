package grapheme

import (
	"testing"
	"unicode/utf8"
)

// clusters cuts s at every boundary.
func clusters(s string) []string {
	b := []byte(s)
	var out []string
	start := 0
	for i := 1; i <= len(b); i++ {
		if Boundary(b, i) {
			out = append(out, string(b[start:i]))
			start = i
		}
	}
	return out
}

func TestClusters(t *testing.T) {
	cases := []struct {
		s    string
		want []string
	}{
		{"abc", []string{"a", "b", "c"}},
		{"café!", []string{"c", "a", "f", "é", "!"}},                                                         // combining accent
		{"❤️ x", []string{"❤️", " ", "x"}},                                                                     // variation selector
		{"\U0001F44D\U0001F3FD!", []string{"\U0001F44D\U0001F3FD", "!"}},                                       // skin tone
		{"\U0001F469‍\U0001F4BB.", []string{"\U0001F469‍\U0001F4BB", "."}},                                     // ZWJ sequence
		{"\U0001F1EB\U0001F1F7\U0001F1E9\U0001F1EA", []string{"\U0001F1EB\U0001F1F7", "\U0001F1E9\U0001F1EA"}}, // two flags
		{"\U0001F1EB\U0001F1F7\U0001F1E9", []string{"\U0001F1EB\U0001F1F7", "\U0001F1E9"}},
		{"a\r\nb", []string{"a", "\r\n", "b"}},
		{"각 각", []string{"각", " ", "각"}}, // Hangul jamo
		{"कि", []string{"कि"}},                 // devanagari vowel sign (Mc)
		{"\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", []string{"\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"}},
	}
	for _, c := range cases {
		got := clusters(c.s)
		if len(got) != len(c.want) {
			t.Errorf("%+q : %+q, attendu %+q", c.s, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%+q : %+q, attendu %+q", c.s, got, c.want)
				break
			}
		}
	}
}

// FuzzBoundary: a boundary is always the start of a character, and the
// clusters cover the text exactly.
func FuzzBoundary(f *testing.F) {
	f.Add([]byte("é\U0001F469‍\U0001F4BB\r\n\U0001F1EB\U0001F1F7"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if !utf8.Valid(b) {
			return
		}
		for i := 0; i <= len(b); i++ {
			if Boundary(b, i) && i < len(b) && !utf8.RuneStart(b[i]) {
				t.Fatalf("frontière au milieu d'un caractère : %q, %d", b, i)
			}
		}
		if !Boundary(b, 0) || !Boundary(b, len(b)) {
			t.Fatal("début ou fin refusés")
		}
	})
}
