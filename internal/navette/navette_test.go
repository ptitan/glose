package navette

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
)

type world struct {
	t   *testing.T
	dir string
	n   *Navette
}

func newWorld(t *testing.T, files map[string]string) *world {
	t.Helper()
	dir := t.TempDir()
	for name, c := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &world{t, dir, New(p)}
}

func (w *world) read(name string) string {
	b, err := os.ReadFile(filepath.Join(w.dir, name))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

func (w *world) write(name, content string) {
	if err := os.WriteFile(filepath.Join(w.dir, name), []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) do(actor string, file int, kind, decision string, c Compute) *Result {
	w.t.Helper()
	r, err := w.n.Do(actor, file, "", kind, decision, c)
	if err != nil {
		w.t.Fatalf("%s : %v", kind, err)
	}
	return r
}

func span(src []byte, sub string) critic.Span {
	i := strings.Index(string(src), sub)
	return critic.Span{Start: i, End: i + len(sub)}
}

func noMeta(s string) string {
	marks, _ := critic.Parse([]byte(s))
	var b strings.Builder
	pos := 0
	for _, m := range marks {
		if m.Kind == critic.Comment && review.IsMeta([]byte(s[m.Text.Start:m.Text.End])) {
			b.WriteString(s[pos:m.Span.Start])
			pos = m.Span.End
		}
	}
	b.WriteString(s[pos:])
	return b.String()
}

// The cycle of the P2 criterion (ROADMAP): the author makes 2 comments and 1
// proposal, then hands over. The agent replies, makes 2 changes, then hands
// over. The author accepts one, rejects the other, and resolves the threads.
// At the end no annotation is left, the journal is complete and consistent,
// and the file is exactly as expected.
func TestFullCycle(t *testing.T) {
	src := "# Titre\n\nLe premier paragraphe parle du contrat.\n\nLe second paragraphe parle de la responsabilité civile.\n\nLe troisième conclut.\n"
	w := newWorld(t, map[string]string{"a.md": src})
	A, G := review.Author, review.Agent

	// Turn 1, the author.
	w.do(A, 0, "comment", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		return review.Comment(s, span(s, "contrat"), "Lequel ?", A, g)
	})
	w.do(A, 0, "comment", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		at := span(s, "Le troisième conclut.").End
		return review.Comment(s, critic.Span{Start: at, End: at}, "Trop court.", A, g)
	})
	w.do(A, 0, "propose", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		e, _, err := review.Propose(s, span(s, "parle de la"), "traite de la", "", A, g)
		return e, err
	})
	if _, err := w.n.Do(G, 0, "", "reply", "", nil); !errors.Is(err, ErrAgentNot) {
		t.Fatalf("l'agent écrit pendant le tour de l'auteur : %v", err)
	}
	if _, err := w.n.Handoff(G, ""); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("remise de l'agent pendant le tour de l'auteur : %v", err)
	}
	if _, err := w.n.Handoff(A, "Trois remarques."); err != nil {
		t.Fatal(err)
	}
	st, _, _, _ := w.n.State()
	if st.Turn != 2 || st.Hand != G || st.Annotations != 3 || st.Statuses[StatusOpen] != 3 || len(st.Problems) != 0 {
		t.Fatalf("état après la remise : %+v", st)
	}
	if _, err := w.n.Do(A, 0, "", "comment", "", nil); !errors.Is(err, ErrAgentTurn) {
		t.Fatalf("l'auteur écrit pendant le tour de l'agent : %v", err)
	}

	// Turn 2, the agent: a reply by command, two free changes in the file.
	d := review.Read([]byte(w.read("a.md")))
	first := d.Annotations[0]
	w.do(G, 0, "reply", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		e, _, err := review.Reply(s, 0, first.ID, "Le contrat de vente.", G, g)
		return e, err
	})
	cur := w.read("a.md")
	cur = strings.Replace(cur, "Le premier paragraphe", "Ce premier paragraphe", 1)
	cur = strings.Replace(cur, "Le troisième conclut.", "Le troisième paragraphe conclut.", 1)
	w.write("a.md", cur)
	ho, err := w.n.Handoff(G, "Deux corrections.")
	if err != nil {
		t.Fatal(err)
	}
	if ho.Converted != 2 {
		t.Fatalf("%d propositions converties :\n%s", ho.Converted, w.read("a.md"))
	}
	st, h, f, _ := w.n.State()
	if st.Turn != 3 || st.Hand != A || st.Annotations != 5 || len(st.Problems) != 0 {
		t.Fatalf("état après done : %+v\n%s", st, w.read("a.md"))
	}
	for _, a := range f.Annot[0] {
		if a.ID == "" {
			t.Fatalf("annotation sans identité : %+v", a)
		}
	}
	_ = h

	// Turn 3, the author decides.
	text := w.read("a.md")
	t.Logf("avant les décisions :\n%s", text)
	decide := func(sub, decision string) {
		w.do(A, 0, "decide", decision, func(s []byte, _ *review.Gen) (patch.Edit, error) {
			return DecideRange(s, strings.Index(string(s), sub), 0, decision)
		})
	}
	resolve := func(sub string) {
		w.do(A, 0, "resolve", Resolve, func(s []byte, _ *review.Gen) (patch.Edit, error) {
			return ResolveAt(s, strings.Index(string(s), sub))
		})
	}
	decide("{~~Le~>Ce~~}", Accept)
	decide("{++paragraphe ++}", Reject)
	decide("{~~parle~>traite~~}", Accept)
	resolve("{==contrat==}")
	resolve("{>>Trop court.<<}")
	want := "# Titre\n\nCe premier paragraphe parle du contrat.\n\nLe second paragraphe traite de la responsabilité civile.\n\nLe troisième conclut.\n"
	if got := w.read("a.md"); got != want {
		t.Fatalf("fichier final :\n%q\n%q", got, want)
	}
	if _, err := w.n.Handoff(A, "Fini."); err != nil {
		t.Fatal(err)
	}

	// The journal: complete and consistent.
	st, h, _, _ = w.n.State()
	if st.Annotations != 0 || len(st.Problems) != 0 || len(h.Pending) != 0 || len(h.Problems) != 0 {
		t.Fatalf("état final : %+v", st)
	}
	if len(h.Turns) != 3 || h.Turns[0].Actor != A || h.Turns[1].Actor != G || h.Turns[1].TurnEv.Note != "Deux corrections." {
		t.Fatalf("tours : %+v", h.Turns)
	}
	closed := h.Closed()
	decisions := map[string]int{}
	for id := range h.Born() {
		c, ok := closed[id]
		if !ok {
			t.Fatalf("identifiant %s jamais clos", id)
		}
		if c.Kind != "message" {
			decisions[c.Decision]++
		}
	}
	if decisions[Accept] != 2 || decisions[Reject] != 1 || decisions[Resolve] != 2 {
		t.Fatalf("décisions : %v", decisions)
	}
	// The reply of the agent is archived in the thread of the first comment.
	c := closed[first.ID]
	if len(c.Messages) != 2 || c.Messages[1].By != G || c.Messages[1].Text != "Le contrat de vente." || c.Text != "contrat" {
		t.Fatalf("copie : %+v", c)
	}
	// Every operation of the files is confirmed, and the hashes chain.
	ops := h.Ops
	for i := 1; i < len(ops); i++ {
		if ops[i].Files[0].Before != ops[i-1].Files[0].After {
			// The agent's free changes happen outside Glose, between two operations.
			if ops[i].Kind != "track" {
				t.Fatalf("chaîne des empreintes rompue avant %s", ops[i].Kind)
			}
		}
	}
}

