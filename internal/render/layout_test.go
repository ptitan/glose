package render

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// D-66 (P4.7): the declarative layout rules number the paragraphs from one
// file to the next, under the titles that contain « — », skip a lead in
// italics, turn a quotation with a bold prefix into a box, and fill the
// references; the words keep their provenance.
func TestLayoutRules(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{
		"glose.json": `{"files":["a.md","b.md"],"layout":{"numbers":{"within":"—","skipItalic":true},` +
			`"boxes":[{"prefix":"À retenir","class":"retenir"}],"references":true}}`,
		"a.md": "# Avant-propos\n\nPas numéroté.\n\n# Première partie — Le sujet\n\n*Un chapeau.*\n\n" +
			"{#p-un} Premier paragraphe.\n\nDeuxième, voir [](#p-trois) et [](#p-absent).\n\n" +
			"> **À retenir** — une chose.\n\n> **Autre** — pas un encadré.\n",
		"b.md": "# Deuxième partie — Suite\n\n{#p-trois} Troisième paragraphe, renvoi au [](#p-un).\n",
	})
	if p.PerFile {
		t.Fatal("numéros d'un fichier à l'autre : rendu d'un seul tenant attendu")
	}
	d, err := New(p).Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := d.HTML
	nums := regexp.MustCompile(`<span(?: id="([^"]*)")? class="numpar" data-n="(\d+)">(\d+)</span>`).FindAllStringSubmatch(h, -1)
	if len(nums) != 3 || nums[0][2] != "1" || nums[2][2] != "3" || !strings.HasSuffix(nums[0][1], "p-un") || !strings.HasSuffix(nums[2][1], "p-trois") {
		t.Fatalf("numéros : %q\n%s", nums, h)
	}
	if strings.Contains(h, "{#p-un}") || strings.Contains(h, "{#p-trois}") {
		t.Fatal("étiquette affichée")
	}
	if !regexp.MustCompile(`<a href="#[^"]*p-trois" class="renvoi">3</a>`).MatchString(h) ||
		!regexp.MustCompile(`<a href="#[^"]*p-un" class="renvoi">1</a>`).MatchString(h) ||
		!regexp.MustCompile(`<a href="#[^"]*p-absent" class="renvoi-inconnu renvoi">\?</a>`).MatchString(h) {
		t.Fatalf("renvois :\n%s", h)
	}
	if strings.Count(h, `class="encadre retenir"`) != 1 || strings.Count(h, "<blockquote>") != 1 {
		t.Fatalf("encadrés :\n%s", h)
	}
	// Words keep their provenance: every word shown is the bytes of its file.
	checkContract(t, p, d)
	// Without rules: nothing of the above.
	plain := mkproject(t, map[string]string{"a.md": "{#p-un} Premier.\n\n> **À retenir** — x.\n"})
	d2, err := New(plain).Render(context.Background())
	if err != nil || strings.Contains(d2.HTML, "numpar") || strings.Contains(d2.HTML, "encadre") {
		t.Fatalf("sans règles : %v\n%s", err, d2.HTML)
	}
}

// Boxes alone stay local: the HTML is still made file by file, and kept by
// the rules too (a change of the rules is not served from the cache).
func TestLayoutBoxesAloneStayPerFile(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{
		"glose.json": `{"layout":{"boxes":[{"prefix":"Note","class":"note"}]}}`,
		"a.md":       "> **Note** — x.\n",
	})
	if !p.PerFile {
		t.Fatal("encadrés seuls : rendu fichier par fichier attendu")
	}
	r := New(p)
	d, err := r.Render(context.Background())
	if err != nil || !strings.Contains(d.HTML, `class="encadre note"`) {
		t.Fatalf("%v\n%s", err, d.HTML)
	}
	r.P.Layout.Boxes[0].Class = "autre"
	d, err = r.Render(context.Background())
	if err != nil || !strings.Contains(d.HTML, `class="encadre autre"`) {
		t.Fatalf("HTML gardé malgré des règles changées : %v\n%s", err, d.HTML)
	}
}
