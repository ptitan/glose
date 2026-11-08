package patch

import (
	"bytes"
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/critic"
)

// faithful checks the fidelity invariant independently of Edit.Apply: the
// bytes before e.Start and after e.End are identical in src and out.
func faithful(t *testing.T, src, out []byte, e Edit) {
	t.Helper()
	tail := len(src) - e.End
	if !bytes.Equal(out[:e.Start], src[:e.Start]) || !bytes.Equal(out[len(out)-tail:], src[e.End:]) {
		t.Fatalf("octets modifiés hors de la plage [%d, %d)\navant : %q\naprès : %q", e.Start, e.End, src, out)
	}
}

// result carries the two values returned by an operation.
type result struct {
	e   Edit
	err error
}

func res(e Edit, err error) result { return result{e, err} }
func ok(e Edit) result             { return result{e, nil} }

func apply(t *testing.T, src []byte, r result) []byte {
	t.Helper()
	if r.err != nil {
		t.Fatalf("erreur inattendue : %v", r.err)
	}
	out := r.e.Apply(src)
	faithful(t, src, out, r.e)
	return out
}

// only returns the single annotation of src.
func only(t *testing.T, src []byte) (critic.Annotation, []critic.Mark) {
	t.Helper()
	marks, issues := critic.Parse(src)
	groups := critic.Group(marks)
	if len(issues) != 0 || len(groups) != 1 {
		t.Fatalf("une annotation attendue dans %q : %d, problèmes %v", src, len(groups), issues)
	}
	return groups[0], marks
}

// The fixture mixes accents, an emoji, CRLF line endings and a BOM.
var fixture = []byte("\ufeffL’été, la cour 😀 rejette.\r\nSeconde ligne\r\n")

func span(src []byte, sub string) critic.Span {
	i := bytes.Index(src, []byte(sub))
	return critic.Span{Start: i, End: i + len(sub)}
}

func TestCommentAPassageThenResolve(t *testing.T) {
	s := span(fixture, "la cour")
	out := apply(t, fixture, res(Comment(fixture, s, "Quelle cour ?")))
	if !bytes.Contains(out, []byte("{==la cour==}{>>Quelle cour ?<<}")) {
		t.Fatalf("résultat : %q", out)
	}
	a, marks := only(t, out)
	back := apply(t, out, res(Resolve(out, marks, a)))
	if !bytes.Equal(back, fixture) {
		t.Fatalf("résoudre ne rend pas l'original : %q", back)
	}
}

func TestCommentAtAPosition(t *testing.T) {
	at := span(fixture, "rejette").End
	out := apply(t, fixture, res(Comment(fixture, critic.Span{Start: at, End: at}, "Vérifier.")))
	if !bytes.Contains(out, []byte("rejette{>>Vérifier.<<}.")) {
		t.Fatalf("résultat : %q", out)
	}
	a, marks := only(t, out)
	if back := apply(t, out, res(Withdraw(out, marks, a))); !bytes.Equal(back, fixture) {
		t.Fatalf("retirer ne rend pas l'original : %q", back)
	}
}

func TestProposalsAcceptAndReject(t *testing.T) {
	cases := []struct {
		name     string
		edit     func() (Edit, error)
		accepted string
	}{
		{"ajout", func() (Edit, error) { return ProposeAddition(fixture, span(fixture, " rejette").Start, " ne") },
			"\ufeffL’été, la cour 😀 ne rejette.\r\nSeconde ligne\r\n"},
		{"suppression", func() (Edit, error) { return ProposeDeletion(fixture, span(fixture, " 😀")) },
			"\ufeffL’été, la cour rejette.\r\nSeconde ligne\r\n"},
		{"remplacement", func() (Edit, error) { return ProposeSubstitution(fixture, span(fixture, "rejette"), "casse") },
			"\ufeffL’été, la cour 😀 casse.\r\nSeconde ligne\r\n"},
		{"remplacement sur deux lignes", func() (Edit, error) {
			return ProposeSubstitution(fixture, span(fixture, "rejette.\r\nSeconde"), "casse.\r\nDeuxième")
		}, "\ufeffL’été, la cour 😀 casse.\r\nDeuxième ligne\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := c.edit()
			proposed := apply(t, fixture, res(e, err))
			a, marks := only(t, proposed)
			if got := apply(t, proposed, res(Accept(proposed, marks, a))); string(got) != c.accepted {
				t.Errorf("accepté : %q\nattendu : %q", got, c.accepted)
			}
			if got := apply(t, proposed, res(Reject(proposed, marks, a))); !bytes.Equal(got, fixture) {
				t.Errorf("rejeté : %q, attendu l'original", got)
			}
		})
	}
}

