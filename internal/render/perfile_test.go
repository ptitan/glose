package render

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// P6.5 : sans filtre, chaque fichier a ses notes, numérotées en lui ; leurs
// identifiants sont préfixés par le fichier et chaque lien mène à sa cible.
func TestFootnotesPerFileHaveDistinctIdentifiers(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{
		"a.md": "Premier[^1] texte.\n\n[^1]: Note de a.\n",
		"b.md": "Second[^1] texte.\n\n[^1]: Note de b.\n",
	})
	d, err := New(p).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	for _, m := range regexp.MustCompile(`id="([^"]+)"`).FindAllStringSubmatch(d.HTML, -1) {
		ids[m[1]]++
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("identifiant %q répété %d fois", id, n)
		}
	}
	for _, want := range []string{"gl-f0-fn1", "gl-f1-fn1", "gl-f0-fnref1", "gl-f1-fnref1", "gl-f0-footnotes", "gl-f1-footnotes"} {
		if ids[want] != 1 {
			t.Errorf("identifiant %q absent", want)
		}
	}
	for _, m := range regexp.MustCompile(`href="#([^"]+)"`).FindAllStringSubmatch(d.HTML, -1) {
		if ids[m[1]] != 1 {
			t.Errorf("lien vers %q sans cible", m[1])
		}
	}
	checkContract(t, p, d)
}

// P6.5 : après la modification d'un fichier, seul ce fichier repasse par
// pandoc (lecture et HTML) ; les autres sont repris tels quels.
func TestOnlyTheChangedFileIsRenderedAgain(t *testing.T) {
	needPandoc(t)
	if runtime.GOOS == "windows" {
		t.Skip("enveloppe de pandoc en script shell")
	}
	p := mkproject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b.\n", "c.md": "Texte c.\n"})
	log := filepath.Join(t.TempDir(), "appels")
	wrapper := filepath.Join(t.TempDir(), "pandoc")
	os.WriteFile(wrapper, []byte("#!/bin/sh\necho x >> "+log+"\nexec pandoc \"$@\"\n"), 0o755)
	p.Pandoc = wrapper
	r := New(p)
	if _, err := r.Render(context.Background()); err != nil {
		t.Fatal(err)
	}
	count := func() int { b, _ := os.ReadFile(log); return strings.Count(string(b), "x") }
	if n := count(); n != 6 {
		t.Fatalf("premier rendu : %d appels, 6 attendus (3 lectures, 3 HTML)", n)
	}
	os.WriteFile(p.Path(1), []byte("Texte b modifié.\n"), 0o644)
	d, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 8 {
		t.Fatalf("après modification de b.md : %d appels au total, 8 attendus", n)
	}
	if !strings.Contains(d.HTML, "modifié") || !strings.Contains(d.HTML, ">c<") && !strings.Contains(d.HTML, "Texte") {
		t.Fatalf("rendu : %s", d.HTML)
	}
	checkContract(t, p, d)
}

// P6.5 : avec un filtre, le rendu reste d'un seul tenant (un filtre peut
// numéroter d'un fichier à l'autre), sauf si le profil dit filterPerFile.
func TestFilterKeepsTheWholeRenderUnlessPerFile(t *testing.T) {
	needPandoc(t)
	filter := "local n = 0\nfunction Para(p) n = n + 1; p.content:insert(1, pandoc.Str(n .. '. ')); return p end\n"
	files := map[string]string{"a.md": "Un.\n", "b.md": "Deux.\n", "f.lua": filter}
	files["glose.json"] = `{"filter": "f.lua"}`
	p := mkproject(t, files)
	d, err := New(p).Render(context.Background())
	if err != nil || !strings.Contains(d.HTML, "2. ") {
		t.Fatalf("rendu d'un seul tenant : numérotation suivie attendue : %v %s", err, d.HTML)
	}
	files["glose.json"] = `{"filter": "f.lua", "filterPerFile": true}`
	p = mkproject(t, files)
	if !p.PerFile {
		t.Fatal("filterPerFile non lu")
	}
	d, err = New(p).Render(context.Background())
	if err != nil || strings.Contains(d.HTML, "2. ") || strings.Count(d.HTML, "1. ") != 2 {
		t.Fatalf("rendu par fichier : chaque fichier recommence : %v %s", err, d.HTML)
	}
}

// P6.8 : une partie du projet est seule rendue ; les annotations, empreintes
// et tailles de tous les fichiers sont données. Avec un filtre d'un seul
// tenant, tout est rendu. Une partie qui ne nomme rien rend tout.
func TestRenderPart(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{"a.md": "Texte {>>ra<<} a.\n", "livre/01.md": "Un {>>r1<<}.\n", "livre/02.md": "Deux.\n"})
	r := New(p)
	d, err := r.RenderPart(context.Background(), 0, "livre/")
	if err != nil {
		t.Fatal(err)
	}
	if d.Part != "livre/" || strings.Contains(d.HTML, `data-path="a.md"`) || !strings.Contains(d.HTML, `data-path="livre/02.md"`) {
		t.Fatalf("partie : %q %s", d.Part, d.HTML)
	}
	if len(d.Annotations) != 2 || d.Files[0].Shown || !d.Files[1].Shown || d.Files[0].Hash == "" || d.Files[0].Size == 0 {
		t.Fatalf("annotations ou fichiers : %+v %+v", d.Annotations, d.Files)
	}
	checkContract(t, p, d)
	if d, _ := r.RenderPart(context.Background(), 0, "absent/"); d.Part != "" || !strings.Contains(d.HTML, `data-path="a.md"`) {
		t.Fatalf("partie inconnue : %q", d.Part)
	}
	if d, _ := r.RenderPart(context.Background(), 0, AutoPart); d.Part != "" {
		t.Fatalf("petit projet, partie automatique : %q", d.Part)
	}
	p.PerFile = false
	if d, _ := New(p).RenderPart(context.Background(), 0, "livre/"); d.Part != "" || !strings.Contains(d.HTML, `data-path="a.md"`) {
		t.Fatalf("rendu d'un seul tenant : %q", d.Part)
	}
}

