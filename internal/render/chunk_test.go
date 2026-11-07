package render

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/sourcemap"
)

// bigFile writes a file of n sections, with footnotes defined at the end,
// a link reference, a code block holding a fake heading and a fenced div.
func bigFile(n int) string {
	var b strings.Builder
	b.WriteString("Introduction avec un [lien][ref] et une note[^n0].\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "# Section %d\n\nTexte de la section %d, avec *emphase* et une note[^n%d].\n\n", i, i, i)
		if i%3 == 0 {
			b.WriteString("```\n# pas un titre\n\n# toujours du code\n```\n\n")
		}
		if i%4 == 0 {
			b.WriteString("::: encadre\nDans l'encadré.\n\n# titre dans l'encadré\n\nSuite.\n:::\n\n")
		}
		b.WriteString("- un point\n- un autre\n\n")
		if i == 9 {
			b.WriteString("# Titre dans l'encadré-5\n\nVoir [l'ancre](#titre-dans-lencadré-5) et [l'explicite](#mon-id).\n\n# Explicite {#mon-id}\n\nTexte.\n\n")
			// Written by the author, with the form of an automatic one.
			b.WriteString("# Section 3 {#section-3-7}\n\nVoir [là](#section-3-7).\n\n")
			// Its identifier is that of an earlier title: pandoc keeps it
			// and numbers the earlier one (revue de Codex).
			b.WriteString("# Section 2 {#section-2}\n\nVoir [ici](#section-2).\n\n")
		}
	}
	b.WriteString("[ref]: https://exemple.org\n\n[^n0]: Note d'introduction.\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "[^n%d]: Note %d,\n    sur deux lignes.\n", i, i)
	}
	return b.String()
}

func smallChunks(t *testing.T) {
	t.Helper()
	oldMin, oldTarget := chunkMin, chunkTarget
	t.Cleanup(func() { chunkMin, chunkTarget = oldMin, oldTarget })
	chunkMin, chunkTarget = 1, 300
}

var footnoteBits = regexp.MustCompile(`(?s)<aside id="[^"]*footnotes".*?</aside>\n?|<a href="#[^"]*" class="footnote-ref" id="[^"]*" role="doc-noteref"><sup>\d+</sup></a>`)

func withoutNotes(h string) string {
	h = footnoteBits.ReplaceAllString(h, "")
	return regexp.MustCompile(`\n+`).ReplaceAllString(h, "\n")
}

func words(m *sourcemap.Map) []string {
	var w []string
	for _, it := range m.Items {
		if it.Kind == sourcemap.Word || it.Kind == sourcemap.Space {
			w = append(w, fmt.Sprintf("%d:%d:%v", it.Start, it.End, it.Certified))
		}
	}
	sort.Strings(w)
	return w
}

// P6.12 : un fichier découpé en sections se rend comme d'un seul tenant
// (hors place des notes) ; notes numérotées sur tout le fichier ; mêmes
// mots, aux mêmes positions ; aucune coupe dans du code ou un encadré.
func TestChunkedFileRendersAsAWhole(t *testing.T) {
	needPandoc(t)
	src := bigFile(12)
	whole := mkproject(t, map[string]string{"a.md": src})
	dw, err := New(whole).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	smallChunks(t)
	p := mkproject(t, map[string]string{"a.md": src})
	r := New(p)
	dc, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	res := r.stage1[p.Files[0]+"\x000\x00"+mustRead(t, p.Path(0)).Hash]
	if res == nil || len(res.parts) < 3 {
		t.Fatalf("fichier non découpé : %v", res)
	}
	if withoutNotes(dc.HTML) != withoutNotes(dw.HTML) {
		t.Fatalf("rendu découpé différent hors notes :\n%s\n----\n%s", withoutNotes(dc.HTML), withoutNotes(dw.HTML))
	}
	sups := regexp.MustCompile(`role="doc-noteref"><sup>(\d+)</sup>`).FindAllStringSubmatch(dc.HTML, -1)
	for k, m := range sups {
		if m[1] != fmt.Sprint(k+1) {
			t.Fatalf("note %d numérotée %s", k+1, m[1])
		}
	}
	if len(sups) != 13 {
		t.Fatalf("%d notes, 13 attendues", len(sups))
	}
	ids := map[string]int{}
	for _, m := range regexp.MustCompile(`id="([^"]+)"`).FindAllStringSubmatch(dc.HTML, -1) {
		ids[m[1]]++
	}
	for _, m := range regexp.MustCompile(`href="#([^"]+)"`).FindAllStringSubmatch(dc.HTML, -1) {
		if ids[m[1]] != 1 {
			t.Errorf("lien vers %q : %d cibles", m[1], ids[m[1]])
		}
	}
	checkContract(t, p, dc)
	mw, _ := New(whole).Map(context.Background(), 0, mustRead(t, whole.Path(0)))
	mc, _ := r.Map(context.Background(), 0, mustRead(t, p.Path(0)))
	if strings.Join(words(mw), " ") != strings.Join(words(mc), " ") {
		t.Fatal("mots de la carte différents")
	}
	for _, c := range splitChunks([]byte(src)) {
		head := src[c.start:min(c.end, c.start+40)]
		if c.start > 0 && (!strings.HasPrefix(head, "# ") || strings.Contains(head, "encadré\n") || strings.HasPrefix(head, "# pas") || strings.HasPrefix(head, "# toujours")) {
			t.Errorf("coupe ailleurs que devant un titre hors code et encadré : %q", head)
		}
	}
}

