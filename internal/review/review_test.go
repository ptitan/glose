package review

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/notes"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/projection"
)

// fixed returns a generator that draws the given identifiers in order.
func fixed(ids ...string) *Gen {
	g := NewGen(nil)
	g.rand = func(b []byte) error {
		if len(ids) == 0 {
			return errors.New("plus d'identifiants")
		}
		id := ids[0]
		ids = ids[1:]
		for i := range b {
			b[i] = byte(strings.IndexByte(idAlphabet, id[i]))
		}
		return nil
	}
	return g
}

func TestMetaRoundTrip(t *testing.T) {
	m := Meta{ID: "abcdef", Re: "ghijkm", By: Agent}
	b := m.Bytes()
	if string(b) != "{>>glose:v1 id=abcdef re=ghijkm by=agent<<}" {
		t.Fatalf("%s", b)
	}
	got, err := ParseMeta(b[3 : len(b)-3])
	if err != nil || got != m {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"glose:v2 id=abcd", "glose:v1", "glose:v1 id=AB", "glose:v1 id=abcd by=robot", "glose:v1 id=abcd id=efgh", "glose:v1 nimporte"} {
		if _, err := ParseMeta([]byte(bad)); err == nil {
			t.Errorf("%q accepté", bad)
		}
	}
}

func TestReadStructure(t *testing.T) {
	src := "Le {==passage==}{>>remarque<<}{>>glose:v1 id=aaaa by=author<<}{>>réponse<<}{>>glose:v1 id=bbbb re=aaaa by=agent<<} " +
		"et {~~ancien~>nouveau~~}{>>glose:v1 id=cccc by=agent<<}{>>parce que<<} puis {>>seul<<}."
	d := Read([]byte(src))
	if len(d.Issues) != 0 {
		t.Fatalf("anomalies : %+v", d.Issues)
	}
	if len(d.Annotations) != 3 {
		t.Fatalf("%d annotations", len(d.Annotations))
	}
	a := d.Annotations[0]
	if a.ID != "aaaa" || a.By != Author || len(a.Messages) != 2 || a.Messages[0].ID != "aaaa" ||
		a.Messages[1].ID != "bbbb" || a.Messages[1].By != Agent || len(a.Replies()) != 1 {
		t.Fatalf("annotation 0 : %+v", a)
	}
	b := d.Annotations[1]
	if b.ID != "cccc" || len(b.Messages) != 1 || b.Messages[0].ID != "" || b.Messages[0].Meta != nil {
		t.Fatalf("annotation 1 : %+v", b)
	}
	c := d.Annotations[2]
	if c.ID != "" || c.Meta != nil || len(c.Messages) != 1 {
		t.Fatalf("annotation 2 : %+v", c)
	}
	if got := strings.Join(d.IDs(), ","); got != "aaaa,bbbb,cccc" {
		t.Fatalf("identifiants %s", got)
	}
}

func TestReadIssues(t *testing.T) {
	cases := map[string]string{
		"{>>a<<}{>>glose:v1 id=aaaa<<} {>>b<<}{>>glose:v1 id=aaaa<<}": IssueDuplicated,
		"{>>glose:v1 id=aaaa<<}":                                                                           IssueMisplaced,
		"{>>a<<}{>>glose:v1 id=aaaa<<}{>>glose:v1 id=bbbb<<}":                                              IssueMisplaced,
		"{>>a<<}{>>glose:nimporte<<}":                                                                      IssueMalformed,
		"{>>a<<}{>>glose:v1 id=aaaa<<}{>>b<<}{>>glose:v1 id=bbbb re=cccc<<}":                               IssueMoved,
		"{++a++}{>>glose:v1 id=aaaa<<} {--b--}{>>glose:v1 id=bbbb<<}{>>c<<}{>>glose:v1 id=aaaa re=bbbb<<}": IssueDuplicated,
	}
	for src, want := range cases {
		d := Read([]byte(src))
		if len(d.Issues) == 0 || !strings.HasPrefix(d.Issues[0].Message, want) {
			t.Errorf("%s : %+v, attendu %s", src, d.Issues, want)
		}
	}
}