func TestDisappearanceIsReported(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Un texte.\n"})
	w.do(review.Author, 0, "comment", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		return review.Comment(s, span(s, "texte"), "x", review.Author, g)
	})
	w.n.Handoff(review.Author, "")
	// The agent deletes the annotation by hand: done refuses.
	w.write("a.md", "Un texte.\n")
	_, err := w.n.Handoff(review.Agent, "")
	var te *TrackError
	if !errors.As(err, &te) || !strings.Contains(te.Problems[0].Message, "supprimée") {
		t.Fatalf("%v", err)
	}
	st, _, _, _ := w.n.State()
	if len(st.Problems) != 1 || !strings.Contains(st.Problems[0], "disparue sans décision") {
		t.Fatalf("%+v", st.Problems)
	}
	// The author takes the hand back, without converting.
	if _, err := w.n.Reclaim("", false); err == nil {
		t.Fatal("reprise acceptée malgré l'anomalie")
	}
	if _, err := w.n.Reclaim("", true); err != nil {
		t.Fatal(err)
	}
	st, _, _, _ = w.n.State()
	if st.Hand != review.Author || !st.Reclaimed {
		t.Fatalf("%+v", st)
	}
}

func TestHandoffGivesIdentityToHandWrittenMarks(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Un {==passage==}{>>écrit dans VS Code<<} ici.\n", "b.md": "Rien.\n"})
	ho, err := w.n.Handoff(review.Author, "")
	if err != nil || ho.Identified != 1 {
		t.Fatalf("%+v %v", ho, err)
	}
	if !strings.Contains(w.read("a.md"), "{>>écrit dans VS Code<<}{>>glose:v1 id=") || noMeta(w.read("a.md")) != "Un {==passage==}{>>écrit dans VS Code<<} ici.\n" {
		t.Fatalf("%s", w.read("a.md"))
	}
	if w.read("b.md") != "Rien.\n" {
		t.Fatal("b.md modifié")
	}
	// The copies of the files are kept.
	_, h, _, _ := w.n.State()
	for _, fh := range h.Last().TurnEv.Files {
		if _, err := journal.LoadSnapshot(w.n.P.State(), fh.Hash); err != nil {
			t.Fatal(err)
		}
	}
}

