package typo

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/sourcemap"
)

func certified(src []byte) *sourcemap.Map {
	m := &sourcemap.Map{}
	b := m.Add(sourcemap.Item{Kind: sourcemap.Block, Start: 0, End: len(src), Parent: -1})
	m.Add(sourcemap.Item{Kind: sourcemap.Word, Start: 0, End: len(src), Parent: b, Certified: true})
	return m
}
func TestDefaultRules(t *testing.T) {
	examples := map[string][2]string{
		"semicolon": {"Bonjour ;", "Bonjour\u00a0;"}, "colon": {"Voici :", "Voici\u00a0:"}, "exclamation": {"Bonjour !", "Bonjour\u00a0!"}, "question": {"Vraiment ?", "Vraiment\u00a0?"},
		"closing-quote": {"« Bonjour »", "« Bonjour\u00a0»"}, "opening-quote": {"« Bonjour »", "«\u00a0Bonjour »"}, "paired-quotes": {"Il dit \"Bonjour\".", "Il dit «\u00a0Bonjour\u00a0»."}, "footnote": {"Texte [^1]", "Texte[^1]"}, "article": {"art. 12", "art.\u00a012"}, "number": {"n° 12", "n°\u00a012"}, "ellipsis": {"Voilà...", "Voilà…"},
	}
	for _, r := range Defaults() {
		t.Run(r.ID, func(t *testing.T) {
			ex, ok := examples[r.ID]
			if !ok {
				t.Fatal("règle sans exemples")
			}
			src := []byte(ex[0])
			cs, e := Find(src, certified(src), []Rule{r})
			if e != nil || len(cs) != 1 {
				t.Fatalf("%v %v", cs, e)
			}
			edit, e := Propose(src, cs, review.NewGen(nil))
			if e != nil {
				t.Fatal(e)
			}
			out := edit.Apply(src)
			if !bytes.Equal(projection.Project(out, projection.Current).Text, src) || string(projection.Project(out, projection.Proposed).Text) != ex[1] {
				t.Fatalf("%s", out)
			}
			if review.Read(out).Annotations[0].Via != "Typo" {
				t.Fatal("signature")
			}
			neg := []byte(ex[1])
			cs, e = Find(neg, certified(neg), []Rule{r})
			if e != nil || len(cs) != 0 {
				t.Fatalf("déjà corrigé : %v %v", cs, e)
			}
			r.Disabled = true
			cs, _ = Find(src, certified(src), []Rule{r})
			if len(cs) != 0 {
				t.Fatal("désactivée")
			}
		})
	}
}
func TestExclusions(t *testing.T) {
	for _, s := range []string{"`Texte...`", "`\"Texte\"`", "[\"Texte\"](cible)", "```md\nTexte...\n```", "~~~\nTexte...\n~~~", "    Texte...", "\tTexte...", "[Texte...](cible...)", "[Texte...][ref]", "[Texte...]", "<https://texte...>", "\\macro{Texte...}", "$Texte...$", "Texte {#texte...}", "---\ntitle: Texte...\n---\n", "+++\ntitle = 'Texte...'\n+++\n", "{~~Texte...~>Suite...~~}", "{>>Texte...<<}", "« Texte... »", "\"Texte...\"", "“Texte...”", "> Texte...", "\"sans fin...", "Texte \"douteux", "5\"...", "Texte...."} {
		t.Run(s, func(t *testing.T) {
			src := []byte(s)
			r := Defaults()
			if s == "\"Texte...\"" {
				r = append(r[:6], r[7:]...)
			}
			cs, e := Find(src, certified(src), r)
			if e != nil || len(cs) != 0 {
				t.Fatalf("%v %v", cs, e)
			}
		})
	}
	src := []byte("Bonjour !")
	if cs, _ := Find(src, nil, Defaults()); len(cs) != 0 {
		t.Fatal("sans certification")
	}
}
func TestLoadPreservesAuthorBytes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GLOSE_CONFIG_DIR", dir)
	r, e := Load()
	if e != nil || len(r) != len(Defaults()) {
		t.Fatal(r, e)
	}
	path := filepath.Join(dir, "typo.json")
	b, _ := os.ReadFile(path)
	expected, _ := json.MarshalIndent(Defaults(), "", "  ")
	if !bytes.Equal(b, expected) {
		t.Fatal("octets des règles initiales")
	}
	var got []Rule
	if json.Unmarshal(b, &got) != nil {
		t.Fatal("JSON")
	}
	custom := append([]byte(" \r\n"), b...)
	os.WriteFile(path, custom, 0600)
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, custom) {
		t.Fatal("règles réécrites")
	}
	os.WriteFile(path, []byte("invalide"), 0600)
	if _, e := Load(); e == nil {
		t.Fatal("corruption ignorée")
	}
}
func FuzzCorrections(f *testing.F) {
	for _, s := range []string{"\ufeffTexte...\r\n", "art. 12 ; n° 3 !", "Un \"texte\"."} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		src := []byte(s)
		cs, e := Find(src, certified(src), Defaults())
		if e != nil {
			return
		}
		p, e := Propose(src, cs, review.NewGen(nil))
		if e != nil {
			return
		}
		if !bytes.Equal(projection.Project(p.Apply(src), projection.Current).Text, projection.Project(src, projection.Current).Text) {
			t.Fatal("octets altérés")
		}
		for _, c := range cs {
			if c.Start < 0 || c.End > len(src) || c.Start > c.End {
				t.Fatal("bornes")
			}
		}
	})
}
func TestProposeFidelity(t *testing.T) {
	src := []byte("\ufeffAvant\r\nTexte...\nAprès\r\n")
	at := bytes.Index(src, []byte("..."))
	cs := []Correction{{"ellipsis", at, at + 3, "…"}}
	p, e := Propose(src, cs, review.NewGen(nil))
	if e != nil {
		t.Fatal(e)
	}
	out := p.Apply(src)
	if !bytes.Equal(projection.Project(out, projection.Current).Text, src) || !bytes.Equal(src, []byte("\ufeffAvant\r\nTexte...\nAprès\r\n")) {
		t.Fatal("fidélité")
	}
	want := bytes.Replace(src, []byte("..."), []byte("…"), 1)
	if !bytes.Equal(projection.Project(out, projection.Proposed).Text, want) {
		t.Fatal("autres octets changés")
	}
}