// Revue de Codex (P6) : un filtre « filterPerFile » modifié sous le même nom
// n'est pas servi par le HTML gardé.
func TestChangedFilterIsNotServedFromTheCache(t *testing.T) {
	needPandoc(t)
	files := map[string]string{"a.md": "Un.\n", "f.lua": "function Str(s) s.text = s.text end\n", "glose.json": `{"filter": "f.lua", "filterPerFile": true}`}
	p := mkproject(t, files)
	r := New(p)
	r.EnableDiskCache(context.Background())
	if d, err := r.Render(context.Background()); err != nil || strings.Contains(d.HTML, "AJOUT") {
		t.Fatalf("premier rendu : %v", err)
	}
	os.WriteFile(filepath.Join(p.Root, "f.lua"), []byte("function Para(p) p.content:insert(1, pandoc.Str('AJOUT ')); return p end\n"), 0o644)
	if d, err := r.Render(context.Background()); err != nil || !strings.Contains(d.HTML, "AJOUT") {
		t.Fatalf("filtre modifié ignoré (mémoire) : %v", err)
	}
	r2 := New(p)
	r2.EnableDiskCache(context.Background())
	if d, err := r2.Render(context.Background()); err != nil || !strings.Contains(d.HTML, "AJOUT") {
		t.Fatalf("filtre modifié ignoré (disque) : %v", err)
	}
}

// The memory keeps one HTML per file, view and index: the versions of a file
// annotated again and again do not pile up.
func TestHTMLCacheKeepsOneVersionPerFile(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b.\n"})
	r := New(p)
	for n := range 5 {
		os.WriteFile(p.Path(0), []byte(strings.Repeat("Texte a modifié. ", n+1)+"\n"), 0o644)
		if _, err := r.Render(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	kept := len(r.html)
	r.mu.Unlock()
	if kept != 2 {
		t.Fatalf("%d versions gardées en mémoire, 2 attendues (une par fichier)", kept)
	}
}

// pandoc keeps the quotes of inline code: what a text writes there is not an
// attribute, and is neither renamed nor moved.
func TestInlineCodeIsNotTakenForAttributes(t *testing.T) {
	in := []byte(`<p><span data-g="0:10,12">x</span><a href="#fn1" id="fnref1"><code>id="fn1" data-g="0:120,125"</code></a></p>`)
	got := string(footnoteIDs(in, 2))
	if want := `<p><span data-g="0:10,12">x</span><a href="#gl-f2-fn1" id="gl-f2-fnref1"><code>id="fn1" data-g="0:120,125"</code></a></p>`; got != want {
		t.Errorf("footnoteIDs :\n%s\nattendu :\n%s", got, want)
	}
	got = string(moveHTML(in, 100))
	if want := `<p><span data-g="0:110,112">x</span><a href="#fn1" id="fnref1"><code>id="fn1" data-g="0:120,125"</code></a></p>`; got != want {
		t.Errorf("moveHTML :\n%s\nattendu :\n%s", got, want)
	}
	needPandoc(t)
	p := mkproject(t, map[string]string{"a.md": "Code `id=\"fn1\"` sans note.\n"})
	d, err := New(p).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.HTML, `id="fn1"`) || strings.Contains(d.HTML, "gl-f0-fn1") {
		t.Fatalf("code en ligne réécrit : %s", d.HTML)
	}
}

// A failed run of pandoc is not kept: once pandoc works again, the next
// render succeeds without the file having changed (revue de Codex).
func TestPandocFailureIsNotKept(t *testing.T) {
	needPandoc(t)
	if runtime.GOOS == "windows" {
		t.Skip("enveloppe de pandoc en script shell")
	}
	p := mkproject(t, map[string]string{"a.md": "Texte a.\n"})
	broken := filepath.Join(t.TempDir(), "en-panne")
	os.WriteFile(broken, nil, 0o644)
	wrapper := filepath.Join(t.TempDir(), "pandoc")
	os.WriteFile(wrapper, []byte("#!/bin/sh\n[ -e "+broken+" ] && exit 1\nexec pandoc \"$@\"\n"), 0o755)
	p.Pandoc = wrapper
	r := New(p)
	if _, err := r.Map(context.Background(), 0, mustRead(t, p.Path(0))); err == nil {
		t.Fatal("pandoc en panne : pas d'erreur")
	}
	os.Remove(broken)
	if _, err := r.Map(context.Background(), 0, mustRead(t, p.Path(0))); err != nil {
		t.Fatalf("échec de pandoc gardé : %v", err)
	}
}