// Revue de Codex (P6) : une clôture de code suivie d'espaces ferme bien le
// bloc ; une définition de lien dont l'adresse est à la ligne suivante fait
// rendre le fichier d'un seul tenant ; son titre sur la ligne suivante est
// gardé avec elle.
func TestChunkDefinitionsEdgeCases(t *testing.T) {
	defs, ok := definitions([]byte("```\ncode\n```   \n\n[^a]: Note a.\n"))
	if !ok || len(defs) != 1 {
		t.Fatalf("clôture suivie d'espaces : %v %v", defs, ok)
	}
	if _, ok := definitions([]byte("Texte.\n\n[r]:\n  https://exemple.org\n")); ok {
		t.Fatal("adresse à la ligne suivante acceptée")
	}
	defs, ok = definitions([]byte("[r]: https://exemple.org\n  \"Titre\"\nTexte.\n"))
	if !ok || len(defs) != 1 || defs[0].last != 2 {
		t.Fatalf("titre à la ligne suivante : %v", defs)
	}
	// A title over several lines (revue de Codex) : rendu d'un seul tenant.
	for _, src := range []string{
		"[r]: https://exemple.org \"titre\nsur deux lignes\"\n",
		"[r]: https://exemple.org\n  \"titre\nsur deux lignes\"\n",
		"[r]: <https://exemple.org/a b> (titre\nsuite)\n",
		"[r]: /url \"titre \\\"\nsuite\"\n",
	} {
		if _, ok := definitions([]byte(src)); ok {
			t.Errorf("titre sur plusieurs lignes accepté : %q", src)
		}
	}
	for _, src := range []string{
		"[r]: https://exemple.org \"Titre\"\n",
		"[r]: <https://exemple.org/a b> 'Titre'\n",
		"[r]: https://exemple.org\n",
		"[r]: /url \"titre \\\\\"\n",
	} {
		if defs, ok := definitions([]byte(src)); !ok || len(defs) != 1 {
			t.Errorf("définition sur une ligne refusée : %q", src)
		}
	}
}

// P6.12 : après une modification dans une section, seule elle repasse par
// pandoc (lecture et HTML), et les coupes suivantes ne bougent pas ; le
// cache disque garde les sections.
func TestChunkedFileChangeIsLocal(t *testing.T) {
	needPandoc(t)
	smallChunks(t)
	wrapper, count := withCounter(t)
	src := bigFile(12)
	p := mkproject(t, map[string]string{"a.md": src})
	p.Pandoc = wrapper
	r := New(p)
	r.EnableDiskCache(context.Background())
	first, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := splitChunks([]byte(src))
	changed := strings.Replace(src, "Texte de la section 2,", "Texte bien plus long de la deuxième section,", 1)
	os.WriteFile(p.Path(0), []byte(changed), 0o644)
	n0 := count()
	d, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := count() - n0; n != 2 {
		t.Fatalf("après une modification locale : %d appels à pandoc, 2 attendus", n)
	}
	checkContract(t, p, d)
	after := splitChunks([]byte(changed))
	delta := len(changed) - len(src)
	if len(after) != len(before) {
		t.Fatalf("coupes : %d avant, %d après", len(before), len(after))
	}
	for k := 2; k < len(after); k++ {
		if after[k].start != before[k].start+delta {
			t.Fatalf("coupe %d déplacée", k)
		}
	}
	// Réouverture : tout vient du cache disque.
	os.WriteFile(p.Path(0), []byte(src), 0o644)
	r2 := New(p)
	r2.EnableDiskCache(context.Background())
	if _, err := r2.Render(context.Background()); err != nil {
		t.Fatal(err)
	}
	r3 := New(p)
	r3.EnableDiskCache(context.Background())
	n0 = count()
	again, err := r3.Render(context.Background())
	if err != nil || count()-n0 != 0 || again.HTML != first.HTML {
		t.Fatalf("réouverture : %v, %d appels, identique %v", err, count()-n0, again.HTML == first.HTML)
	}
}