func TestDecisionRemovesTheWholeThread(t *testing.T) {
	src := []byte("Avant {~~rejette~>casse~~}{>>pourquoi ?<<}{>>parce que.<<} après.")
	a, marks := only(t, src)
	if len(a.Comments) != 2 {
		t.Fatalf("fil attendu de 2 commentaires : %+v", a)
	}
	if got := apply(t, src, res(Accept(src, marks, a))); string(got) != "Avant casse après." {
		t.Fatalf("accepté : %q", got)
	}
}

func TestReplyExtendsTheThread(t *testing.T) {
	src := []byte("Texte {==passage==}{>>question<<} suite.")
	a, am := only(t, src)
	out := apply(t, src, res(Reply(src, am, a, "réponse")))
	b, marks := only(t, out)
	if len(b.Comments) != 2 || string(out[marks[b.Comments[1]].Text.Start:marks[b.Comments[1]].Text.End]) != "réponse" {
		t.Fatalf("fil : %q", out)
	}
	e, err := RemoveComment(out, marks, b, b.Comments[1])
	if back := apply(t, out, res(e, err)); !bytes.Equal(back, src) {
		t.Fatalf("retrait de la réponse : %q", back)
	}
}

func TestDecisionsCheckTheKind(t *testing.T) {
	comment := []byte("a {>>note<<} b")
	a, marks := only(t, comment)
	if _, err := Accept(comment, marks, a); !errors.Is(err, ErrKind) {
		t.Errorf("accepter un commentaire : %v", err)
	}
	proposal := []byte("a {++b++} c")
	p, pm := only(t, proposal)
	if _, err := Resolve(proposal, pm, p); !errors.Is(err, ErrKind) {
		t.Errorf("résoudre une proposition : %v", err)
	}
}

func TestRefusals(t *testing.T) {
	marked := []byte("Un {++ajout++} puis {>>note<<} fin.")
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"au milieu d'un caractère", errOf(ProposeAddition(fixture, span(fixture, "é").Start+1, "x")), ErrRange},
		{"avant la marque d'ordre des octets", errOf(ProposeAddition(fixture, 0, "x")), ErrRange},
		{"entre \\r et \\n", errOf(ProposeAddition(fixture, span(fixture, "\r\n").Start+1, "x")), ErrRange},
		{"hors du texte", errOf(ProposeAddition(fixture, len(fixture)+1, "x")), ErrRange},
		{"plage inversée", errOf(ProposeDeletion(fixture, critic.Span{Start: 10, End: 5})), ErrRange},
		{"dans une marque", errOf(ProposeAddition(marked, span(marked, "jout").Start, "x")), ErrOverlap},
		{"à cheval sur une marque", errOf(ProposeDeletion(marked, span(marked, "Un {++aj"))), ErrOverlap},
		{"englobe une marque", errOf(ProposeDeletion(marked, span(marked, "{++ajout++} puis"))), ErrOverlap},
		{"collé devant un commentaire", errOf(ProposeAddition(marked, span(marked, "{>>").Start, "x")), ErrAdjacent},
		{"commentaire collé après une marque", errOf(Comment(marked, critic.Span{Start: span(marked, "++}").End, End: span(marked, "++}").End}, "x")), ErrAdjacent},
		{"délimiteur dans le texte", errOf(ProposeAddition(fixture, span(fixture, "rejette").Start, "a ++} b")), ErrDelimiter},
		{"délimiteur dans la note", errOf(Comment(fixture, span(fixture, "cour"), "voir {>> ici")), ErrDelimiter},
		{"flèche dans l'ancien texte", errOf(ProposeSubstitution([]byte("a ~> b"), critic.Span{Start: 0, End: 6}, "c")), ErrArrow},
		{"ajout vide", errOf(ProposeAddition(fixture, span(fixture, "rejette").Start, "")), ErrEmpty},
		{"suppression vide", errOf(ProposeDeletion(fixture, critic.Span{Start: span(fixture, "cour").Start, End: span(fixture, "cour").Start})), ErrEmpty},
		{"remplacement par rien", errOf(ProposeSubstitution(fixture, span(fixture, "cour"), "")), ErrEmpty},
		{"note vide", errOf(Comment(fixture, span(fixture, "cour"), "")), ErrEmpty},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s : erreur %v, attendu %v", c.name, c.err, c.want)
		}
	}
}

