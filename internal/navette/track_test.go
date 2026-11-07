package navette

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
)

// rejectAll rejects every proposal of src, one after the other.
func rejectAll(t *testing.T, src []byte) []byte {
	t.Helper()
	for {
		marks, _ := critic.Parse(src)
		var target *critic.Annotation
		for _, a := range critic.Group(marks) {
			if k := marks[a.Main].Kind; k == critic.Addition || k == critic.Deletion || k == critic.Substitution {
				a := a
				target = &a
				break
			}
		}
		if target == nil {
			return src
		}
		e, err := patch.Reject(src, marks, *target)
		if err != nil {
			t.Fatal(err)
		}
		src = e.Apply(src)
	}
}

func track(t *testing.T, base, cur string, known ...string) (string, int, []Problem) {
	t.Helper()
	k := map[string]bool{}
	for _, id := range known {
		k[id] = true
	}
	out, n, pb := Track("a.md", []byte(base), []byte(cur), k, nil)
	return string(out), n, pb
}

// P2.6: the agent rewrites three paragraphs and deletes one: four groups of
// proposals; rejecting them all gives back the original, byte for byte.
func TestTrackFreeRewriting(t *testing.T) {
	base := "# Titre\r\n\r\nLe premier paragraphe est assez court.\r\n\r\nLe deuxième paragraphe dit une chose.\r\n\r\n" +
		"Un paragraphe à supprimer.\r\n\r\nLe troisième paragraphe est inchangé.\r\n\r\nLe dernier paragraphe conclut.\r\n"
	cur := "# Titre\r\n\r\nLe premier paragraphe est bref.\r\n\r\nLe deuxième paragraphe dit autre chose, et plus.\r\n\r\n" +
		"Le troisième paragraphe est inchangé.\r\n\r\nLe dernier paragraphe, enfin, conclut.\r\n"
	out, n, pb := track(t, base, cur)
	if len(pb) != 0 {
		t.Fatal(pb)
	}
	if n < 4 {
		t.Fatalf("%d propositions :\n%s", n, out)
	}
	if got := rejectAll(t, []byte(out)); !bytes.Equal(got, []byte(base)) {
		t.Fatalf("tout rejeter :\n%q\n%q", got, base)
	}
	if got := projection.Project([]byte(out), projection.Proposed).Text; !bytes.Equal(got, []byte(cur)) {
		t.Fatalf("tout accepter :\n%q", got)
	}
	want := "Le premier paragraphe est {~~assez court~>bref~~}."
	if !strings.Contains(out, want) {
		t.Fatalf("%s", out)
	}
}

func TestTrackMarksOfTheAgent(t *testing.T) {
	base := "Un {==passage==}{>>pourquoi ?<<}{>>glose:v1 id=aaaaaa by=author<<} et une phrase.\n"
	// A reply in the thread, a comment, a proposal written by hand.
	cur := "Un {==passage==}{>>pourquoi ?<<}{>>glose:v1 id=aaaaaa by=author<<}{>>parce que.<<} et une {~~phrase~>proposition~~}.{>>Voilà.<<}\n"
	out, n, pb := track(t, base, cur)
	if len(pb) != 0 || n != 0 || out != cur {
		t.Fatalf("%q %d %v", out, n, pb)
	}
	// Technical comments written by Glose for the agent (glose propose) are
	// kept; one written by hand is refused.
	cur2 := "Un {==passage==}{>>pourquoi ?<<}{>>glose:v1 id=aaaaaa by=author<<} et une {~~phrase~>proposition~~}{>>glose:v1 id=bbbbbb by=agent<<}.\n"
	if out, _, pb := track(t, base, cur2, "bbbbbb"); len(pb) != 0 || out != cur2 {
		t.Fatalf("connu : %q %v", out, pb)
	}
	if _, _, pb := track(t, base, cur2); len(pb) != 1 || !strings.Contains(pb[0].Message, "à la main") {
		t.Fatalf("inconnu : %v", pb)
	}
}

