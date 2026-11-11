package render

import (
	"context"
	"github.com/ptitan/glose/internal/critic"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/store"
)

func needPandoc(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent : test sauté")
	}
}

func mkproject(t *testing.T, files map[string]string) *project.Project {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// spanRe matches the word spans of the provenance contract.
var spanRe = regexp.MustCompile(`<span data-g="(\d+):(\d+),(\d+)(,t)?">([^<]*)</span>`)

// checkContract verifies that every certified word span of the HTML holds
// exactly the source bytes it points to, and returns the number of spans.
func checkContract(t *testing.T, p *project.Project, doc *Doc) int {
	t.Helper()
	srcs := map[int][]byte{}
	n := 0
	for _, m := range spanRe.FindAllStringSubmatch(doc.HTML, -1) {
		i, _ := strconv.Atoi(m[1])
		s, _ := strconv.Atoi(m[2])
		e, _ := strconv.Atoi(m[3])
		src, ok := srcs[i]
		if !ok {
			b, err := os.ReadFile(p.Path(i))
			if err != nil {
				t.Fatal(err)
			}
			src, srcs[i] = b, b
		}
		text := html.UnescapeString(m[5])
		if s < 0 || e > len(src) || s > e {
			t.Fatalf("plage hors du fichier : %v", m)
		}
		if m[4] == "" && strings.TrimSpace(text) != "" && string(src[s:e]) != text {
			t.Fatalf("%s : mot certifié %q, source %q", p.Files[i], text, src[s:e])
		}
		n++
	}
	return n
}

const annotated = "# Titre — essai\n\n" +
	"{#p-un} **Gras.** Le juge {~~rejette~>casse~~}{>>pourquoi ?<<} le {++nouveau ++}pourvoi " +
	"{--très --}vite. {==Passage==}{>>note<<} fin, voir n° [](#p-un).\n\n" +
	"L’été, « citation » et une formule $e^{x}$.\n\n" +
	"<script>alert(1)</script>\n\n" +
	"![image externe](https://exemple.org/a.png) et ![locale](img/a.png).\n\n" +
	"| a | b |\n|---|---|\n| cellule | **gras** |\n"

func TestRenderWithoutProfile(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{"a.md": annotated, "b.md": "Second fichier.\n"})
	doc, err := New(p).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := checkContract(t, p, doc); n < 30 {
		t.Fatalf("seulement %d mots repérés", n)
	}
	for _, bad := range []string{"data-pos", "<script>", "exemple.org", "{~~", "{>>"} {
		if strings.Contains(doc.HTML, bad) {
			t.Errorf("le HTML contient %q", bad)
		}
	}
	for _, good := range []string{"{#p-un}", "&lt;script&gt;", "<math", `data-f="1"`, "glose-image-externe"} {
		if !strings.Contains(doc.HTML, good) {
			t.Errorf("le HTML ne contient pas %q", good)
		}
	}
	if len(doc.Annotations) != 4 || doc.Annotations[0].Kind != "substitution" || doc.Annotations[0].Comments[0] != "pourquoi ?" {
		t.Fatalf("annotations : %+v", doc.Annotations)
	}
	// Current view: the addition is hidden, the deletion and the old text shown.
	if strings.Contains(doc.HTML, ">nouveau<") || !strings.Contains(doc.HTML, ">rejette<") {
		t.Error("la vue affichée n'est pas la vue Actuel")
	}
}

