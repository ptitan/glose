package notes

import (
	"reflect"
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	got, defs := Expand("Un mot^[Sa note, avec [crochets] et `^[code]`.] et un autre^[Seconde.] ; `^[pas une note]`.", map[string]bool{"1": true})
	if got != "Un mot[^2] et un autre[^3] ; `^[pas une note]`." {
		t.Fatalf("%q", got)
	}
	want := []Def{{"2", "Sa note, avec [crochets] et `^[code]`."}, {"3", "Seconde."}}
	if !reflect.DeepEqual(defs, want) {
		t.Fatalf("%+v", defs)
	}
	if got, defs := Expand(`Échappé \^[non] et vide ^[ ] et ouvert ^[sans fin`, nil); len(defs) != 0 || got != `Échappé \^[non] et vide ^[ ] et ouvert ^[sans fin` {
		t.Fatalf("%q %v", got, defs)
	}
}

func TestAppend(t *testing.T) {
	defs := []Def{{"1", "Une."}, {"2", "Deux."}}
	cases := map[string]string{
		"Texte.\n":   "\n[^1]: Une.\n\n[^2]: Deux.\n",
		"Texte.":     "\n\n[^1]: Une.\n\n[^2]: Deux.\n",
		"Texte.\n\n": "[^1]: Une.\n\n[^2]: Deux.\n",
		"Texte.\r\n": "\r\n[^1]: Une.\r\n\r\n[^2]: Deux.\r\n",
	}
	for src, want := range cases {
		if got := string(Append([]byte(src), defs)); got != want {
			t.Errorf("%q : %q", src, got)
		}
	}
}

func TestDefinition(t *testing.T) {
	src := "Texte[^a] ici.\n\n[^a]: La note,\nsur deux lignes.\n\n    Second paragraphe de la note.\n\n[^b]: Autre.\n\nFin.\n"
	s, ok := Definition([]byte(src), "a")
	if !ok || src[:s.Start]+src[s.End:] != "Texte[^a] ici.\n\n[^b]: Autre.\n\nFin.\n" {
		t.Fatalf("%v %q", ok, src[s.Start:s.End])
	}
	s, ok = Definition([]byte(src), "b")
	if !ok || src[:s.Start]+src[s.End:] != strings.Replace(src, "\n[^b]: Autre.\n", "", 1) {
		t.Fatalf("%q", src[s.Start:s.End])
	}
	if _, ok := Definition([]byte("{==[^a]: annotée==}\n"), "a"); ok {
		t.Fatal("définition annotée acceptée")
	}
}

func TestLinks(t *testing.T) {
	src := "Un mot{++[^1]++} et{--[^a]--} ici{++ seul++}.\n\n{--[^a]: Ancienne.\n--}{++\n[^1]: Nouvelle.\n++}"
	l := Links([]byte(src))
	add, del := strings.Index(src, "{++[^1]"), strings.Index(src, "{--[^a]--}")
	addDef, delDef := strings.Index(src, "{++\n[^1]"), strings.Index(src, "{--[^a]:")
	if !l[add].Primary || !reflect.DeepEqual(l[add].Partners, []int{addDef}) || l[add].Note != "Nouvelle." {
		t.Fatalf("ajout : %+v", l[add])
	}
	if l[addDef].Primary || !reflect.DeepEqual(l[addDef].Partners, []int{add}) {
		t.Fatalf("définition ajoutée : %+v", l[addDef])
	}
	if !l[del].Primary || !reflect.DeepEqual(l[del].Partners, []int{delDef}) || l[del].Note != "Ancienne." {
		t.Fatalf("suppression : %+v", l[del])
	}
	if _, ok := l[strings.Index(src, "{++ seul")]; ok {
		t.Fatal("proposition sans note liée")
	}
}

// Revue Codex du 6 novembre 2026 (notes).
func TestDefinitionStopsAtABlock(t *testing.T) {
	src := "[^a]: Note.\n# Titre\nSuite.\n"
	s, ok := Definition([]byte(src), "a")
	if !ok || src[s.Start:s.End] != "[^a]: Note.\n" {
		t.Fatalf("%q", src[s.Start:s.End])
	}
	// A long note: a paragraph starting with a reference, code inside.
	src = "[^a]: Note avec `code`.\n\n    [^b] suite de la note.\n\nAprès.\n"
	s, ok = Definition([]byte(src), "a")
	if !ok || src[s.End:] != "\nAprès.\n" {
		t.Fatalf("%v %q", ok, src[s.Start:s.End])
	}
}

func TestLabelsIgnoreCodeAndCase(t *testing.T) {
	if r := Refs([]byte("Voir `[^a]` et \\[^b] mais [^C].")); len(r) != 1 || !r["c"] {
		t.Fatalf("%v", r)
	}
	src := "Texte[^A].\n\n[^a]: Note.\n"
	if _, ok := Definition([]byte(src), "A"); !ok {
		t.Fatal("étiquette en majuscules")
	}
}

// Définitions dans un conteneur (comme pandoc) ; appel suivi de « : » au
// milieu d'une ligne.
func TestDefinitionsInContainers(t *testing.T) {
	for _, src := range []string{"> [^a]: Note.\n", "- [^a]: Note.\n", "1. [^a]: Note.\n", "- x\n\n    [^a]: Note.\n", "   [^a]: Note.\r\n"} {
		if !Defs([]byte(src))["a"] || Refs([]byte(src))["a"] {
			t.Errorf("%q : définition non reconnue", src)
		}
	}
	if src := "A. B[^a]: suite\n"; Defs([]byte(src))["a"] || !Refs([]byte(src))["a"] {
		t.Errorf("%q : appel pris pour une définition", src)
	}
}