func TestTrackAnomalies(t *testing.T) {
	base := "Avant {~~a~>b~~}{>>glose:v1 id=aaaaaa by=author<<} milieu {>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin du texte.\n"
	cases := map[string]string{
		// (Changing what a proposal proposes is a correction, not an anomaly:
		// TestTrackCorrectionOfAProposal. Changing its original text is one.)
		"marque modifiée":  "Avant {~~x~>b~~}{>>glose:v1 id=aaaaaa by=author<<} milieu {>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin du texte.\n",
		"marque supprimée": "Avant b milieu {>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin du texte.\n",
		"identité effacée": "Avant {~~a~>b~~} milieu {>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin du texte.\n",
		"mélange":          "Avant {~~a~>b~~}{>>glose:v1 id=aaaaaa by=author<<} centre{>>note<<} {>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin du texte.\n",
		"collée":           "Avant {~~a~>b~~}{>>glose:v1 id=aaaaaa by=author<<} centre{>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin du texte.\n",
		"mal formée":       "Avant {~~a~>b~~}{>>glose:v1 id=aaaaaa by=author<<} milieu {>>remarque<<}{>>glose:v1 id=cccccc by=author<<} fin {++du texte.\n",
	}
	base2 := strings.Replace(base, " milieu {>>", " milieu{>>", 1)
	for name, cur := range cases {
		b := base
		if name == "collée" {
			b = base2
		}
		out, _, pb := track(t, b, cur)
		if len(pb) == 0 {
			t.Errorf("%s : accepté\n%s", name, out)
		}
	}
	// The same changes away from the marks are converted.
	cur := strings.Replace(base, "fin du texte", "fin de ce texte", 1)
	if out, n, pb := track(t, base, cur); len(pb) != 0 || n != 1 || !strings.Contains(out, "fin {~~du~>de ce~~} texte") {
		t.Fatalf("%q %d %v", out, n, pb)
	}
}

func TestTrackDelimitersAndCode(t *testing.T) {
	base := "Lancer `glose open` puis lire.\n"
	cur := "Lancer `glose open --json` puis lire.\n"
	out, n, pb := track(t, base, cur)
	if len(pb) != 0 || n != 1 || out != "Lancer {~~`glose open`~>`glose open --json`~~} puis lire.\n" {
		t.Fatalf("%q %d %v", out, n, pb)
	}
	if _, _, pb := track(t, base, "Lancer `glose open` puis <<} lire.\n"); len(pb) == 0 {
		t.Fatal("délimiteur accepté")
	}
}

func TestMyersAgainstRandomTexts(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	words := []string{"le", "la", "droit", "civil", " ", " ", "\n", ".", ",", "é", "contrat", "\n\n", "**", "_"}
	gen := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(words[r.Intn(len(words))])
		}
		return b.String()
	}
	for i := 0; i < 2000; i++ {
		base := gen(r.Intn(60))
		cur := base
		// a few random edits
		for range r.Intn(5) {
			if len(cur) == 0 {
				cur = gen(3)
				continue
			}
			p := r.Intn(len(cur) + 1)
			for p < len(cur) && (cur[p]&0xC0) == 0x80 {
				p++
			}
			q := p + r.Intn(8)
			if q > len(cur) {
				q = len(cur)
			}
			for q < len(cur) && (cur[q]&0xC0) == 0x80 {
				q++
			}
			cur = cur[:p] + gen(r.Intn(3)) + cur[q:]
		}
		out, _, pb := track(t, base, cur)
		if len(pb) != 0 {
			// Only a change glued to nothing can fail here (no marks).
			t.Fatalf("%q → %q : %v", base, cur, pb)
		}
		if got := projection.Project([]byte(out), projection.Current).Text; string(got) != base {
			t.Fatalf("actuel %q ≠ %q", got, base)
		}
		if got := projection.Project([]byte(out), projection.Proposed).Text; string(got) != cur {
			t.Fatalf("proposé %q ≠ %q", got, cur)
		}
	}
}

func FuzzTrack(f *testing.F) {
	f.Add("Le chat dort.", "Le chien dort bien.")
	f.Add("a {++b++} c", "a {++b++} d")
	f.Fuzz(func(t *testing.T, base, cur string) {
		out, _, pb := Track("a.md", []byte(base), []byte(cur), nil, nil)
		if len(pb) > 0 {
			return
		}
		if !bytes.Equal(projection.Project(out, projection.Current).Text, projection.Project([]byte(base), projection.Current).Text) ||
			!bytes.Equal(projection.Project(out, projection.Proposed).Text, projection.Project([]byte(cur), projection.Proposed).Text) {
			t.Fatalf("%q", out)
		}
	})
}