func TestAssign(t *testing.T) {
	src := "Un {==passage==}{>>remarque<<}{>>réponse<<} et {~~a~>b~~}{>>glose:v1 id=kkkk by=agent<<}{>>pourquoi ?<<}.\r\n{>>seul<<}"
	out, err := Assign([]byte(src), 0, len(src), Author, fixed("aaaaaa", "bbbbbb", "cccccc", "dddddd"))
	if err != nil {
		t.Fatal(err)
	}
	want := "Un {==passage==}{>>remarque<<}{>>glose:v1 id=aaaaaa by=author<<}{>>réponse<<}{>>glose:v1 id=bbbbbb re=aaaaaa by=author<<}" +
		" et {~~a~>b~~}{>>glose:v1 id=kkkk by=agent<<}{>>pourquoi ?<<}{>>glose:v1 id=cccccc re=kkkk by=author<<}.\r\n" +
		"{>>seul<<}{>>glose:v1 id=dddddd by=author<<}"
	if string(out) != want {
		t.Fatalf("obtenu\n%s\nattendu\n%s", out, want)
	}
	if d := Read(out); len(d.Issues) != 0 || len(d.IDs()) != 5 {
		t.Fatalf("%+v %v", d.Issues, d.IDs())
	}
	// Nothing missing: the text is returned unchanged.
	again, err := Assign(out, 0, len(out), Agent, NewGen(nil))
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("seconde passe : %v", err)
	}
}

func TestAssignOnlyInRange(t *testing.T) {
	src := "{>>a<<} texte {>>b<<}"
	at := strings.Index(src, "{>>b")
	out, err := Assign([]byte(src), at, len(src), Agent, fixed("zzzzzz"))
	if err != nil || string(out) != "{>>a<<} texte {>>b<<}{>>glose:v1 id=zzzzzz by=agent<<}" {
		t.Fatalf("%s %v", out, err)
	}
}

func TestSameButMeta(t *testing.T) {
	src := []byte("Un {++mot++} ici.")
	if err := SameButMeta(src, []byte("Un {++mot++}{>>glose:v1 id=aaaa<<} ici.")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Un {++mots++} ici.", "Un {++mot++}{>>x<<} ici.", "Un mot ici.", "Un {++mot++} ici !"} {
		if SameButMeta(src, []byte(bad)) == nil {
			t.Errorf("%q accepté", bad)
		}
	}
}

func TestWithIDsAndRemoveReply(t *testing.T) {
	src := []byte("Avant {>>a<<}{>>glose:v1 id=aaaa by=author<<} après.")
	d := Read(src)
	a := d.Annotations[0]
	e, err := patch.Reply(src, d.Marks, d.Group(a), "réponse")
	if err != nil {
		t.Fatal(err)
	}
	e, err = WithIDs(src, e, Agent, fixed("rrrrrr"))
	if err != nil {
		t.Fatal(err)
	}
	out := e.Apply(src)
	want := "Avant {>>a<<}{>>glose:v1 id=aaaa by=author<<}{>>réponse<<}{>>glose:v1 id=rrrrrr re=aaaa by=agent<<} après."
	if string(out) != want {
		t.Fatalf("%s", out)
	}
	at := strings.Index(want, "{>>réponse")
	r, err := RemoveReply(out, at)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Apply(out); !bytes.Equal(got, src) {
		t.Fatalf("%s", got)
	}
	if _, err := RemoveReply(out, strings.Index(want, "{>>a<<}")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("la tête a été retirée : %v", err)
	}
}

func TestDecisionsRemoveTheIdentity(t *testing.T) {
	src := []byte("x {~~a~>b~~}{>>glose:v1 id=aaaa by=agent<<}{>>car<<}{>>glose:v1 id=bbbb re=aaaa by=agent<<} y")
	d := Read(src)
	e, err := patch.Accept(src, d.Marks, d.Group(d.Annotations[0]))
	if err != nil || string(e.Apply(src)) != "x b y" {
		t.Fatalf("%s %v", e.Apply(src), err)
	}
}

