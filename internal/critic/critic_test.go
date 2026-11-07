package critic

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func text(src string, s Span) string { return src[s.Start:s.End] }

func TestParseRecognisesTheFiveMarks(t *testing.T) {
	src := "a {++ajout++} b {--retrait--} c {~~ancien~>nouveau~~} d {>>remarque<<} e {==passage==}"
	marks, issues := Parse([]byte(src))
	if len(issues) != 0 {
		t.Fatalf("problèmes inattendus : %v", issues)
	}
	want := []struct {
		kind      Kind
		whole     string
		text, new string
	}{
		{Addition, "{++ajout++}", "ajout", ""},
		{Deletion, "{--retrait--}", "retrait", ""},
		{Substitution, "{~~ancien~>nouveau~~}", "ancien", "nouveau"},
		{Comment, "{>>remarque<<}", "remarque", ""},
		{Highlight, "{==passage==}", "passage", ""},
	}
	if len(marks) != len(want) {
		t.Fatalf("%d marques, attendu %d : %+v", len(marks), len(want), marks)
	}
	for i, w := range want {
		m := marks[i]
		if m.Kind != w.kind || text(src, m.Span) != w.whole || text(src, m.Text) != w.text || text(src, m.New) != w.new {
			t.Errorf("marque %d = %v %q %q %q ; attendu %v %q %q %q", i, m.Kind,
				text(src, m.Span), text(src, m.Text), text(src, m.New), w.kind, w.whole, w.text, w.new)
		}
	}
}

func TestParseOffsetsAreBytes(t *testing.T) {
	src := "été {>>ça<<}"
	marks, _ := Parse([]byte(src))
	if len(marks) != 1 || marks[0].Span != (Span{6, 15}) || marks[0].Text != (Span{9, 12}) {
		t.Fatalf("marques = %+v", marks)
	}
}

func TestParseMarkOnSeveralLines(t *testing.T) {
	src := "{--première ligne\r\nseconde ligne--}"
	marks, issues := Parse([]byte(src))
	if len(issues) != 0 || len(marks) != 1 || text(src, marks[0].Text) != "première ligne\r\nseconde ligne" {
		t.Fatalf("marques = %+v, problèmes = %v", marks, issues)
	}
}

func TestParseSubstitutionUsesTheFirstArrow(t *testing.T) {
	src := "{~~a~>b~>c~~}"
	marks, _ := Parse([]byte(src))
	if len(marks) != 1 || text(src, marks[0].Text) != "a" || text(src, marks[0].New) != "b~>c" {
		t.Fatalf("marques = %+v", marks)
	}
}

func TestParseMalformedMarks(t *testing.T) {
	cases := []struct {
		src    string
		marks  int
		issues []Issue
	}{
		{"texte {++ jamais fermé", 0, []Issue{{6, Unclosed}}},
		{"{~~sans flèche~~}", 0, []Issue{{0, MissingArrow}}},
		{"{++ a {>>b<<} ++}", 1, []Issue{{0, Nested}}},
		{"{++++}", 1, []Issue{{0, Empty}}},
		{"{~~~>~~}", 1, []Issue{{0, Empty}}},
		{"{~~~>neuf~~}", 1, nil}, // empty old text: a pure insertion, allowed
		{"{--}", 0, []Issue{{0, Unclosed}}},
		{"une accolade { seule et {+ presque", 0, nil},
		{"fermants seuls ++} --} <<}", 0, nil},
	}
	for _, c := range cases {
		marks, issues := Parse([]byte(c.src))
		if len(marks) != c.marks || !reflect.DeepEqual(issues, c.issues) {
			t.Errorf("Parse(%q) = %d marques, %v ; attendu %d, %v", c.src, len(marks), issues, c.marks, c.issues)
		}
	}
}

func TestParseKeepsGoingAfterAnUnclosedMark(t *testing.T) {
	src := "{>> ouvert {++ajout++}"
	marks, issues := Parse([]byte(src))
	if len(issues) != 1 || len(marks) != 1 || marks[0].Kind != Addition {
		t.Fatalf("marques = %+v, problèmes = %v", marks, issues)
	}
}

