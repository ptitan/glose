package edit

import (
	"bytes"
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/projection"
)

func propose(t *testing.T, src, block, text string) string {
	t.Helper()
	i := bytes.Index([]byte(src), []byte(block))
	e, err := Propose([]byte(src), critic.Span{Start: i, End: i + len(block)}, text)
	if err != nil {
		t.Fatalf("%q → %q : %v", block, text, err)
	}
	return string(e.Apply([]byte(src)))
}

func TestPropose(t *testing.T) {
	cases := []struct{ src, block, text, want string }{
		{"Avant.\n\nLe chat dort.\n", "Le chat dort.", "Le chien dort.", "Avant.\n\nLe {~~chat~>chien~~} dort.\n"},
		{"Le chat dort.", "Le chat dort.", "Le chat dort bien.", "Le chat dort{++ bien++}."},
		{"Le chat dort.", "Le chat dort.", "Le gros chat dort.", "Le {++gros ++}chat dort."},
		{"Le gros chat dort.", "Le gros chat dort.", "Le chat dort.", "Le {--gros --}chat dort."},
		{"Un **mot** ici.", "Un **mot** ici.", "Un **terme** ici.", "Un **{~~mot~>terme~~}** ici."},
		{"L’été\r\nfut chaud.", "L’été\r\nfut chaud.", "L’été\nfut très chaud.", "L’été\r\nfut {++très ++}chaud."},
	}
	for _, c := range cases {
		if got := propose(t, c.src, c.block, c.text); got != c.want {
			t.Errorf("%q → %q :\n%q\nattendu\n%q", c.block, c.text, got, c.want)
		}
	}
}

func TestProposeRefusals(t *testing.T) {
	src := []byte("Un {>>note<<} texte.\n\nAutre.")
	if _, err := Propose(src, critic.Span{Start: 0, End: 19}, "Un texte."); !errors.Is(err, ErrMarked) {
		t.Errorf("bloc annoté : %v", err)
	}
	other := []byte("Autre texte.")
	if _, err := Propose(other, critic.Span{Start: 0, End: len(other)}, "Autre texte."); !errors.Is(err, ErrUnchanged) {
		t.Errorf("sans changement : %v", err)
	}
	if _, err := Propose(other, critic.Span{Start: 0, End: len(other)}, "Autre {++ texte."); !errors.Is(err, patch.ErrDelimiter) {
		t.Errorf("délimiteur : %v", err)
	}
	// A comment right after the block would join the new proposal's thread.
	glued := []byte("Mot{>>note<<}")
	if _, err := Propose(glued, critic.Span{Start: 0, End: 3}, "Terme"); err == nil {
		t.Error("une proposition collée à un commentaire doit être refusée")
	}
}

// FuzzPropose: whatever the texts, a proposal either is refused, or rejecting
// it gives the original file and accepting it gives the rewritten one.
func FuzzPropose(f *testing.F) {
	f.Add([]byte("Le chat dort.\n\nFin."), uint8(0), uint8(13), "Le chien dort bien.")
	f.Add([]byte("a b c d"), uint8(2), uint8(5), "x b y")
	f.Fuzz(func(t *testing.T, src []byte, s, e uint8, text string) {
		if !utf8Valid(src) {
			return
		}
		a, b := int(s)%(len(src)+1), int(e)%(len(src)+1)
		if a > b {
			a, b = b, a
		}
		ed, err := Propose(src, critic.Span{Start: a, End: b}, text)
		if err != nil {
			return
		}
		out := ed.Apply(src)
		if !bytes.Equal(out[:a], src[:a]) || !bytes.Equal(out[len(out)-(len(src)-b):], src[b:]) {
			t.Fatalf("octets modifiés hors du bloc")
		}
		if !bytes.Equal(projection.Project(out[a:a+len(ed.Insert)], projection.Current).Text, src[a:b]) {
			t.Fatalf("rejeter ne rend pas l'original")
		}
	})
}

func utf8Valid(b []byte) bool { return bytes.Equal([]byte(string([]rune(string(b)))), b) }

