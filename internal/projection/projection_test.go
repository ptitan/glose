package projection

import (
	"bytes"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/patch"
)

const sample = "Le juge {~~rejette~>casse~~}{>>pourquoi ?<<} le {++nouveau ++}pourvoi " +
	"{--très --}vite. {==Passage==}{>>note<<} fin.{>>seul<<}"

func TestViews(t *testing.T) {
	want := map[View]string{
		Current:  "Le juge rejette le pourvoi très vite. Passage fin.",
		Proposed: "Le juge casse le nouveau pourvoi vite. Passage fin.",
		Review:   "Le juge rejettecasse le nouveau pourvoi très vite. Passage fin.",
	}
	for v, w := range want {
		if got := string(Project([]byte(sample), v).Text); got != w {
			t.Errorf("vue %s :\n%q\nattendu\n%q", v, got, w)
		}
	}
}

func TestRolesInReview(t *testing.T) {
	p := Project([]byte(sample), Review)
	var got []string
	for _, s := range p.Segments {
		if s.Role != Plain {
			got = append(got, s.Role.String()+":"+string(p.Text[s.Out.Start:s.Out.End]))
		}
	}
	want := []string{"ancien:rejette", "nouveau:casse", "ajouté:nouveau ", "supprimé:très ", "surligné:Passage"}
	if len(got) != len(want) {
		t.Fatalf("rôles %q, attendu %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rôle %d : %q, attendu %q", i, got[i], want[i])
		}
	}
}

func TestCommentsArePlacedWhereTheyWere(t *testing.T) {
	p := Project([]byte(sample), Current)
	last := len(p.Marks) - 1 // {>>seul<<}, after "fin."
	if p.Marks[last].Kind != critic.Comment || p.Places[last] != (critic.Span{Start: len(p.Text), End: len(p.Text)}) {
		t.Fatalf("place du dernier commentaire : %+v (vue de %d octets)", p.Places[last], len(p.Text))
	}
}

func TestTextWithoutMarksIsItsOwnCurrentView(t *testing.T) {
	src := []byte("\ufeffL’été,\r\nrien à signaler.\n")
	for _, v := range []View{Current, Proposed, Review} {
		p := Project(src, v)
		if !bytes.Equal(p.Text, src) || len(p.Segments) != 1 || p.Segments[0].Src != (critic.Span{Start: 0, End: len(src)}) {
			t.Errorf("vue %s : %q, segments %+v", v, p.Text, p.Segments)
		}
	}
}

func TestMalformedMarksStayAsText(t *testing.T) {
	src := []byte("Début {++ jamais fermé, et {~~sans flèche~~}.")
	p := Project(src, Current)
	if !bytes.Equal(p.Text, src) || len(p.Issues) != 2 {
		t.Fatalf("vue %q, problèmes %v", p.Text, p.Issues)
	}
}

func TestSourceStartAndEnd(t *testing.T) {
	src := []byte(sample)
	p := Project(src, Current)
	// "le pourvoi" in the view: across the removed addition.
	i := bytes.Index(p.Text, []byte("le pourvoi"))
	s, _, ok1 := p.SourceStart(i)
	e, _, ok2 := p.SourceEnd(i + len("le pourvoi"))
	if !ok1 || !ok2 {
		t.Fatal("hors de la vue")
	}
	if got := string(src[s:e]); got != "le {++nouveau ++}pourvoi" {
		t.Fatalf("source %q", got)
	}
	// At the boundary "le |pourvoi", start and end resolve to different sides.
	b := i + len("le ")
	se, _, _ := p.SourceEnd(b)
	ss, _, _ := p.SourceStart(b)
	if string(src[se:ss]) != "{++nouveau ++}" {
		t.Fatalf("frontière : %q", src[se:ss])
	}
	if _, _, ok := p.SourceStart(len(p.Text)); ok {
		t.Error("SourceStart en fin de vue devrait échouer")
	}
	if _, _, ok := p.SourceEnd(0); ok {
		t.Error("SourceEnd en début de vue devrait échouer")
	}
}

// decideAll closes every annotation of src with the given decision for
// proposals, resolving discussions, from the end so offsets stay valid.
func decideAll(t *testing.T, src []byte, accept bool) []byte {
	marks, _ := critic.Parse(src)
	groups := critic.Group(marks)
	out := src
	for i := len(groups) - 1; i >= 0; i-- {
		a := groups[i]
		var e patch.Edit
		var err error
		switch k := marks[a.Main].Kind; {
		case k == critic.Comment || k == critic.Highlight:
			e, err = patch.Resolve(out, marks, a)
		case accept:
			e, err = patch.Accept(out, marks, a)
		default:
			e, err = patch.Reject(out, marks, a)
		}
		if err != nil {
			return nil // a malformed neighbour: no comparison
		}
		out = e.Apply(out)
	}
	return out
}

// FuzzProjection checks the provenance of every byte, and that the Current
// and Proposed views equal the text obtained by deciding every annotation with
// package patch: the two packages must agree.
func FuzzProjection(f *testing.F) {
	f.Add([]byte(sample))
	f.Add([]byte("{++a++}{>>b<<}{--c--}{~~d~>e~~}{==f==}"))
	f.Add([]byte("\ufeffx {>> y\r\nz <<} {++ jamais"))
	f.Fuzz(func(t *testing.T, src []byte) {
		for _, v := range []View{Current, Proposed, Review} {
			p := Project(src, v)
			pos, prevSrc := 0, 0
			for _, s := range p.Segments {
				if s.Out.Start != pos || s.Out.Len() != s.Src.Len() || s.Src.Start < prevSrc {
					t.Fatalf("segment incohérent %+v (position %d)", s, pos)
				}
				if !bytes.Equal(p.Text[s.Out.Start:s.Out.End], src[s.Src.Start:s.Src.End]) {
					t.Fatalf("octets différents pour %+v", s)
				}
				pos, prevSrc = s.Out.End, s.Src.End
			}
			if pos != len(p.Text) {
				t.Fatalf("les segments couvrent %d octets sur %d", pos, len(p.Text))
			}
			for off := 0; off < len(p.Text); off++ {
				s, _, ok := p.SourceStart(off)
				if !ok || src[s] != p.Text[off] {
					t.Fatalf("octet %d de la vue mal relié à la source", off)
				}
			}
		}
		if _, issues := critic.Parse(src); len(issues) > 0 {
			return
		}
		if got := decideAll(t, src, false); got != nil && !bytes.Equal(got, Project(src, Current).Text) {
			t.Fatalf("vue actuelle %q, tout rejeter donne %q", Project(src, Current).Text, got)
		}
		if got := decideAll(t, src, true); got != nil && !bytes.Equal(got, Project(src, Proposed).Text) {
			t.Fatalf("vue proposée %q, tout accepter donne %q", Project(src, Proposed).Text, got)
		}
	})
}