func errOf(_ Edit, err error) error { return err }

// FuzzRoundTrip proposes or comments at random places of random texts and
// checks that (1) only the target range changes, (2) the new mark is parsed
// back with the expected kind and content, and (3) withdrawing it restores the
// original text byte for byte.
func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("L’été, la cour 😀 rejette.\r\nSeconde\n"), uint16(3), uint16(9), uint8(0), "neuf")
	f.Add([]byte("\ufeffabc {++x++} def"), uint16(0), uint16(2), uint8(2), "y")
	f.Add([]byte("a\r\nb"), uint16(1), uint16(2), uint8(1), "z")
	f.Fuzz(func(t *testing.T, src []byte, i, j uint16, op uint8, text string) {
		if !utf8.Valid(src) {
			return
		}
		s := critic.Span{Start: int(i) % (len(src) + 1), End: int(j) % (len(src) + 1)}
		if s.Start > s.End {
			s.Start, s.End = s.End, s.Start
		}
		var e Edit
		var err error
		var kind critic.Kind
		switch op % 4 {
		case 0:
			e, err, kind = withKind(Comment(src, s, text))(critic.Comment)
			if s.Start != s.End {
				kind = critic.Highlight
			}
		case 1:
			e, err, kind = withKind(ProposeAddition(src, s.Start, text))(critic.Addition)
		case 2:
			e, err, kind = withKind(ProposeDeletion(src, s))(critic.Deletion)
		case 3:
			e, err, kind = withKind(ProposeSubstitution(src, s, text))(critic.Substitution)
		}
		if err != nil {
			return // refused: nothing to check
		}
		out := e.Apply(src)
		faithful(t, src, out, e)
		a, marks, err := Find(out, e.Start)
		if err != nil {
			t.Fatalf("nouvelle marque introuvable à l'octet %d dans %q", e.Start, out)
		}
		if marks[a.Main].Kind != kind || a.Span.End != e.Start+len(e.Insert) {
			t.Fatalf("marque %v sur %+v, attendu %v sur [%d, %d) dans %q",
				marks[a.Main].Kind, a.Span, kind, e.Start, e.Start+len(e.Insert), out)
		}
		before, _ := critic.Parse(src)
		after, _ := critic.Parse(out)
		if len(after) != len(before)+len(a.Comments)+1 {
			t.Fatalf("les autres marques ont changé : %d avant, %d après, dans %q", len(before), len(after), out)
		}
		w, err := Withdraw(out, marks, a)
		if err != nil {
			t.Fatalf("retrait refusé : %v dans %q", err, out)
		}
		if back := w.Apply(out); !bytes.Equal(back, src) {
			t.Fatalf("retirer ne rend pas l'original :\n%q\n%q", src, back)
		}
	})
}

func withKind(e Edit, err error) func(critic.Kind) (Edit, error, critic.Kind) {
	return func(k critic.Kind) (Edit, error, critic.Kind) { return e, err, k }
}

func TestMalformedNeighbourIsDetected(t *testing.T) {
	src := []byte("Début {++ jamais fermé, puis du texte.")
	at := bytes.Index(src, []byte("puis"))
	if _, err := ProposeAddition(src, at, "ajout"); !errors.Is(err, ErrNeighbour) {
		t.Fatalf("erreur %v, attendu ErrNeighbour", err)
	}
	// Before the dangling opener, nothing can capture the new mark.
	if _, err := ProposeAddition(src, 0, "ajout"); err != nil {
		t.Fatalf("ajout en tête refusé : %v", err)
	}
}