func TestMapCheck(t *testing.T) {
	needPandoc(t)
	src := "Un **mot important** ici et [un lien](x.md) puis $f$ fin.\n\nSecond paragraphe.\n"
	p := mkproject(t, map[string]string{"a.md": src})
	d, _ := store.Read(p.Path(0))
	m, err := New(p).Map(context.Background(), 0, d)
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) int { return strings.Index(src, s) }
	end := func(s string) int { return strings.Index(src, s) + len(s) }
	b := []byte(src)
	// Inside a word, a bold, a whole paragraph: accepted as they are.
	for _, r := range [][2]int{{at("mot"), end("mot")}, {at("ot impor"), end("ot impor")}, {at("Un"), end("fin.")}, {at("mot"), end("important")}} {
		res, err := m.Check(b, r[0], r[1])
		if err != nil || res.Widened || res.Start != r[0] || res.End != r[1] {
			t.Errorf("%q : %+v, %v", src[r[0]:r[1]], res, err)
		}
	}
	// Cutting the bold or the link: widened to the whole element.
	res, err := m.Check(b, at("important"), end("ici"))
	if err != nil || !res.Widened || src[res.Start:res.End] != "**mot important** ici" {
		t.Errorf("gras coupé : %+v %q, %v", res, src[res.Start:res.End], err)
	}
	res, err = m.Check(b, at("et"), end("un"))
	if err != nil || !res.Widened || src[res.Start:res.End] != "et [un lien](x.md)" {
		t.Errorf("lien coupé : %+v, %v", res, err)
	}
	// The widened range is accepted as it is when sent again (SPEC F5).
	if again, err := m.Check(b, res.Start, res.End); err != nil || again.Widened {
		t.Errorf("passage élargi renvoyé : %+v, %v", again, err)
	}
	// Two paragraphs, a boundary on a delimiter, inside "**".
	if _, err := m.Check(b, at("fin"), end("Second")); err != sourcemap.ErrBlocks {
		t.Errorf("deux blocs : %v", err)
	}
	if res, err := m.Check(b, at("**mot"), end("mot")); err != nil || src[res.Start:res.End] != "**mot important**" {
		t.Errorf("début sur un délimiteur : %+v, %v", res, err)
	}
	if _, err := m.Check(b, at("*mot"), end("mot")); err != sourcemap.ErrBoundary {
		t.Errorf("borne au milieu d'un délimiteur : %v", err)
	}
	if got, err := m.BlockEnd(at("ici")); err != nil || got != end("fin.") {
		t.Errorf("fin de paragraphe : %d (%q), %v", got, src[:got], err)
	}
}

func TestCheckPandoc(t *testing.T) {
	needPandoc(t)
	if v, err := CheckPandoc(context.Background(), "pandoc"); err != nil || !strings.HasPrefix(v, "pandoc") {
		t.Fatalf("%q, %v", v, err)
	}
	if _, err := CheckPandoc(context.Background(), "pandoc-qui-n-existe-pas"); err == nil {
		t.Fatal("erreur attendue")
	}
}

// Regression tests for the code review of P1.

func TestFootnotesAndCellsAreBlocksOfTheirOwn(t *testing.T) {
	needPandoc(t)
	src := "Text[^n] end.\n\n[^n]: footnote text\n\n| a | b |\n|---|---|\n| one | two |\n"
	p := mkproject(t, map[string]string{"a.md": src})
	d, _ := store.Read(p.Path(0))
	m, err := New(p).Map(context.Background(), 0, d)
	if err != nil {
		t.Fatal(err)
	}
	b := []byte(src)
	at := func(s string) int { return strings.Index(src, s) }
	end := func(s string) int { return strings.Index(src, s) + len(s) }
	if got, err := m.BlockEnd(at("Text")); err != nil || got != end("end.") {
		t.Errorf("fin du paragraphe qui appelle la note : %d (%q), %v", got, src[:got], err)
	}
	if got, err := m.BlockEnd(at("footnote")); err != nil || got != end("footnote text") {
		t.Errorf("fin de la note : %d, %v", got, err)
	}
	if _, err := m.Check(b, at("one"), end("two")); err != sourcemap.ErrBlocks {
		t.Errorf("sélection sur deux cellules : %v", err)
	}
	if got, err := m.BlockEnd(at("one")); err != nil || got != end("one") {
		t.Errorf("fin de la cellule : %d, %v", got, err)
	}
	if _, err := m.Check(b, at("Text"), end("text")); err != sourcemap.ErrBlocks {
		t.Errorf("du paragraphe à sa note : %v", err)
	}
}

func TestAttributesFromTheTextAreCleaned(t *testing.T) {
	needPandoc(t)
	src := "Un [mot]{onclick=\"alert(1)\" data-g=\"0:90,99\" title=\"ok\"} et ![i](a.png){onerror=\"x\" srcset=\"http://e/x 2x\"}.\n\n# Saisie\n\n" +
		// Revue de GLM : un style, la provenance et les classes de l'interface.
		"Un [masqué]{style=\"display:none\" .glose-file .gardee data-path=\"b.md\" data-f=\"3\" data-glose-explicit=\"1\"}.\n"
	p := mkproject(t, map[string]string{"a.md": src, "a.png": "x"})
	doc, err := New(p).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"onclick", "onerror", "srcset", `data-g="0:90,99"`, `id="saisie"`, "display:none", `data-path="b.md"`, `data-f="3"`, "data-glose-explicit", `class="glose-file gardee"`} {
		if strings.Contains(doc.HTML, bad) {
			t.Errorf("le HTML contient %q", bad)
		}
	}
	for _, good := range []string{`title="ok"`, `id="gl-f0-a-gl-saisie"`, `class="gardee"`} {
		if !strings.Contains(doc.HTML, good) {
			t.Errorf("le HTML ne contient pas %q", good)
		}
	}
	checkContract(t, p, doc)
}