// P2.5: 50 proposals accepted at once: file as expected, journal complete.
func TestFiftyProposalsAcceptedAtOnce(t *testing.T) {
	var b, want strings.Builder
	for i := range 50 {
		b.WriteString("Le mot ancien numéro " + string(rune('a'+i%26)) + ".\n\n")
		want.WriteString("Le mot nouveau numéro " + string(rune('a'+i%26)) + ".\n\n")
	}
	w := newWorld(t, map[string]string{"a.md": b.String()})
	w.do(review.Author, 0, "propose", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		e, _, err := review.Propose(s, critic.Span{Start: 0, End: len(s)}, want.String(), "", review.Author, g)
		return e, err
	})
	src := []byte(w.read("a.md"))
	if n := Count(src, 0, len(src)); n != 50 {
		t.Fatalf("%d propositions", n)
	}
	w.do(review.Author, 0, "decide", Accept, func(s []byte, _ *review.Gen) (patch.Edit, error) {
		return DecideRange(s, 0, len(s), Accept)
	})
	if got := w.read("a.md"); got != want.String() {
		t.Fatalf("%q", got)
	}
	_, h, _, _ := w.n.State()
	n := 0
	for _, c := range h.Closed() {
		if c.Decision == Accept && c.Kind == "substitution" && c.Text == "ancien" && c.New == "nouveau" {
			n++
		}
	}
	if n != 50 {
		t.Fatalf("%d décisions archivées", n)
	}
}

// Revue Codex, défaut 3 : une reprise forcée ne désactive pas la conversion
// d'une autre remise (plus d'état partagé).
func TestReclaimForceIsLocal(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Un texte.\n"})
	w.n.Handoff(review.Author, "")
	w.write("a.md", "Un autre texte.\n")
	ho, err := w.n.Reclaim("", false)
	if err != nil || ho.Converted != 1 || !strings.Contains(w.read("a.md"), "{++autre ++}") {
		t.Fatalf("%+v %v %q", ho, err, w.read("a.md"))
	}
}

// Revue Codex, défaut 8 : un identifiant de tête effacé, déjà connu du
// journal, est restitué d'après les réponses qui le citent.
func TestErasedIdentityKnownToTheJournalIsRestored(t *testing.T) {
	w := newWorld(t, map[string]string{"a.md": "Un texte.\n"})
	w.do(review.Author, 0, "comment", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		return review.Comment(s, span(s, "texte"), "Pourquoi ?", review.Author, g)
	})
	w.n.Handoff(review.Author, "")
	id := review.Read([]byte(w.read("a.md"))).Annotations[0].ID
	w.do(review.Agent, 0, "reply", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		e, _, err := review.Reply(s, 0, id, "Parce que.", review.Agent, g)
		return e, err
	})
	w.n.Handoff(review.Agent, "")
	// The author erases the technical comment of the head by hand.
	src := w.read("a.md")
	w.write("a.md", strings.Replace(src, "{>>glose:v1 id="+id+" by=author<<}", "", 1))
	if _, err := w.n.Handoff(review.Author, ""); err != nil {
		t.Fatal(err)
	}
	if got := w.read("a.md"); got != src {
		t.Fatalf("identité non restituée :\n%s\n%s", got, src)
	}
	st, _, _, _ := w.n.State()
	if len(st.Problems) != 0 || st.Issues != 0 {
		t.Fatalf("%+v", st)
	}
}