// Revue Codex, défaut 1 : une proposition écrite à la main par l'agent juste
// avant un commentaire existant le prendrait pour réponse.
func TestTrackHandWrittenProposalCannotStealAComment(t *testing.T) {
	base := "x{>>note<<}{>>glose:v1 id=aaaaaa by=author<<}\n"
	cur := "{~~x~>y~~}{>>note<<}{>>glose:v1 id=aaaaaa by=author<<}\n"
	if out, _, pb := track(t, base, cur); len(pb) == 0 {
		t.Fatalf("accepté : %q", out)
	}
	// Same with a reply in a thread: it stays with its head.
	base = "Un {==mot==}{>>a<<}{>>glose:v1 id=aaaaaa by=author<<} ici.\n"
	cur = "Un {==mot==}{>>a<<}{>>glose:v1 id=aaaaaa by=author<<}{>>b<<} ici.\n"
	if out, _, pb := track(t, base, cur); len(pb) != 0 || out != cur {
		t.Fatalf("réponse : %q %v", out, pb)
	}
}

// Revue Codex, défaut 2 : une marque écrite à la main dans un mot ne fait pas
// paniquer Track.
func TestTrackMarkInsideAWord(t *testing.T) {
	out, _, pb := track(t, "Le mot ab ici.\n", "Le mot {==a==}b ici.\n")
	if len(pb) != 0 || out != "Le mot {==a==}b ici.\n" {
		t.Fatalf("%q %v", out, pb)
	}
}

// Essai réel : deux changements séparés par une espace forment une seule
// proposition.
func TestTrackMergesChangesAcrossSpaces(t *testing.T) {
	out, n, pb := track(t, "Le site `https://markdown.org/` en présente la syntaxe.\n",
		"La spécification `https://spec.commonmark.org/` en présente la syntaxe.\n")
	if len(pb) != 0 || n != 1 || !strings.HasPrefix(out, "{~~Le site `https://markdown.org/`~>La spécification `https://spec.commonmark.org/`~~} en") {
		t.Fatalf("%q %d %v", out, n, pb)
	}
}

// Essai réel : un ajout à la fin d'un paragraphe qui porte un commentaire va
// après le fil, sans le lui prendre.
func TestTrackAdditionGoesAfterTheThread(t *testing.T) {
	base := "Fin du texte.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<}\n\nAnnexes.\n"
	cur := "Fin du texte. Voici la transition.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<}\n\nAnnexes.\n"
	out, n, pb := track(t, base, cur)
	want := "Fin du texte.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<}{++ Voici la transition.++}\n\nAnnexes.\n"
	if len(pb) != 0 || n != 1 || out != want {
		t.Fatalf("%q %d %v", out, n, pb)
	}
	// Written after the thread, the same.
	cur2 := "Fin du texte.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<} Voici la transition.\n\nAnnexes.\n"
	if out, _, pb := track(t, base, cur2); len(pb) != 0 || out != want {
		t.Fatalf("après : %q %v", out, pb)
	}
}

// Contre-vérification Codex : un ajout déplacé passe après tout le fil, y
// compris une réponse ajoutée pendant le tour.
func TestTrackAdditionGoesAfterNewReplies(t *testing.T) {
	out, n, pb := track(t, "Fin.{>>note<<}\n", "Fin. suite{>>note<<}{>>réponse<<}\n")
	if len(pb) != 0 || n != 1 || out != "Fin.{>>note<<}{>>réponse<<}{++ suite++}\n" {
		t.Fatalf("%q %d %v", out, n, pb)
	}
}

// Second essai réel : une réponse par glose reply, puis du texte libre écrit
// après le fil.
func TestTrackReplyThenFreeText(t *testing.T) {
	base := "figurer.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<}\n"
	cur := "figurer.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<}{>>Voici.<<}{>>glose:v1 id=rrrrrr re=aaaaaa by=agent<<} Transition.\n"
	out, n, pb := track(t, base, cur, "rrrrrr")
	want := "figurer.{>>Une transition ?<<}{>>glose:v1 id=aaaaaa by=author<<}{>>Voici.<<}{>>glose:v1 id=rrrrrr re=aaaaaa by=agent<<}{++ Transition.++}\n"
	if len(pb) != 0 || n != 1 || out != want {
		t.Fatalf("%q %d %v", out, n, pb)
	}
}