func TestGenAvoidsTakenIdentifiers(t *testing.T) {
	g := fixed("aaaaaa", "aaaaaa", "bbbbbb")
	g.Take("aaaaaa")
	id, err := g.New()
	if err != nil || id != "bbbbbb" {
		t.Fatalf("%s %v", id, err)
	}
	real := NewGen(nil)
	seen := map[string]bool{}
	for range 1000 {
		id, err := real.New()
		if err != nil || seen[id] || !ValidID(id) {
			t.Fatalf("%s %v", id, err)
		}
		seen[id] = true
	}
}

func FuzzAssign(f *testing.F) {
	f.Add("{==a==}{>>b<<}{>>c<<} {~~d~>e~~} {>>glose:v1 id=aaaa<<}")
	f.Add("`{>>x<<}` {++y++}{>>z<<}")
	f.Fuzz(func(t *testing.T, s string) {
		out, err := Assign([]byte(s), 0, len(s), Author, NewGen(nil))
		if err != nil {
			return
		}
		if err := SameButMeta([]byte(s), out); err != nil {
			t.Fatalf("%q", out)
		}
		d := Read(out)
		for _, a := range d.Annotations {
			if a.Meta == nil {
				t.Fatalf("annotation sans identifiant après Assign : %q", out)
			}
		}
	})
}