func TestWideningInsideAScope(t *testing.T) {
	needPandoc(t)
	src := "Texte[^n].\n\n[^n]: **bold** tail\n\n| a |\n|---|\n| **gras** fin |\n"
	p := mkproject(t, map[string]string{"a.md": src})
	d, _ := store.Read(p.Path(0))
	m, err := New(p).Map(context.Background(), 0, d)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][3]string{{"bold", "tail", "**bold** tail"}, {"gras", "fin", "**gras** fin"}} {
		s, e := strings.Index(src, c[0]), strings.Index(src, c[1])+len(c[1])
		res, err := m.Check([]byte(src), s, e)
		if err != nil || !res.Widened || src[res.Start:res.End] != c[2] {
			t.Errorf("%s → %s : %+v, %v", c[0], c[1], res, err)
		}
	}
}

// Filet de sécurité de D-24 : une marque que Glose prend pour du code alors
// que pandoc ne la lit pas comme du code est signalée ; une marque citée dans
// du code ne l'est pas.
func TestLiteralOutsideCode(t *testing.T) {
	needPandoc(t)
	hidden := "    texte `2. ***\n{>>vraie<<} Terme\nfin `|---|---|\ntexte\n"
	cited := "Voir `{>> remarque <<}` et :\n\n```\n{++ ajout ++}\n```\n"
	p := mkproject(t, map[string]string{"a.md": hidden + "\n" + cited})
	src := []byte(hidden + "\n" + cited)
	if r := critic.Scan(src); len(r.Literal) != 3 {
		t.Fatalf("marques littérales : %v", r.Literal)
	}
	sus, err := New(p).LiteralOutsideCode(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(sus) != 1 || string(src[sus[0]:sus[0]+3]) != "{>>" || sus[0] != strings.Index(hidden, "{>>vraie") {
		t.Fatalf("suspectes : %v", sus)
	}
}

// The view Proposed (P3.5): every proposal accepted; the words of a proposal
// carry the place of their new text in the source, even when a word straddles
// the edge of a mark; the map used to annotate stays that of the view Current.
func TestRenderProposed(t *testing.T) {
	needPandoc(t)
	src := annotated + "\nLes {~~chat~>chien~~}s dorment{++ bien++}.\n"
	p := mkproject(t, map[string]string{"a.md": src})
	r := New(p)
	doc, err := r.RenderView(context.Background(), projection.Proposed)
	if err != nil {
		t.Fatal(err)
	}
	if n := checkContract(t, p, doc); n < 30 {
		t.Fatalf("seulement %d mots repérés", n)
	}
	for _, bad := range []string{">rejette<", ">très<", "{~~", "data-pos"} {
		if strings.Contains(doc.HTML, bad) {
			t.Errorf("vue Proposé : le HTML contient %q", bad)
		}
	}
	at := func(sub string) string {
		i := strings.Index(src, sub)
		return strconv.Itoa(i) + "," + strconv.Itoa(i+len(sub))
	}
	// « chiens » is cut into « chien » (new text of the substitution) and « s ».
	for _, want := range []string{
		`data-g="0:` + at("casse") + `">casse<`,
		`data-g="0:` + at("nouveau") + `">nouveau<`,
		`data-g="0:` + at("chien") + `">chien<`,
		`>s</span>`,
		`data-g="0:` + at("bien") + `">bien<`,
	} {
		if !strings.Contains(doc.HTML, want) {
			t.Errorf("vue Proposé : %q absent", want)
		}
	}
	if a := doc.Annotations[0]; a.Kind != "substitution" || src[a.NewStart:a.NewEnd] != "casse" {
		t.Errorf("plage du nouveau texte : %+v", a)
	}
	// The map used to annotate is that of the view Current.
	d, _ := store.Read(p.Path(0))
	m, err := r.Map(context.Background(), 0, d)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(src, "rejette")
	if _, err := m.Check(d.Bytes, i, i+len("rejette")); err != nil {
		t.Errorf("carte de la vue Actuel : %v", err)
	}
	// The view Current is unchanged by the rendering of the view Proposed.
	cur, err := r.Render(context.Background())
	if err != nil || !strings.Contains(cur.HTML, ">rejette<") || strings.Contains(cur.HTML, ">casse<") {
		t.Fatalf("vue Actuel après la vue Proposé : %v", err)
	}
	// In the view Current too, « chats » is cut at the edge of the mark: the
	// « s » left unchanged is not part of the proposal (Codex review, point 3).
	if !strings.Contains(cur.HTML, `data-g="0:`+at("chat")+`">chat<`) || strings.Contains(cur.HTML, ">chats<") {
		t.Errorf("vue Actuel : « chats » non découpé")
	}
	i = strings.Index(src, "}s dorment") + 1
	if _, err := m.Check(d.Bytes, i, i+1); err != nil {
		t.Errorf("le « s » hors de la marque n'est pas un mot de la carte : %v", err)
	}
}

// P3.4: a selection never cuts a character as the reader sees it: an emoji
// with its skin tone, a sequence joined by U+200D, a flag, a variation
// selector. Bounds around them are accepted.
func TestMapCheckGraphemes(t *testing.T) {
	needPandoc(t)
	src := "Un \U0001F44D\U0001F3FD bien, \U0001F469\u200D\U0001F4BB code, \U0001F1EB\U0001F1F7 et \u2764\uFE0F fin.\n\n" +
		"Un cafe\u0301 noir.\n\nFin.\n"
	p := mkproject(t, map[string]string{"a.md": src})
	d, _ := store.Read(p.Path(0))
	m, err := New(p).Map(context.Background(), 0, d)
	if err != nil {
		t.Fatal(err)
	}
	b := []byte(src)
	at := func(s string) int { return strings.Index(src, s) }
	end := at("fin.") + len("fin.")
	for _, cut := range []string{"\U0001F3FD bien", "\u200D\U0001F4BB", "\U0001F4BB code", "\U0001F1F7 et", "\uFE0F fin"} {
		if _, err := m.Check(b, at(cut), end); err == nil {
			t.Errorf("début au milieu d'un caractère accepté : %+q", cut)
		}
		if _, err := m.Check(b, at("Un"), at(cut)); err == nil {
			t.Errorf("fin au milieu d'un caractère acceptée : %+q", cut)
		}
	}
	for _, whole := range []string{"\U0001F44D\U0001F3FD", "\U0001F469\u200D\U0001F4BB", "\U0001F1EB\U0001F1F7", "\u2764\uFE0F"} {
		if res, err := m.Check(b, at(whole), at(whole)+len(whole)); err != nil || res.Widened {
			t.Errorf("caractère entier refusé : %+q, %v", whole, err)
		}
	}
	// A decomposed accent (e + U+0301) is composed by pandoc (NFC): Glose
	// counts its columns as pandoc does (D-29), so the rest of the line can
	// be annotated; the accented word itself only as a whole.
	if _, err := m.Check(b, at("noir"), at("noir")+len("noir")); err != nil {
		t.Errorf("mot après un accent décomposé refusé : %v", err)
	}
	if _, err := m.Check(b, at("cafe"), at("cafe")+len("cafe\u0301")); err != nil {
		t.Errorf("mot à l'accent décomposé, entier, refusé : %v", err)
	}
	if _, err := m.Check(b, at("cafe"), at("cafe")+len("cafe")); err == nil {
		t.Error("accent décomposé coupé de sa lettre accepté")
	}
	second := strings.LastIndex(src, "Un cafe")
	if _, err := m.Check(b, second, second+2); err != nil {
		t.Errorf("mot avant l'accent décomposé refusé : %v", err)
	}
	if _, err := m.Check(b, at("Fin."), at("Fin.")+4); err != nil {
		t.Errorf("paragraphe suivant refusé : %v", err)
	}
}

func TestLineColOutOfRange(t *testing.T) {
	if l, c := LineCol([]byte("ab\ncd"), -1); l != 1 || c != 1 {
		t.Fatalf("%d %d", l, c)
	}
	if l, c := LineCol([]byte("ab\ncd"), 99); l != 2 || c != 3 {
		t.Fatalf("%d %d", l, c)
	}
}

// The HTML has LF line endings on every system (--eol=lf): pandoc writes
// CRLF under Windows by default; found by the first run on Windows with
// pandoc (CI).
func TestHTMLHasLFLineEndings(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{"a.md": "# Titre\r\n\r\nUn paragraphe.\r\n\r\n- un\r\n- deux\r\n"})
	d, err := New(p).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.HTML, "\r") || !strings.Contains(d.HTML, "\n") {
		t.Fatalf("fins de ligne : %q", d.HTML)
	}
}
