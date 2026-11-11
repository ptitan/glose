package navette

import (
	"errors"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
)

// D-57 : une modification directe donne exactement le texte réécrit ; les
// annotations voisines restent telles quelles ; chaque modification est
// copiée avec sa ligne.
func TestImpose(t *testing.T) {
	src := "Un.\n\nLe chat {>>lequel ?<<}{>>glose:v1 id=aaaa by=author<<} dort.\n\nLa souris danse.\n"
	at := strings.Index(src, "La souris")
	e, _, err := review.Propose([]byte(src), critic.Span{Start: at, End: at + len("La souris danse.")}, "Le rat\ndort.", "", review.Author, review.NewGen(map[string]bool{"aaaa": true}))
	if err != nil {
		t.Fatal(err)
	}
	out, copies, err := Impose([]byte(src), e.Apply([]byte(src)))
	want := strings.Replace(src, "La souris danse.", "Le rat\ndort.", 1)
	if err != nil || string(out) != want {
		t.Fatalf("%v\n%q\n%q", err, out, want)
	}
	if len(copies) == 0 {
		t.Fatal("aucune copie")
	}
	for _, c := range copies {
		if c.Decision != Direct || c.By != review.Author || c.ID == "" || c.Line < 5 {
			t.Fatalf("copie : %+v", c)
		}
	}
	// Nothing to impose.
	if _, _, err := Impose([]byte(src), []byte(src)); !errors.Is(err, ErrDirectEmpty) {
		t.Fatalf("rien : %v", err)
	}
	// A proposal with a message asks the agent: refused.
	e, _, _ = review.Propose([]byte(src), critic.Span{Start: at, End: at + 2}, "Une", "Pourquoi ?", review.Author, review.NewGen(map[string]bool{"aaaa": true}))
	if _, _, err := Impose([]byte(src), e.Apply([]byte(src))); !errors.Is(err, ErrDirectMarks) {
		t.Fatalf("message : %v", err)
	}
	// An existing annotation changed on the way: refused.
	e, _, _ = review.Propose([]byte(src), critic.Span{Start: at, End: at + 2}, "Une", "", review.Author, review.NewGen(map[string]bool{"aaaa": true}))
	changed := strings.Replace(string(e.Apply([]byte(src))), "lequel ?", "lequel", 1)
	if _, _, err := Impose([]byte(src), []byte(changed)); !errors.Is(err, ErrDirectMarks) {
		t.Fatalf("annotation changée : %v", err)
	}
}

// Fuzzing : quelle que soit la réécriture d'un passage de texte simple, le
// résultat est celui de la suggestion entièrement acceptée (vue Proposé),
// sans aucune marque, et les octets hors du passage ne changent pas.
func FuzzImpose(f *testing.F) {
	f.Add("Le chat dort sur le tapis.\r\nFin.", 3, 12, "chien ronfle", false)
	f.Add("Un deux trois.", 0, 14, "Trois deux un.", true)
	f.Add("a b c d e f", 2, 9, "", false)
	f.Fuzz(func(t *testing.T, src string, a, b int, text string, whole bool) {
		clean := func(s string) bool {
			return !strings.ContainsAny(s, "{}~^[]<>=+-\\`*_") && !strings.Contains(s, "\x00")
		}
		if !clean(src) || !clean(text) || len(src) > 300 || len(text) > 300 {
			t.Skip()
		}
		if a < 0 || b < a || b > len(src) {
			t.Skip()
		}
		if !validCut(src, a) || !validCut(src, b) || src[a:b] == text {
			t.Skip()
		}
		// The proposals, as the server computes them (they may refuse).
		propose := review.Propose
		if whole {
			propose = review.ProposeWhole
		}
		e, _, err := propose([]byte(src), critic.Span{Start: a, End: b}, text, "", review.Author, review.NewGen(nil))
		if err != nil {
			t.Skip()
		}
		out, _, err := Impose([]byte(src), e.Apply([]byte(src)))
		if err != nil {
			t.Fatalf("refus : %v", err)
		}
		got := string(out)
		if want := string(projection.Project(e.Apply([]byte(src)), projection.Proposed).Text); got != want {
			t.Fatalf("\n%q\n%q", got, want)
		}
		if !strings.HasPrefix(got, src[:a]) || !strings.HasSuffix(got, src[b:]) {
			t.Fatalf("hors du passage : %q", got)
		}
		if !strings.ContainsAny(src+text, "\r") && got != src[:a]+text+src[b:] {
			t.Fatalf("texte : %q", got)
		}
		if marks, _ := critic.Parse([]byte(got)); len(marks) != 0 {
			t.Fatalf("marques : %q", got)
		}
	})
}

// validCut reports whether i is not inside a UTF-8 sequence.
func validCut(s string, i int) bool {
	return i == len(s) || i == 0 || s[i]&0xC0 != 0x80
}