func TestProposeWithNote(t *testing.T) {
	src := []byte("Le chat dort sur le tapis.")
	e, err := ProposeWithNote(src, critic.Span{Start: 0, End: len(src)}, "Le chien dort sur le tapis.", "Qu'en penses-tu ?")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(e.Apply(src)); got != "Le {~~chat~>chien~~}{>>Qu'en penses-tu ?<<} dort sur le tapis." {
		t.Fatalf("%q", got)
	}
	if _, err := ProposeWithNote(src, critic.Span{Start: 0, End: len(src)}, "Le chien dort.", "voir {>> ici"); err == nil {
		t.Fatal("une note avec un délimiteur doit être refusée")
	}
}

func TestProposeReplacesCodeAsAWhole(t *testing.T) {
	src := "Lancer `glose open` puis lire."
	got := propose(t, src, src, "Lancer `glose open --no-browser` puis lire.")
	want := "Lancer {~~`glose open`~>`glose open --no-browser`~~} puis lire."
	if got != want {
		t.Fatalf("obtenu %q\nattendu %q", got, want)
	}
}

// Essai réel du lot P2 : deux mots voisins changés forment une seule
// proposition ; un mot inchangé entre les deux les sépare.
func TestProposeMergesChangesAcrossSpaces(t *testing.T) {
	src := "Le chat noir dort.\n"
	if got := propose(t, src, "Le chat noir dort.", "Un chien noir dort."); got != "{~~Le chat~>Un chien~~} noir dort.\n" {
		t.Fatalf("%q", got)
	}
	if got := propose(t, src, "Le chat noir dort.", "Un chat blanc dort."); got != "{~~Le~>Un~~} chat {~~noir~>blanc~~} dort.\n" {
		t.Fatalf("%q", got)
	}
}

// Mettre un mot ou deux en gras : une seule proposition.
func TestProposeEmphasisIsOneChange(t *testing.T) {
	src := "Un outil utile ici.\n"
	if got := propose(t, src, "Un outil utile ici.", "Un **outil** utile ici."); got != "Un {~~outil~>**outil**~~} utile ici.\n" {
		t.Fatalf("%q", got)
	}
	if got := propose(t, src, "Un outil utile ici.", "Un **outil utile** ici."); got != "Un {~~outil utile~>**outil utile**~~} ici.\n" {
		t.Fatalf("%q", got)
	}
}

// P3.3: structures. A whole line added or deleted (list item, table row,
// paragraph) starts its line; a formula changes as a whole.
func TestProposeStructures(t *testing.T) {
	cases := []struct{ old, neu, want string }{
		{"- a\n- b\n- c", "- a\n- b\n- x\n- c", "- a\n- b\n{++- x\n++}- c"},
		{"- a\n- b\n- c", "- a\n- c", "- a\n{--- b\n--}- c"},
		{"| N | V |\n|---|---|\n| a | 1 |\n| c | 3 |", "| N | V |\n|---|---|\n| a | 1 |\n| b | 2 |\n| c | 3 |",
			"| N | V |\n|---|---|\n| a | 1 |\n{++| b | 2 |\n++}| c | 3 |"},
		{"| N | V |\n|---|---|\n| a | 1 |\n| b | 2 |\n| c | 3 |", "| N | V |\n|---|---|\n| a | 1 |\n| c | 3 |",
			"| N | V |\n|---|---|\n| a | 1 |\n{--| b | 2 |\n--}| c | 3 |"},
		{"Premier.\n\nSecond.", "Premier.\n\nMilieu.\n\nSecond.", "Premier.\n\n{++Milieu.\n\n++}Second."},
		{"> a\n> b", "> a\n> x\n> b", "> a\n{++> x\n++}> b"},
		{"Formule $e^{x}$ fin.", "Formule $e^{2x}$ fin.", "Formule {~~$e^{x}$~>$e^{2x}$~~} fin."},
		{"$$\nx^2\n$$ et $5 et $6.", "$$\nx^3\n$$ et $5 et $7.", "{~~$$\nx^2\n$$~>$$\nx^3\n$$~~} et $5 et ${~~6~>7~~}."},
		{"- a\r\n- c", "- a\r\n- b\r\n- c", "- a\r\n{++- b\r\n++}- c"},
		// A table written in one go after a paragraph is one proposal.
		{"Une liste :\n", "Une liste :\n\n| A | B |\n| --- | --- |\n|   |   |\n",
			"Une liste :\n{++\n| A | B |\n| --- | --- |\n|   |   |\n++}"},
		{"Un.\n\n| A | B |\n| --- | --- |\n| x | y |\n\nDeux.\n", "Un.\n\nDeux.\n",
			"Un.\n\n{--| A | B |\n| --- | --- |\n| x | y |\n\n--}Deux.\n"},
	}
	for _, c := range cases {
		if got := propose(t, c.old, c.old, c.neu); got != c.want {
			t.Errorf("%q → %q :\n%q\nattendu\n%q", c.old, c.neu, got, c.want)
		}
	}
}