// D-26 : l'agent réécrit le passage surligné par l'auteur ; la proposition
// reprend le fil, et l'identité de l'annotation.
func TestTrackRewrittenHighlight(t *testing.T) {
	base := "Un {==mot juste==}{>>Corrige.<<}{>>glose:v1 id=aaaaaa by=author<<} ici.\n"
	cur := "Un {==terme exact==}{>>Corrige.<<}{>>glose:v1 id=aaaaaa by=author<<}{>>Fait.<<}{>>glose:v1 id=rrrrrr re=aaaaaa by=agent<<} ici.\n"
	out, n, pb := track(t, base, cur, "rrrrrr")
	if len(pb) != 0 || n != 1 {
		t.Fatalf("%q %d %v", out, n, pb)
	}
	if !strings.HasPrefix(out, "Un {~~mot juste~>terme exact~~}{>>glose:v1 id=aaaaaa by=agent<<}{>>Corrige.<<}{>>glose:v1 id=") ||
		!strings.HasSuffix(out, " re=aaaaaa by=author<<}{>>Fait.<<}{>>glose:v1 id=rrrrrr re=aaaaaa by=agent<<} ici.\n") {
		t.Fatalf("%q", out)
	}
	d := review.Read([]byte(out))
	a := d.Annotations[0]
	if len(d.Issues) != 0 || a.ID != "aaaaaa" || a.By != review.Agent || a.Kind != critic.Substitution ||
		len(a.Messages) != 2 || a.Messages[0].By != review.Author || a.Messages[1].ID != "rrrrrr" {
		t.Fatalf("%+v %+v", a, d.Issues)
	}
	// Rejecting the proposal gives back the text; the thread goes with it.
	if got := rejectAll(t, []byte(out)); string(got) != "Un mot juste ici.\n" {
		t.Fatalf("%q", got)
	}
}

func TestTrackMergesAcrossAShortWord(t *testing.T) {
	out, n, pb := track(t, "Le chat de la voisine dort.\n", "Un chien de la voisine dort.\n")
	if len(pb) != 0 || n != 1 || out != "{~~Le chat~>Un chien~~} de la voisine dort.\n" {
		t.Fatalf("%q %d %v", out, n, pb)
	}
	out, n, _ = track(t, "Couche qui, en sortie, donne.\n", "Couche : en sortie, il donne.\n")
	if n > 2 {
		t.Fatalf("%q %d", out, n)
	}
}

// The agent rewrites what a proposal of the author proposes: the proposal
// stays as it was, and its thread ends with a correction (Meta.Fix) that the
// author may apply. His own proposal, he corrects in place.
func TestTrackCorrectionOfAProposal(t *testing.T) {
	base := "Le principe{++ du fait d'autrui++}{>>glose:v1 id=abcdef by=author<<}{>>À vérifier ?<<}{>>glose:v1 id=ghjkmn re=abcdef by=author<<} est général.\n"
	cur := "Le principe{++ du fait des choses++}{>>glose:v1 id=abcdef by=author<<}{>>À vérifier ?<<}{>>glose:v1 id=ghjkmn re=abcdef by=author<<} est général.\n"
	out, n, pb := track(t, base, cur, "abcdef", "ghjkmn")
	if len(pb) > 0 || n != 1 {
		t.Fatalf("problèmes %v, %d conversions", pb, n)
	}
	d := review.Read([]byte(out))
	if len(d.Annotations) != 1 || string(d.Src[d.Annotations[0].Mark.Text.Start:d.Annotations[0].Mark.Text.End]) != " du fait d'autrui" {
		t.Fatalf("proposition de l'auteur changée : %q", out)
	}
	msgs := d.Annotations[0].Replies()
	last := msgs[len(msgs)-1]
	if !last.Fix || last.By != review.Agent || string(d.Src[last.Mark.Text.Start:last.Mark.Text.End]) != " du fait des choses" {
		t.Fatalf("correction : %+v dans %q", last, out)
	}
	// His own proposal: corrected in place, thread kept.
	base2 := "Le principe{++ du fait d'autrui++}{>>glose:v1 id=abcdef by=agent<<} est général.\n"
	cur2 := "Le principe{++ du fait des choses++}{>>glose:v1 id=abcdef by=agent<<} est général.\n"
	out2, _, pb2 := track(t, base2, cur2, "abcdef")
	if len(pb2) > 0 || out2 != cur2 {
		t.Fatalf("sa propre proposition : %v %q", pb2, out2)
	}
	// The original text of the proposal changed: still an anomaly.
	cur3 := "Le principe{~~ du~> au~~}{>>glose:v1 id=abcdef by=author<<}{>>À vérifier ?<<}{>>glose:v1 id=ghjkmn re=abcdef by=author<<} est général.\n"
	if _, _, pb3 := track(t, base, cur3, "abcdef", "ghjkmn"); len(pb3) == 0 {
		t.Fatal("changement de nature de la proposition accepté")
	}
}