// FuzzDecisions applies every decision to every annotation of random texts:
// a decision is either refused, or changes nothing outside the annotation and
// leaves the other marks intact (checked by verify).
func FuzzDecisions(f *testing.F) {
	f.Add([]byte("a {++b++}{>>c<<} d {~~e~>f~~} g {==h==}{>>i<<}{>>j<<} {--k--} {>>l<<}"))
	f.Add([]byte("{++ {>>x<<} ++} {~~a~>b"))
	f.Fuzz(func(t *testing.T, src []byte) {
		marks, _ := critic.Parse(src)
		for _, a := range critic.Group(marks) {
			for _, op := range []func([]byte, []critic.Mark, critic.Annotation) (Edit, error){Accept, Reject, Resolve, Withdraw} {
				e, err := op(src, marks, a)
				if err != nil {
					continue
				}
				if e.Start != a.Span.Start || e.End != a.Span.End {
					t.Fatalf("la décision déborde de l'annotation : %+v pour %+v", e, a.Span)
				}
				faithful(t, src, e.Apply(src), e)
			}
		}
	})
}

// Regression tests for the defects found by the code review of 2026-11-06.

func TestDecisionCannotWakeAMalformedNeighbour(t *testing.T) {
	src := []byte("{+{--x--}+")
	a, marks, err := Find(src, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(src, marks, a); !errors.Is(err, ErrNeighbour) {
		t.Fatalf("erreur %v, attendu ErrNeighbour (un « {++ » non fermé apparaîtrait à l'octet 0)", err)
	}
}

func TestNestedAnnotationIsReadOnly(t *testing.T) {
	src := []byte("{++a++}{++{>>note<<}++}")
	a, marks, err := Find(src, 7)
	if err != nil {
		t.Fatal(err)
	}
	for name, op := range map[string]func([]byte, []critic.Mark, critic.Annotation) (Edit, error){
		"accepter": Accept, "rejeter": Reject, "retirer": Withdraw,
	} {
		if _, err := op(src, marks, a); !errors.Is(err, ErrNested) {
			t.Errorf("%s : erreur %v, attendu ErrNested", name, err)
		}
	}
}

func TestInvalidAnnotationsAreRefused(t *testing.T) {
	src := []byte("x {++a++}")
	marks, _ := critic.Parse(src)
	bad := []critic.Annotation{
		{Main: 5},
		{Main: -1},
		{Main: 0, Span: critic.Span{Start: 2, End: 99}},
		{Main: 0, Comments: []int{0}, Span: marks[0].Span},
	}
	for _, a := range bad {
		if _, err := Accept(src, marks, a); !errors.Is(err, ErrRange) {
			t.Errorf("Accept(%+v) : erreur %v, attendu ErrRange", a, err)
		}
		if _, err := Reply(src, marks, a, "r"); !errors.Is(err, ErrRange) {
			t.Errorf("Reply(%+v) : erreur %v, attendu ErrRange", a, err)
		}
	}
}

func TestRemoveACommentInTheMiddleOfAThread(t *testing.T) {
	src := []byte("{==p==}{>>q<<}{>>erreur<<}{>>r<<} fin")
	a, marks, err := Find(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := apply(t, src, res(RemoveComment(src, marks, a, a.Comments[1])))
	if string(out) != "{==p==}{>>q<<}{>>r<<} fin" {
		t.Fatalf("résultat %q", out)
	}
}

// A proposal corrected several times stays one mark, with its thread: only
// the main mark changes, its old text is kept (P3.2).
func TestReviseKeepsTheThread(t *testing.T) {
	cases := []struct {
		src, text, want string
	}{
		{"a {~~chat~>chien~~}{>>t<<}{>>r<<} b", "loup", "a {~~chat~>loup~~}{>>t<<}{>>r<<} b"},
		{"a {~~chat~>chien~~}{>>t<<} b", "", "a {--chat--}{>>t<<} b"},
		{"a {--chat--}{>>t<<} b", "chien", "a {~~chat~>chien~~}{>>t<<} b"},
		{"a {++ très++}{>>t<<} b", " bien", "a {++ bien++}{>>t<<} b"},
		{"\ufeffL’{~~été~>hiver~~}\r\n{++x++}", "automne 😀", "\ufeffL’{~~été~>automne 😀~~}\r\n{++x++}"},
	}
	for _, c := range cases {
		src := []byte(c.src)
		marks, _ := critic.Parse(src)
		a := critic.Group(marks)[0]
		out := apply(t, src, res(Revise(src, marks, a, c.text)))
		if string(out) != c.want {
			t.Errorf("Revise(%q, %q) = %q, attendu %q", c.src, c.text, out, c.want)
		}
	}
	// Three corrections in a row: one mark, same thread.
	src := []byte("x {~~a~>b~~}{>>glose:v1 id=abcdef by=author<<}{>>pourquoi ?<<} y")
	for _, text := range []string{"c", "d", "e"} {
		marks, _ := critic.Parse(src)
		var a critic.Annotation
		for _, g := range critic.Group(marks) {
			if marks[g.Main].Kind == critic.Substitution {
				a = g
			}
		}
		src = apply(t, src, res(Revise(src, marks, a, text)))
	}
	if string(src) != "x {~~a~>e~~}{>>glose:v1 id=abcdef by=author<<}{>>pourquoi ?<<} y" {
		t.Fatalf("après trois corrections : %q", src)
	}
}

func TestReviseRefusals(t *testing.T) {
	cases := []struct {
		src, text string
		want      error
	}{
		{"{++a++}", "", ErrEmpty},
		{"{~~a~>b~~}", "a", ErrSame},
		{"{~~a~>b~~}", "b", ErrSame},
		{"{--a--}", "", ErrSame},
		{"{--a ~> b--}", "c", ErrArrow},
		{"{~~a~>b~~}", "c {++d", ErrDelimiter},
		{"{==a==}{>>b<<}", "c", ErrKind},
		{"{>>a<<}", "c", ErrKind},
	}
	for _, c := range cases {
		src := []byte(c.src)
		marks, _ := critic.Parse(src)
		a := critic.Group(marks)[0]
		if _, err := Revise(src, marks, a, c.text); !errors.Is(err, c.want) {
			t.Errorf("Revise(%q, %q) : erreur %v, attendu %v", c.src, c.text, err, c.want)
		}
	}
}

// FuzzRevise corrects every proposal of random texts: a correction is either
// refused, or changes only the main mark, and the views stay consistent: the
// current view is unchanged.
func FuzzRevise(f *testing.F) {
	f.Add([]byte("a {++b++}{>>c<<} d {~~e~>f~~} g {--k--} {>>l<<}"), "nouveau")
	f.Add([]byte("{++ {>>x<<} ++} {~~a~>b"), "")
	f.Fuzz(func(t *testing.T, src []byte, text string) {
		marks, _ := critic.Parse(src)
		for _, a := range critic.Group(marks) {
			e, err := Revise(src, marks, a, text)
			if err != nil {
				continue
			}
			if e.Start != marks[a.Main].Span.Start || e.End != marks[a.Main].Span.End {
				t.Fatalf("la correction déborde de la marque : %+v", e)
			}
			out := e.Apply(src)
			faithful(t, src, out, e)
			after, _ := critic.Parse(out)
			if len(after) != len(marks) {
				t.Fatalf("nombre de marques changé : %q → %q", src, out)
			}
		}
	})
}

// P9.1 : un commentaire réécrit garde sa place, son fil et les identités ;
// rien d'autre ne bouge, octet pour octet.
func TestAmendKeepsTheThread(t *testing.T) {
	cases := []struct {
		src  string
		nth  int // which comment mark of the annotation, from 0 (the main mark if a comment)
		text string
		want string
	}{
		{"a {>>vieux<<} b", 0, "neuf", "a {>>neuf<<} b"},
		{"a {==passage==}{>>vieux<<}{>>glose:v1 id=abcdef by=author<<} b", 1, "neuf",
			"a {==passage==}{>>neuf<<}{>>glose:v1 id=abcdef by=author<<} b"},
		{"a {~~x~>y~~}{>>glose:v1 id=abcdef by=agent<<}{>>vieux<<}{>>glose:v1 id=ghijkm re=abcdef by=author<<} b", 2, "neuf 😀",
			"a {~~x~>y~~}{>>glose:v1 id=abcdef by=agent<<}{>>neuf 😀<<}{>>glose:v1 id=ghijkm re=abcdef by=author<<} b"},
		{"\ufeffL’{>>été<<}\r\nsuite\r\n{++x++}", 0, "hiver\r\nlong", "\ufeffL’{>>hiver\r\nlong<<}\r\nsuite\r\n{++x++}"},
	}
	for _, c := range cases {
		src := []byte(c.src)
		marks, _ := critic.Parse(src)
		a := critic.Group(marks)[0]
		members := append([]int{a.Main}, a.Comments...)
		idx := members[c.nth]
		out := apply(t, src, res(Amend(src, marks, a, idx, c.text)))
		if string(out) != c.want {
			t.Errorf("Amend(%q, %q) = %q, attendu %q", c.src, c.text, out, c.want)
		}
	}
}

func TestAmendRefusals(t *testing.T) {
	cases := []struct {
		src  string
		nth  int
		text string
		want error
	}{
		{"{>>a<<}", 0, "a", ErrUnchanged},
		{"{>>a<<}", 0, "", ErrEmpty},
		{"{>>a<<}", 0, "b <<} c", ErrDelimiter},
		{"{>>a<<}", 0, "b {++c", ErrDelimiter},
		{"{++a++}{>>b<<}", 0, "c", ErrKind},      // the proposal itself
		{"{==a==}{>>b<<}", 0, "c", ErrKind},      // the highlighted passage itself
		{"{>>a {++b++} c<<}", 0, "d", ErrNested}, // a nested mark: read-only
	}
	for _, c := range cases {
		src := []byte(c.src)
		marks, _ := critic.Parse(src)
		a := critic.Group(marks)[0]
		members := append([]int{a.Main}, a.Comments...)
		if _, err := Amend(src, marks, a, members[c.nth], c.text); !errors.Is(err, c.want) {
			t.Errorf("Amend(%q, %q) : erreur %v, attendu %v", c.src, c.text, err, c.want)
		}
	}
	// A mark of another annotation is refused.
	src := []byte("{>>a<<} b {>>c<<}")
	marks, _ := critic.Parse(src)
	if _, err := Amend(src, marks, critic.Group(marks)[0], 1, "d"); !errors.Is(err, ErrKind) {
		t.Errorf("commentaire d'une autre annotation : erreur %v", err)
	}
}

// FuzzAmend rewrites every comment of random texts: a rewriting is either
// refused, or changes only that comment, and every other mark keeps its kind.
func FuzzAmend(f *testing.F) {
	f.Add([]byte("a {==b==}{>>c<<}{>>d<<} e {>>f<<} {++g++}{>>h<<}"), "nouveau")
	f.Add([]byte("{>> {++x<< ++} {~~a~>b"), "<")
	f.Fuzz(func(t *testing.T, src []byte, text string) {
		marks, _ := critic.Parse(src)
		for _, a := range critic.Group(marks) {
			for _, idx := range append([]int{a.Main}, a.Comments...) {
				e, err := Amend(src, marks, a, idx, text)
				if err != nil {
					continue
				}
				if e.Start != marks[idx].Span.Start || e.End != marks[idx].Span.End {
					t.Fatalf("la réécriture déborde du commentaire : %+v", e)
				}
				out := e.Apply(src)
				faithful(t, src, out, e)
				after, _ := critic.Parse(out)
				if len(after) != len(marks) {
					t.Fatalf("nombre de marques changé : %q → %q", src, out)
				}
				for i := range after {
					if after[i].Kind != marks[i].Kind {
						t.Fatalf("type de marque changé : %q → %q", src, out)
					}
				}
			}
		}
	})
}