func TestQuoteSpacingWithoutSpacesAndAmbiguities(t *testing.T) {
	src := []byte("«Bonjour»")
	cs, e := Find(src, certified(src), Defaults())
	if e != nil || len(cs) != 2 {
		t.Fatal(cs, e)
	}
	p, e := Propose(src, cs, review.NewGen(nil))
	if e != nil {
		t.Fatal(e)
	}
	if got := string(projection.Project(p.Apply(src), projection.Proposed).Text); got != "«\u00a0Bonjour\u00a0»" {
		t.Fatal(got)
	}
	for _, s := range []string{"2:30", "12:30", `« Une "citation" »`, `"Un" et "deux"`, `5" de long`, `Il dit "sans fin`} {
		src := []byte(s)
		for _, r := range Defaults() {
			if r.ID != "paired-quotes" && r.ID != "colon" {
				continue
			}
			cs, e := Find(src, certified(src), []Rule{r})
			if e != nil || len(cs) != 0 {
				t.Fatal(s, cs, e)
			}
		}
	}
}

// Audit A7, A8, A11: an existing Unicode space is kept, bare addresses are
// left alone, and corrections that meet become one proposal.
func TestAuditSpacesAddressesAndNeighbours(t *testing.T) {
	for _, s := range []string{
		"Un mot\u202f; puis\u202f! et\u202f? «\u202fici\u202f»",
		"Un mot\u2009; un\u00a0: deux",
		"Voir https://exemple.fr/page?x=1&y=2 ou www.exemple.fr/a?b pour plus",
		"Écrire à mailto:nom@exemple.fr?subject=x maintenant",
		"Le dossier C:\\Users\\a?b reste",
	} {
		src := []byte(s)
		if cs, err := Find(src, certified(src), Defaults()); err != nil || len(cs) != 0 {
			t.Errorf("%q : %+v %v", s, cs, err)
		}
	}
	// The address is left alone, not the sentence around it.
	src := []byte("Voir https://exemple.fr/?q=1 ! Vraiment ?")
	cs, _ := Find(src, certified(src), Defaults())
	if len(cs) != 2 || cs[0].Start <= bytes.Index(src, []byte("1")) {
		t.Fatalf("phrase autour de l'adresse : %+v", cs)
	}
	src = []byte("Bien...!")
	cs, _ = Find(src, certified(src), Defaults())
	if len(cs) != 1 || cs[0].Text != "…\u00a0" || cs[0].Start != 4 || cs[0].End != 7 {
		t.Fatalf("corrections contiguës : %+v", cs)
	}
	edit, err := Propose(src, cs, review.NewGen(nil))
	if err != nil {
		t.Fatal(err)
	}
	out := edit.Apply(src)
	if got := string(projection.Project(out, projection.Proposed).Text); got != "Bien…\u00a0!" {
		t.Fatalf("proposé : %q", got)
	}
	if !bytes.Equal(projection.Project(out, projection.Current).Text, src) {
		t.Fatal("original changé")
	}
}