// Navette d'essai du 7 novembre 2026 : « glose propose » écrit des
// propositions mot à mot que la conversion prenait pour des modifications
// libres mêlées de marques (l'alignement des espaces était ambigu). Un
// paragraphe auquel l'agent n'a qu'ajouté des marques est repris tel quel.
func TestTrackProposalsOfACommand(t *testing.T) {
	base := "Avant de présenter les décisions essentielles, il convient de s'arrêter le principe, et l'histoire qui l'a vue naître.\n"
	gen := review.NewGen(nil)
	e, _, err := review.Propose([]byte(base), critic.Span{Start: 0, End: len(base) - 1},
		"Avant d'en venir aux décisions, il faut présenter le principe et l'histoire qui l'a vu naître.", "Pourquoi.", review.Agent, gen)
	if err != nil {
		t.Fatal(err)
	}
	cur := string(e.Apply([]byte(base)))
	known := review.Read([]byte(cur)).IDs()
	out, _, pb := track(t, base, cur, known...)
	if len(pb) > 0 || out != cur {
		t.Fatalf("problèmes %v\n%q", pb, out)
	}
}

// Codex review (phase 4): an existing annotation moved to another place of
// the same paragraph, with the view « Current » unchanged, is a change of an
// annotation: refused, never taken as it is.
func TestTrackRefusesAMovedAnnotation(t *testing.T) {
	base := "{==mot==}{>>remarque<<}{>>glose:v1 id=abcdef by=author<<} et mot.\n"
	for _, cur := range []string{
		"mot et {==mot==}{>>remarque<<}{>>glose:v1 id=abcdef by=author<<}.\n",
	} {
		out, _, pb := track(t, base, cur, "abcdef")
		if len(pb) == 0 {
			t.Errorf("déplacement accepté :\n%s", out)
		}
	}
	// Annotating around an existing mark is still allowed.
	cur := "{==mot==}{>>remarque<<}{>>glose:v1 id=abcdef by=author<<} et {==mot==}{>>aussi<<}.\n"
	out, _, pb := track(t, base, cur, "abcdef")
	if len(pb) != 0 || out != cur {
		t.Fatalf("%v\n%s", pb, out)
	}
}

// Codex review (phase 4): « ∅ » alone is how a correction says « delete »;
// the agent cannot propose this symbol alone, else accepting it would
// delete the passage.
func TestTrackCorrectionReducedToTheEmptySetSymbol(t *testing.T) {
	base := "Un {~~ensemble~>vide~~}{>>glose:v1 id=abcdef by=author<<} noté.\n"
	cur := "Un {~~ensemble~>∅~~}{>>glose:v1 id=abcdef by=author<<} noté.\n"
	if out, _, pb := track(t, base, cur, "abcdef"); len(pb) == 0 {
		t.Fatalf("correction « ∅ » acceptée : %q", out)
	}
	src := []byte(base)
	if _, _, err := review.Fix(src, "abcdef", "∅", review.Agent, review.NewGen(nil)); err != review.ErrFixSymbol {
		t.Fatalf("review.Fix : %v", err)
	}
	// An empty correction is still a deletion, marked « ∅ » in the thread.
	e, _, err := review.Fix(src, "abcdef", "", review.Agent, review.NewGen(nil))
	if err != nil {
		t.Fatal(err)
	}
	d := review.Read(e.Apply(src))
	msgs := d.Annotations[0].Replies()
	last := msgs[len(msgs)-1]
	if !last.Fix || review.FixText(d.Src[last.Mark.Text.Start:last.Mark.Text.End]) != "" {
		t.Fatalf("%q", e.Apply(src))
	}
}