// FuzzProposeStructures rewrites random texts made of the signs of structures
// (lines, table bars, list markers, dollars): the proposals are always found
// (the arrangements of mergeLines and slideLines keep both views), and
// rejecting them all gives back the old text, accepting them the new one.
func FuzzProposeStructures(f *testing.F) {
	f.Add([]byte("- a\n- b\n"), []byte("- a\n- x\n- b\n"))
	f.Add([]byte("| a |\n| b |"), []byte("| a |\n\n| c |\n| b |\n"))
	f.Add([]byte("x\n"), []byte("x\n\n| A |\n| - |\n|   |\n"))
	alphabet := []byte("ab -|\n$>")
	norm := func(b []byte) []byte {
		out := make([]byte, len(b))
		for i, c := range b {
			out[i] = alphabet[int(c)%len(alphabet)]
		}
		return out
	}
	f.Fuzz(func(t *testing.T, old, neu []byte) {
		if len(old) > 60 || len(neu) > 60 {
			return
		}
		old, neu = norm(old), norm(neu)
		e, err := Propose(old, critic.Span{Start: 0, End: len(old)}, string(neu))
		if errors.Is(err, ErrUnchanged) {
			return
		}
		if err != nil {
			t.Fatalf("%q → %q : %v", old, neu, err)
		}
		out := e.Apply(old)
		if !bytes.Equal(projection.Project(out, projection.Current).Text, old) ||
			!bytes.Equal(projection.Project(out, projection.Proposed).Text, neu) {
			t.Fatalf("%q → %q : vues fausses %q", old, neu, out)
		}
	})
}

// P3.4: a proposal never cuts a character as the reader sees it.
func TestProposeGraphemes(t *testing.T) {
	cases := []struct{ old, neu, want string }{
		{"Bravo \U0001F44D\U0001F3FB !", "Bravo \U0001F44D\U0001F3FF !", "Bravo {~~\U0001F44D\U0001F3FB~>\U0001F44D\U0001F3FF~~} !"},
		{"Dev \U0001F469‍\U0001F4BB.", "Dev \U0001F469‍\U0001F52C.", "Dev {~~\U0001F469‍\U0001F4BB~>\U0001F469‍\U0001F52C~~}."},
		{"Drapeau \U0001F1EB\U0001F1F7.", "Drapeau \U0001F1E9\U0001F1EA.", "Drapeau {~~\U0001F1EB\U0001F1F7~>\U0001F1E9\U0001F1EA~~}."},
		{"Un ❤️ ici.", "Un ❤ ici.", "Un {~~❤️~>❤~~} ici."},
		{"Le café noir.", "Le cafe noir.", "Le {~~café~>cafe~~} noir."},
	}
	for _, c := range cases {
		if got := propose(t, c.old, c.old, c.neu); got != c.want {
			t.Errorf("%+q → %+q :\n%+q\nattendu\n%+q", c.old, c.neu, got, c.want)
		}
	}
}