func TestReplyGivesTheAnnotationItsIdentity(t *testing.T) {
	src := []byte("Texte {++ajout++}{>>pourquoi ?<<} fin.")
	e, at, err := Reply(src, strings.Index(string(src), "{++"), "", "parce que", Author, fixed("aaaaaa", "bbbbbb", "cccccc"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(e.Apply(src))
	want := "Texte {++ajout++}{>>glose:v1 id=aaaaaa by=author<<}{>>pourquoi ?<<}{>>glose:v1 id=bbbbbb re=aaaaaa by=author<<}" +
		"{>>parce que<<}{>>glose:v1 id=cccccc re=aaaaaa by=author<<} fin."
	if out != want {
		t.Fatalf("obtenu\n%s\nattendu\n%s", out, want)
	}
	if !strings.HasPrefix(out[at:], "{>>parce que<<}") {
		t.Fatalf("position de la réponse : %d", at)
	}
	if e.Start > strings.Index(string(src), "{++") {
		t.Fatalf("l'édition ne couvre pas l'annotation : %d", e.Start)
	}
}

func TestCommentNextToAnAnnotationLeavesItAlone(t *testing.T) {
	src := []byte("{++a++}mot suivant")
	at := strings.Index(string(src), "mot")
	e, err := Comment(src, critic.Span{Start: at, End: at + 3}, "remarque", Author, fixed("aaaaaa"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(e.Apply(src)); got != "{++a++}{==mot==}{>>remarque<<}{>>glose:v1 id=aaaaaa by=author<<} suivant" {
		t.Fatalf("%s", got)
	}
}

func TestProposeGivesEachProposalItsIdentity(t *testing.T) {
	src := []byte("Le chat dort bien.")
	e, _, err := Propose(src, critic.Span{Start: 0, End: len(src)}, "Le chien dort.", "plus court", Agent, fixed("aaaaaa", "bbbbbb", "cccccc"))
	if err != nil {
		t.Fatal(err)
	}
	want := "Le {~~chat~>chien~~}{>>glose:v1 id=aaaaaa by=agent<<}{>>plus court<<}{>>glose:v1 id=bbbbbb re=aaaaaa by=agent<<} dort{-- bien--}{>>glose:v1 id=cccccc by=agent<<}."
	if got := string(e.Apply(src)); got != want {
		t.Fatalf("obtenu\n%s\nattendu\n%s", got, want)
	}
}

// P2.1: an annotation moved elsewhere keeps its identity; a copy is a
// duplicate; an identity erased is missing (and is noticed against the
// journal, package navette).
func TestMoveCopyErase(t *testing.T) {
	ann := "{~~a~>b~~}{>>glose:v1 id=aaaa by=agent<<}{>>car<<}{>>glose:v1 id=bbbb re=aaaa by=agent<<}"
	moved := "Premier paragraphe.\n\nSecond " + ann + " paragraphe.\n"
	d := Read([]byte(moved))
	a, ok := d.Find("aaaa")
	if !ok || len(d.Issues) != 0 || a.Span.Start != strings.Index(moved, "{~~") || a.Replies()[0].ID != "bbbb" {
		t.Fatalf("déplacement : %+v %+v", a, d.Issues)
	}
	copied := ann + " et " + ann
	d = Read([]byte(copied))
	if len(d.Issues) != 2 || !strings.HasPrefix(d.Issues[0].Message, IssueDuplicated) {
		t.Fatalf("copie : %+v", d.Issues)
	}
	erased := strings.Replace(ann, "{>>glose:v1 id=aaaa by=agent<<}", "", 1)
	d = Read([]byte(erased))
	// The reply's technical comment still names aaaa: the annotation has lost
	// its own; the thread is read without identity for the head.
	if d.Annotations[0].ID != "" || len(d.Issues) != 0 {
		t.Fatalf("effacement : %+v %+v", d.Annotations[0], d.Issues)
	}
	out, err := Assign([]byte(erased), 0, len(erased), Agent, NewGen(nil))
	if err != nil || string(out) != ann {
		t.Fatalf("restitution : %s %v", out, err)
	}
}

// Revue Codex (notes) : deux notes ajoutées se décident chacune pour soi ; une
// étiquette écrite dans le texte n'est pas reprise ; remplacer une note par une
// autre lie tout (annuler ne laisse rien).
func TestProposeNotes(t *testing.T) {
	src := []byte("Un texte.\n")
	e, _, err := Propose(src, critic.Span{Start: 0, End: 9}, "Un^[A.] texte^[B.].", "", Author, NewGen(nil))
	if err != nil {
		t.Fatal(err)
	}
	out := e.Apply(src)
	l := notes.Links(out)
	for start, link := range l {
		if len(link.Partners) != 1 {
			t.Fatalf("groupe de %d autour de %d : %s", len(link.Partners)+1, start, out)
		}
	}
	e, _, _ = Propose(src, critic.Span{Start: 0, End: 9}, "Un appel[^1] et une note^[Nouvelle.].", "", Author, NewGen(nil))
	if out := string(e.Apply(src)); !strings.Contains(out, "[^2]: Nouvelle.") {
		t.Fatalf("%s", out)
	}
	src = []byte("Texte[^a].\n\n[^a]: Ancienne.\n")
	e, main, err := Propose(src, critic.Span{Start: 0, End: 10}, "Texte^[Nouvelle.].", "", Author, NewGen(nil))
	if err != nil {
		t.Fatal(err)
	}
	out = e.Apply(src)
	if !strings.Contains(string(out), "{~~[^a]~>[^1]~~}") {
		t.Fatalf("%s", out)
	}
	g := notes.Links(out)
	sub := strings.Index(string(out), "{~~[^a]")
	if sub < main.Start || len(g[sub].Partners) != 2 {
		t.Fatalf("liens : %+v\n%s", g[sub], out)
	}
}

// Revue de P4.10 : une réécriture d'un tenant qui englobe la définition
// d'une note est refusée (elle resterait seule, son appel supprimé).
func TestProposeWholeRefusesANoteDefinition(t *testing.T) {
	src := []byte("Texte[^a].\n\n[^a]: Note.\n\nFin.\n")
	_, _, err := ProposeWhole(src, critic.Span{Start: 0, End: len(src) - 1}, "Autre.\n\n[^a]: Note.\n\nSuite.", "", Author, NewGen(nil))
	if !errors.Is(err, ErrWholeNote) {
		t.Fatalf("erreur : %v", err)
	}
	// Une note ajoutée par ^[…] reste permise, avec sa proposition liée.
	e, _, err := ProposeWhole([]byte("Un texte.\n"), critic.Span{Start: 0, End: 9}, "Un autre texte^[Note.].", "", Author, NewGen(nil))
	if err != nil || !strings.Contains(string(e.Apply([]byte("Un texte.\n"))), "[^1]:") {
		t.Fatalf("%v %q", err, e.Apply([]byte("Un texte.\n")))
	}
}

// Revue générale : « B[^a]: suite » au milieu d'une ligne est un appel, pas
// une définition ; supprimer l'autre appel ne propose pas la suppression de
// la note, toujours appelée.
func TestNoteReferenceFollowedByAColon(t *testing.T) {
	src := []byte("A[^a]. B[^a]: suite\n\n[^a]: note\n")
	e, _, err := Propose(src, critic.Span{Start: 0, End: 6}, "A.", "", Author, NewGen(nil))
	if err != nil {
		t.Fatal(err)
	}
	out := e.Apply(src)
	if got := string(projection.Project(out, projection.Proposed).Text); got != "A. B[^a]: suite\n\n[^a]: note\n" {
		t.Fatalf("%q\n%s", got, out)
	}
}

// Contre-vérification : une définition dans un encadré ou une liste suit son
// dernier appel supprimé, comme une définition ordinaire.
func TestNoteDefinitionInAContainerFollowsItsLastReference(t *testing.T) {
	for _, def := range []string{"> [^a]: Note.\n", "- [^a]: Note.\n", "1. [^a]: Note.\n"} {
		src := []byte("Appel[^a].\n\n" + def)
		e, _, err := Propose(src, critic.Span{Start: 0, End: 10}, "Appel.", "", Author, NewGen(nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(projection.Project(e.Apply(src), projection.Proposed).Text); strings.Contains(got, "[^a]") {
			t.Errorf("%q : définition restée :\n%q", def, got)
		}
	}
}

// P9.1 : l'auteur réécrit ses propres messages (commentaire, remarque d'un
// passage, réponse) ; l'identité et le fil restent.
func TestAmend(t *testing.T) {
	const (
		cmt = "a {>>vieux<<}{>>glose:v1 id=abcdef by=author<<} b"
		hl  = "a {==p==}{>>vieux<<}{>>glose:v1 id=abcdef by=author<<}{>>r<<}{>>glose:v1 id=ghijkm re=abcdef by=agent<<} b"
		rep = "a {~~x~>y~~}{>>glose:v1 id=abcdef by=agent<<}{>>vieux<<}{>>glose:v1 id=ghijkm re=abcdef by=author<<} b"
	)
	cases := []struct {
		src, at, want string
	}{
		{cmt, "{>>vieux", "a {>>neuf<<}{>>glose:v1 id=abcdef by=author<<} b"},
		{hl, "{>>vieux", "a {==p==}{>>neuf<<}{>>glose:v1 id=abcdef by=author<<}{>>r<<}{>>glose:v1 id=ghijkm re=abcdef by=agent<<} b"},
		{rep, "{>>vieux", "a {~~x~>y~~}{>>glose:v1 id=abcdef by=agent<<}{>>neuf<<}{>>glose:v1 id=ghijkm re=abcdef by=author<<} b"},
	}
	for _, c := range cases {
		src := []byte(c.src)
		e, m, err := Amend(src, bytes.Index(src, []byte(c.at)), "neuf", Author, NewGen(nil))
		if err != nil {
			t.Fatalf("Amend(%q) : %v", c.src, err)
		}
		if out := e.Apply(src); string(out) != c.want {
			t.Errorf("Amend(%q) = %q, attendu %q", c.src, out, c.want)
		}
		if m.ID == "" || m.By != Author {
			t.Errorf("message rendu : %+v", m)
		}
	}
}

func TestAmendRefusals(t *testing.T) {
	agent := []byte("a {==p==}{>>r<<}{>>glose:v1 id=abcdef by=agent<<} b")
	if _, _, err := Amend(agent, bytes.Index(agent, []byte("{>>r")), "x", Author, NewGen(nil)); !errors.Is(err, ErrAmendNotYours) {
		t.Errorf("message de l'agent : %v", err)
	}
	fix := []byte("a {~~x~>y~~}{>>glose:v1 id=abcdef by=author<<}{>>z<<}{>>glose:v1 id=ghijkm re=abcdef by=agent fix=1<<} b")
	if _, _, err := Amend(fix, bytes.Index(fix, []byte("{>>z")), "w", Agent, NewGen(nil)); !errors.Is(err, ErrAmendFix) {
		t.Errorf("correction de l'agent : %v", err)
	}
	mine := []byte("a {>>m<<}{>>glose:v1 id=abcdef by=author<<} b")
	if _, _, err := Amend(mine, 2, "glose:v1 id=zzzzzz", Author, NewGen(nil)); !errors.Is(err, ErrAmendMeta) {
		t.Errorf("texte d'identifiant : %v", err)
	}
	if _, _, err := Amend(mine, 3, "x", Author, NewGen(nil)); !errors.Is(err, ErrNotFound) {
		t.Errorf("position hors d'un message : %v", err)
	}
	if _, _, err := Amend(mine, 2, "m", Author, NewGen(nil)); !errors.Is(err, patch.ErrUnchanged) {
		t.Errorf("message inchangé : %v", err)
	}
	// The technical comment is not a message: it cannot be amended.
	if _, _, err := Amend(mine, bytes.Index(mine, []byte("{>>glose")), "x", Author, NewGen(nil)); !errors.Is(err, ErrNotFound) {
		t.Errorf("commentaire technique : %v", err)
	}
}

// Un commentaire écrit à la main, sans identité, reçoit la sienne en étant
// modifié, attribuée à celui qui le modifie.
func TestAmendGivesAHandWrittenCommentItsIdentity(t *testing.T) {
	src := []byte("a {>>main<<} b")
	e, _, err := Amend(src, 2, "neuf", Author, fixed("pqrstu"))
	if err != nil {
		t.Fatal(err)
	}
	if out := string(e.Apply(src)); out != "a {>>neuf<<}{>>glose:v1 id=pqrstu by=author<<} b" {
		t.Fatalf("%q", out)
	}
}

// P10.6 (D-49): to= and via= in the technical comment.
func TestMetaToAndVia(t *testing.T) {
	m := Meta{ID: "abcd", By: Author, To: "Sol"}
	b := m.Bytes()
	if string(b) != "{>>glose:v1 id=abcd by=author to=Sol<<}" {
		t.Fatalf("%s", b)
	}
	got, err := ParseMeta(b[3 : len(b)-3])
	if err != nil || got != m {
		t.Fatalf("%+v %v", got, err)
	}
	v := Meta{ID: "efgh", Re: "abcd", By: Agent, Via: "claude-opus"}
	if got, err := ParseMeta(v.Bytes()[3 : len(v.Bytes())-3]); err != nil || got != v {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"glose:v1 id=abcd to=Sol!", "glose:v1 id=abcd via=", "glose:v1 id=abcd to=" + strings.Repeat("a", 33)} {
		if _, err := ParseMeta([]byte(bad)); err == nil {
			t.Errorf("%q accepté", bad)
		}
	}
}

func TestAddressAndSignTouchOnlyTheNewMarks(t *testing.T) {
	src := []byte("Un {==mot==}{>>ancien<<}{>>glose:v1 id=aaaa by=author<<} et une fin.\r\n")
	gen := NewGen(map[string]bool{"aaaa": true})
	at := strings.Index(string(src), "fin")
	e, err := Comment(src, critic.Span{Start: at, End: at + 3}, "Pourquoi ?", Author, gen)
	if err != nil {
		t.Fatal(err)
	}
	plain := e.Apply(src)
	ea, err := Address(src, e, "Sol")
	if err != nil {
		t.Fatal(err)
	}
	out := ea.Apply(src)
	// Only to=Sol is added, to the new annotation; the old one is intact.
	if strings.Replace(string(out), " to=Sol", "", 1) != string(plain) || strings.Count(string(out), "to=Sol") != 1 {
		t.Fatalf("%s", out)
	}
	if !strings.HasPrefix(string(out), "Un {==mot==}{>>ancien<<}{>>glose:v1 id=aaaa by=author<<} et une ") || !strings.HasSuffix(string(out), "\r\n") {
		t.Fatalf("%s", out)
	}
	d := Read(out)
	if a, _ := d.Find("aaaa"); a.Recipient() != "" {
		t.Fatal("ancienne annotation adressée")
	}
	if len(d.Annotations) != 2 || d.Annotations[1].Recipient() != "Sol" {
		t.Fatalf("%+v", d.Annotations)
	}
	// A reply of the author addressed to another agent changes the recipient.
	r, _, err := Reply(out, d.Annotations[1].Span.Start, d.Annotations[1].ID, "Plutôt Luna.", Author, gen)
	if err != nil {
		t.Fatal(err)
	}
	r, err = Address(out, r, "Luna")
	if err != nil {
		t.Fatal(err)
	}
	if a := Read(r.Apply(out)).Annotations[1]; a.Recipient() != "Luna" || a.To != "Sol" {
		t.Fatalf("%+v", a)
	}
	// via= on what the agent writes.
	s, _, err := Reply(out, d.Annotations[0].Span.Start, "aaaa", "Corrigé.", Agent, gen)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Sign(out, s, "Sol")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Read(s.Apply(out)).Find("aaaa")
	if last := a.Messages[len(a.Messages)-1]; last.Via != "Sol" || last.By != Agent {
		t.Fatalf("%+v", last)
	}
	if _, err := Address(src, e, "Sol Levant"); err == nil {
		t.Fatal("nom invalide accepté")
	}
}

// P10.11: the model of the agent, signed beside its name.
func TestSignModel(t *testing.T) {
	m := Meta{ID: "abcd", By: Agent, Via: "Sol", Model: "ollama-cloud/glm-5.3#high"}
	b := m.Bytes()
	if string(b) != "{>>glose:v1 id=abcd by=agent via=Sol model=ollama-cloud/glm-5.3#high<<}" {
		t.Fatalf("%s", b)
	}
	if got, err := ParseMeta(b[3 : len(b)-3]); err != nil || got != m {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := ParseMeta([]byte("glose:v1 id=abcd model=a<b")); err == nil {
		t.Fatal("modèle invalide accepté")
	}
}

// Codex audit: SignModel itself, byte for byte (BOM, mixed line endings, an
// existing annotation left as it is), and a model without a name.
func TestSignModelBytes(t *testing.T) {
	src := []byte("\ufeffUn{>>Ancien.<<}{>>glose:v1 id=aaaa by=author<<} mot\r\net la suite.\n")
	gen := NewGen(map[string]bool{"aaaa": true})
	e, _, err := Reply(src, 3, "aaaa", "Réponse.", Agent, gen)
	if err != nil {
		t.Fatal(err)
	}
	plain := string(e.Apply(src))
	signed, err := SignModel(src, e, "Sol", "ollama-cloud/glm-5.3#high")
	if err != nil {
		t.Fatal(err)
	}
	got := string(signed.Apply(src))
	if want := strings.Replace(plain, " by=agent<<}", " by=agent via=Sol model=ollama-cloud/glm-5.3#high<<}", 1); got != want {
		t.Fatalf("%q\nattendu %q", got, want)
	}
	if !strings.HasPrefix(got, "\ufeffUn{>>Ancien.<<}{>>glose:v1 id=aaaa by=author<<}") || !strings.HasSuffix(got, " mot\r\net la suite.\n") {
		t.Fatalf("%q", got)
	}
	anon, err := SignModel(src, e, "", "gpt-6.1-sol")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(anon.Apply(src)); got != strings.Replace(plain, " by=agent<<}", " by=agent model=gpt-6.1-sol<<}", 1) {
		t.Fatalf("%q", got)
	}
}