// Notes de bas de page : une note ajoutée par « ^[…] » ou supprimée avec son
// dernier appel forme une seule proposition (référence et définition).
func TestFootnotesAreOneProposal(t *testing.T) {
	src := "Premier paragraphe.\n\nSecond paragraphe[^a] ici.\n\n[^a]: Ancienne note.\n"
	w := newWorld(t, map[string]string{"a.md": src})
	A := review.Author
	var main critic.Span
	w.do(A, 0, "propose", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		e, m, err := review.Propose(s, span(s, "Premier paragraphe."), "Premier paragraphe^[Nouvelle note.].", "", A, g)
		main = m
		return e, err
	})
	got := noMeta(w.read("a.md"))
	if got != "Premier paragraphe{++[^1]++}.\n\nSecond paragraphe[^a] ici.\n\n[^a]: Ancienne note.\n{++\n[^1]: Nouvelle note.\n++}" {
		t.Fatalf("ajout :\n%q", got)
	}
	// Undo of the rewriting (its range only): the definition goes with it.
	w.do(A, 0, "decide", Reject, func(s []byte, _ *review.Gen) (patch.Edit, error) {
		return DecideRange(s, main.Start, main.End, Reject)
	})
	if w.read("a.md") != src {
		t.Fatalf("annuler :\n%q", w.read("a.md"))
	}
	// Delete the reference: the definition is proposed for deletion too.
	w.do(A, 0, "propose", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		e, _, err := review.Propose(s, span(s, "Second paragraphe[^a] ici."), "Second paragraphe ici.", "", A, g)
		return e, err
	})
	got = noMeta(w.read("a.md"))
	if got != "Premier paragraphe.\n\nSecond paragraphe{--[^a]--} ici.{--\n\n[^a]: Ancienne note.\n--}" &&
		got != "Premier paragraphe.\n\nSecond paragraphe{--[^a]--} ici.\n{--\n[^a]: Ancienne note.\n--}" {
		t.Fatalf("suppression :\n%q", got)
	}
	// Accepting the reference's deletion accepts the definition's.
	w.do(A, 0, "decide", Accept, func(s []byte, _ *review.Gen) (patch.Edit, error) {
		return DecideRange(s, strings.Index(string(s), "{--[^a]--}"), 0, Accept)
	})
	if got := w.read("a.md"); got != "Premier paragraphe.\n\nSecond paragraphe ici.\n" {
		t.Fatalf("accepter :\n%q", got)
	}
}

// L'agent efface le dernier appel d'une note : la définition est proposée à
// la suppression avec lui, et les deux se décident ensemble.
func TestAgentDeletesANote(t *testing.T) {
	src := "Un texte[^a] ici.\n\n[^a]: La note.\n"
	w := newWorld(t, map[string]string{"a.md": src})
	w.n.Handoff(review.Author, "")
	w.write("a.md", "Un texte ici.\n\n[^a]: La note.\n")
	if _, err := w.n.Handoff(review.Agent, ""); err != nil {
		t.Fatal(err)
	}
	got := w.read("a.md")
	w.do(review.Author, 0, "decide", Reject, func(s []byte, _ *review.Gen) (patch.Edit, error) {
		return DecideRange(s, strings.Index(string(s), "{--[^a]--}"), 0, Reject)
	})
	if w.read("a.md") != src {
		t.Fatalf("%q\n%q", got, w.read("a.md"))
	}
}

// A proposal written by hand (no identity) and corrected by the author: one
// revision in the journal, no closing (Codex review of lot P3, point 4).
func TestCompareRevisedWithoutIdentity(t *testing.T) {
	before := []byte("Le {~~chat~>chien~~} dort.\n")
	after := []byte("Le {~~chat~>loup~~}{>>glose:v1 id=abcdef by=author<<} dort.\n")
	created, closed := Compare("a.md", before, after, "")
	if len(closed) != 0 || len(created) != 1 {
		t.Fatalf("créés %v, clos %+v", created, closed)
	}
	rev := Revisions("a.md", before, after)
	if len(rev) != 1 || rev[0].New != "chien" || rev[0].ID != "abcdef" {
		t.Fatalf("révisions %+v", rev)
	}
	// A rejected proposal followed by an identified one is closed, not revised.
	b2 := []byte("Le {++gros ++}{++petit ++}{>>glose:v1 id=k3fz9a by=agent<<}chat.\n")
	a2 := []byte("Le {++petit ++}{>>glose:v1 id=k3fz9a by=agent<<}chat.\n")
	if _, closed := Compare("a.md", b2, a2, "reject"); len(closed) != 1 || len(Revisions("a.md", b2, a2)) != 0 {
		t.Fatalf("rejet pris pour une correction : %+v", closed)
	}
	// A proposal that really disappears is still closed.
	if _, closed := Compare("a.md", before, []byte("Le chien dort.\n"), "accept"); len(closed) != 1 {
		t.Fatalf("disparition non vue : %+v", closed)
	}
}

// A file replaced, after the project was loaded, by a link to an identical
// copy outside the project is not written through the link.
func TestFileReplacedByALinkToTheOutsideIsNotWritten(t *testing.T) {
	src := "Le contrat.\n"
	w := newWorld(t, map[string]string{"a.md": src})
	outside := filepath.Join(t.TempDir(), "copie.md")
	if err := os.WriteFile(outside, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(w.dir, "a.md")
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, a); err != nil {
		t.Skip("liens symboliques indisponibles :", err)
	}
	_, err := w.n.Do(review.Author, 0, "", "comment", "", func(s []byte, g *review.Gen) (patch.Edit, error) {
		return review.Comment(s, span(s, "contrat"), "Lequel ?", review.Author, g)
	})
	if err == nil {
		t.Fatal("écriture acceptée à travers un lien vers l'extérieur")
	}
	if b, _ := os.ReadFile(outside); string(b) != src {
		t.Fatalf("copie extérieure modifiée : %q", b)
	}
}