func TestGroupAttachesAdjacentComments(t *testing.T) {
	src := "{==passage==}{>>question<<}{>>réponse<<} puis {~~a~>b~~}{>>pourquoi ?<<} " +
		"et {>>seul<<} {>>détaché<<} {++ajout++}"
	marks, _ := Parse([]byte(src))
	groups := Group(marks)
	var got [][]string
	for _, g := range groups {
		row := []string{text(src, marks[g.Main].Span)}
		for _, c := range g.Comments {
			row = append(row, text(src, marks[c].Text))
		}
		got = append(got, row)
		if text(src, g.Span) != src[marks[g.Main].Span.Start:g.Span.End] {
			t.Errorf("étendue du groupe incohérente : %+v", g)
		}
	}
	want := [][]string{
		{"{==passage==}", "question", "réponse"},
		{"{~~a~>b~~}", "pourquoi ?"},
		{"{>>seul<<}"},
		{"{>>détaché<<}"},
		{"{++ajout++}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groupes = %q\nattendu  %q", got, want)
	}
}

func TestContainsDelimiter(t *testing.T) {
	for _, s := range []string{"a {>> b", "x ++} y", "~~}", "{==", "{~~"} {
		if !ContainsDelimiter([]byte(s)) {
			t.Errorf("ContainsDelimiter(%q) = false", s)
		}
	}
	for _, s := range []string{"texte ordinaire", "a ~> b", "{+ x +}", "-- tiret --"} {
		if ContainsDelimiter([]byte(s)) {
			t.Errorf("ContainsDelimiter(%q) = true", s)
		}
	}
}

// FuzzParse checks that Parse never panics and always returns well-formed,
// ordered, non-overlapping marks whose delimiters are where they should be.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "{++a++}", "{~~a~>b~~}", "{>>c<<}{>>d<<}", "{==x==}", "{-- {++ --}",
		"{++", "{~~~>~~}", "été {>>ça<<}\r\n", "{{++a++}}", "{~~a~~}{~~b~>c~~}",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		marks, issues := Parse(src)
		prev := 0
		for _, m := range marks {
			if m.Span.Start < prev || m.Span.End > len(src) || m.Span.Start >= m.Span.End {
				t.Fatalf("marque hors bornes ou chevauchante : %+v (fin précédente %d, longueur %d)", m, prev, len(src))
			}
			prev = m.Span.End
			if !bytes.HasPrefix(src[m.Span.Start:], openers[m.Kind]) ||
				!bytes.HasSuffix(src[:m.Span.End], closers[m.Kind]) {
				t.Fatalf("délimiteurs absents : %q", src[m.Span.Start:m.Span.End])
			}
			if m.Text.Start < m.Span.Start || m.Text.End > m.Span.End || m.Text.Start > m.Text.End {
				t.Fatalf("contenu hors de la marque : %+v", m)
			}
			if m.Kind == Substitution {
				if m.New.Start != m.Text.End+len(arrow) || m.New.End+len(closers[m.Kind]) != m.Span.End {
					t.Fatalf("remplacement mal découpé : %+v", m)
				}
			}
		}
		for _, is := range issues {
			if is.Offset < 0 || is.Offset >= len(src) {
				t.Fatalf("problème hors bornes : %v", is)
			}
		}
		Group(marks)
	})
}

// TestParseIsLinear guards against the quadratic behaviour found by the code
// review: 1 MB of unclosed openers took tens of seconds before the fix.
func TestParseIsLinear(t *testing.T) {
	for _, unit := range []string{"{++", "{~~", "{~~a~~}"} {
		src := bytes.Repeat([]byte(unit), 1_000_000/len(unit))
		start := time.Now()
		Parse(src)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("%q × %d : %v", unit, len(src)/len(unit), d)
		}
	}
}