// D-32 : une réécriture d'un tenant est une seule proposition, du premier
// mot changé au dernier ; lignes entières si elle en couvre plusieurs.
func TestProposeWhole(t *testing.T) {
	cases := []struct{ src, block, text, want string }{
		{"Avant.\n\nLe chat dort sur le tapis.\n", "Le chat dort sur le tapis.", "Le chien somnole sur le canapé.",
			"Avant.\n\nLe {~~chat dort sur le tapis~>chien somnole sur le canapé~~}.\n"},
		{"Le chat dort.", "Le chat dort.", "Le chat dort bien.", "Le chat dort{++ bien++}."},
		{"Le gros chat dort.", "Le gros chat dort.", "Le chat dort.", "Le {--gros --}chat dort."},
		{"Un a~>b ici.", "Un a~>b ici.", "Un c ici.", "Un {--a~>b--}{++c++} ici."},
		{"| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n", "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n", "| a | b |\n|---|---|\n| 1 | 9 |\n| 8 | 4 |\n",
			"| a | b |\n|---|---|\n{~~| 1 | 2 |\n| 3 | 4 |\n~>| 1 | 9 |\n| 8 | 4 |\n~~}"},
		{"Un.\r\nDeux.\r\n", "Un.\r\nDeux.\r\n", "Un.\nTrois.\nQuatre.\n", "Un.\r\n{~~Deux.\r\n~>Trois.\r\nQuatre.\r\n~~}"},
	}
	for _, c := range cases {
		i := bytes.Index([]byte(c.src), []byte(c.block))
		e, err := ProposeWhole([]byte(c.src), critic.Span{Start: i, End: i + len(c.block)}, c.text)
		if err != nil {
			t.Errorf("%q → %q : %v", c.block, c.text, err)
			continue
		}
		got := e.Apply([]byte(c.src))
		if string(got) != c.want {
			t.Errorf("%q → %q :\n%q\nattendu\n%q", c.block, c.text, got, c.want)
		}
		marks, _ := critic.Parse(got)
		props := 0
		for _, m := range marks {
			if m.Kind != critic.Comment {
				props++
			}
		}
		if props != 1 && !bytes.Contains([]byte(c.block), []byte("~>")) {
			t.Errorf("%q : %d propositions", got, props)
		}
	}
	// Garde-fous : délimiteurs, annotation dans la plage.
	if _, err := ProposeWhole([]byte("Un mot."), critic.Span{Start: 0, End: 7}, "Un {++mot."); err == nil {
		t.Error("délimiteur accepté")
	}
	src := []byte("Un {>>x<<} mot.")
	if _, err := ProposeWhole(src, critic.Span{Start: 0, End: len(src)}, "Deux mots."); !errors.Is(err, ErrMarked) {
		t.Errorf("annotation dans la plage : %v", err)
	}
}

// Pour tout texte sans marque, la réécriture d'un tenant passe sa propre
// vérification (Actuel = ancien, Proposé = nouveau, une seule proposition
// sauf « ~> » dans l'ancien texte).
func FuzzProposeWhole(f *testing.F) {
	f.Add("Le chat dort.", "Le chien dort bien.")
	f.Add("| a |\n| 1 |\n", "| a |\n| 2 |\n| 3 |\n")
	f.Add("Un.\r\nDeux.\r\n", "Un.\nTrois.\n")
	f.Add("a~>b c", "d")
	f.Fuzz(func(t *testing.T, old, neu string) {
		if !utf8.ValidString(old) || !utf8.ValidString(neu) || critic.ContainsDelimiter([]byte(old)) {
			return
		}
		if m, _ := critic.Parse([]byte(old)); len(m) > 0 {
			return
		}
		e, err := ProposeWhole([]byte(old), critic.Span{Start: 0, End: len(old)}, neu)
		if errors.Is(err, ErrUnchanged) || errors.Is(err, patch.ErrDelimiter) {
			return
		}
		if errors.Is(err, ErrCheck) {
			// A safe refusal (a mark that would fall in code, D-24): allowed
			// only if the proposals word by word are refused too.
			if _, err2 := Propose([]byte(old), critic.Span{Start: 0, End: len(old)}, neu); err2 == nil {
				t.Fatalf("%q → %q : refusé d'un tenant, accepté mot à mot", old, neu)
			}
			return
		}
		if err != nil {
			t.Fatalf("%q → %q : %v", old, neu, err)
		}
		out := e.Apply([]byte(old))
		if !bytes.Equal(projection.Project(out, projection.Current).Text, []byte(old)) {
			t.Fatalf("Actuel : %q", out)
		}
	})
}