// D-24: marks written in code are literal; every other mark is recognised.
func TestMarksInCodeAreLiteral(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		marks   []string // whole marks recognised, in order
		literal int      // openers left literal
	}{
		{"code en ligne", "voir `{>> remarque <<}` ici {>>vraie<<}", []string{"{>>vraie<<}"}, 1},
		{"double accent grave", "``a ` {++b++} `` et {++c++}", []string{"{++c++}"}, 1},
		{"accent grave sans partenaire", "un ` seul {++a++}", []string{"{++a++}"}, 0},
		{"longueurs différentes", "``a` {++b++}", []string{"{++b++}"}, 0},
		{"échappé", "\\` {++a++} `", []string{"{++a++}"}, 0},
		{"échappé puis code", "\\`` {++a++} `", []string{}, 1},
		{"échappé seul", "\\` {++a++}", []string{"{++a++}"}, 0},
		{"ligne vide", "`a\n\n{++b++} `", []string{"{++b++}"}, 0},
		{"ligne vide d'encadré", "> `a\n>\n> {++b++} `", []string{"{++b++}"}, 0},
		{"code sur deux lignes", "`a\n{++b++}` {--c--}", []string{"{--c--}"}, 1},
		{"bloc clôturé", "{==p==}{>>x<<}\n\n```\n{++a++}\n{~~b~>c~~}\n```\n{++d++}", []string{"{==p==}", "{>>x<<}", "{++d++}"}, 2},
		{"bloc avec langage", "```go\n{++a++}\n```\n{++d++}", []string{"{++d++}"}, 1},
		{"tildes", "~~~\n{++a++}\n~~~~\n{++d++}", []string{"{++d++}"}, 1},
		{"clôture plus courte", "````\n{++a++}\n```\n{++b++}\n````\n{++d++}", []string{"{++d++}"}, 2},
		{"bloc non fermé", "```\n{++a++}\n{++b++}", []string{}, 2},
		{"bloc dans un encadré", "> ```\n> {>>a<<}\n> ```\n{>>b<<}", []string{"{>>b<<}"}, 1},
		{"fin de l'encadré", "> ```\n> {>>a<<}\n\n{>>b<<}", []string{"{>>b<<}"}, 1},
		{"indenté dans une liste", "- a\n\n  ```\n  {++x++}\n  ```\n- {++y++}", []string{"{++y++}"}, 1},
		{"code en ligne de trois accents", "```a``` {++b++}", []string{"{++b++}"}, 0},
		{"accent grave dans une marque", "{>>le `code` ici<<} `x`", []string{"{>>le `code` ici<<}"}, 0},
		{"marque qui contient un bloc", "{>>a\n```\nb\n```\n<<}", []string{"{>>a\n```\nb\n```\n<<}"}, 0},
		{"marque d'ordre des octets", "\xef\xbb\xbf```\n{++a++}\n```\n{++b++}", []string{"{++b++}"}, 1},
		{"CRLF", "```\r\n{++a++}\r\n```\r\n{++b++}", []string{"{++b++}"}, 1},
		{"pas de marque non fermée dans le code", "`{++` et {++a++}", []string{"{++a++}"}, 1},
		// Revue Codex, défauts 6 et 7.
		{"pseudo-clôture indentée", "paragraphe\n    ~~~\n\n{>>vrai<<}", []string{"{>>vrai<<}"}, 0},
		{"code en ligne coupé par un titre", "`début\n# Titre {>>vrai<<}\nfin`", []string{"{>>vrai<<}"}, 0},
		{"code en ligne coupé par une liste", "`début\n- item {>>vrai<<}\nfin`", []string{"{>>vrai<<}"}, 0},
		{"code en ligne coupé par un encadré", "`début\n> cité {>>vrai<<}\nfin`", []string{"{>>vrai<<}"}, 0},
		{"code en ligne dans un tableau", "| `a | {>>vrai<<} |\n|---|---|\n| b` |", []string{"{>>vrai<<}"}, 0},
		{"marque sur la ligne d'ouverture", "```python {>>x<<}\n{++a++}\n```\n", []string{"{>>x<<}"}, 1},
		{"accents graves dans la ligne d'ouverture", "~~~ `{>>x<<}`\n{++a++}\n~~~\n{++b++}", []string{"{>>x<<}", "{++b++}"}, 1},
		// Troisième revue Codex.
		{"éléments numérotés", "1. Avant `\n2. Après {>>vraie<<} `\n", []string{"{>>vraie<<}"}, 0},
		{"encadré pandoc", "::: note\nAvant `\n:::\nAprès {>>vraie<<} `\n", []string{"{>>vraie<<}"}, 0},
		{"clôture indentée de quatre espaces", "   ```\nexemple\n    ```\n```\n{>>vraie<<}", []string{"{>>vraie<<}"}, 0},
		// Contre-vérification Codex.
		{"barre verticale dans un paragraphe", "`a\n| b` {>>vrai<<} `fin", []string{"{>>vrai<<}"}, 0},
		{"liste qui ne commence pas à 1", "`a\n2. b` {>>vrai<<} `fin", []string{"{>>vrai<<}"}, 0},
		{"titre Setext", "`début\n===\n{>>vrai<<} fin`", []string{"{>>vrai<<}"}, 0},
		{"liste de définitions", "`Terme\n: définition {>>vrai<<} fin`", []string{"{>>vrai<<}"}, 0},
		{"sous-liste quittée", "- outer\n  - inner\n\n  outer continuation\n\n      ~~~\n\n{>>vrai<<}", []string{"{>>vrai<<}"}, 0},
		{"liste quittée", "- a\n\n    ~~~\n    code\n{>>vrai<<}\n", []string{"{>>vrai<<}"}, 0},
		{"code en ligne sur deux lignes d'un encadré", "> `a\n> {++b++}` {--c--}", []string{"{--c--}"}, 1},
		{"bloc dans une liste", "- a\n\n  ```\n  {++x++}\n  ```\n{++y++}", []string{"{++y++}"}, 1},
		// Fence not recognised (indented), but its backticks pair as code, as
		// pandoc reads it (code inside the list, indented code block).
		{"bloc dans une liste imbriquée", "- a\n  - b\n\n    ```\n    {++x++}\n    ```\n{++y++}", []string{"{++y++}"}, 1},
		{"bloc indenté hors liste", "Texte.\n\n    ```\n    {++x++}\n    ```\n", []string{}, 1},
		{"clôture trop indentée", "```\n{++a++}\n        ```\n{++b++}\n```\n{++c++}", []string{"{++c++}"}, 2},
	}
	for _, c := range cases {
		r := Scan([]byte(c.src))
		var got []string
		for _, m := range r.Marks {
			got = append(got, text(c.src, m.Span))
		}
		if len(got) != len(c.marks) {
			t.Errorf("%s : marques %q, attendu %q", c.name, got, c.marks)
			continue
		}
		for i := range got {
			if got[i] != c.marks[i] {
				t.Errorf("%s : marques %q, attendu %q", c.name, got, c.marks)
				break
			}
		}
		if len(r.Literal) != c.literal {
			t.Errorf("%s : %d marques littérales, attendu %d", c.name, len(r.Literal), c.literal)
		}
		if len(r.Issues) != 0 {
			t.Errorf("%s : anomalies %v", c.name, r.Issues)
		}
	}
}

func TestCodeScanIsLinear(t *testing.T) {
	for _, src := range [][]byte{
		bytes.Repeat([]byte("` {++a++} "), 50000),
		bytes.Repeat([]byte("``` "), 50000),
		bytes.Repeat([]byte("```\n"), 50000),
		bytes.Repeat([]byte("\\`"), 100000),
	} {
		start := time.Now()
		Scan(src)
		if d := time.Since(start); d > time.Second {
			t.Errorf("analyse trop lente : %v pour %d octets", d, len(src))
		}
	}
}
