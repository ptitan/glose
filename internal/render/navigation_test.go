package render

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestImagesRelativeToTheirMarkdownFile(t *testing.T) {
	needPandoc(t)
	src := "# Texte\n\n![Figure](img/figure%20une.png?x=1#detail)\n\n![Autre](../commun.png)\n"
	p := mkproject(t, map[string]string{"chapitre/texte.md": src})
	r := New(p)
	d, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`src="chapitre/img/figure%20une.png?x=1#detail"`, `src="commun.png"`} {
		if !strings.Contains(d.HTML, want) {
			t.Fatalf("image mal résolue : %s", d.HTML)
		}
	}
	h, err := r.Preview(context.Background(), 0, []byte(src))
	if err != nil || !strings.Contains(h, `src="chapitre/img/figure%20une.png?x=1#detail"`) {
		t.Fatalf("aperçu : %v %s", err, h)
	}
	b, _ := os.ReadFile(p.Path(0))
	if string(b) != src {
		t.Fatal("source modifiée")
	}
	checkContract(t, p, d)
}

func TestAnchorsBelongToTheirFile(t *testing.T) {
	needPandoc(t)
	src := "# Introduction\n\nVoir [le début](#introduction).\n\n`id=\"gl-introduction\"`\n"
	p := mkproject(t, map[string]string{"a.md": src, "b.md": src})
	r := New(p)
	d, err := r.Render(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := range p.Files {
		id := AnchorID(i, "gl-introduction")
		if strings.Count(d.HTML, `id="`+id+`"`) != 1 || !strings.Contains(d.HTML, `href="#`+id+`"`) {
			t.Fatal(d.HTML)
		}
	}
	if strings.Count(d.HTML, `<code>id="gl-introduction"</code>`) != 2 {
		t.Fatal("code modifié")
	}
	part, err := r.RenderPart(context.Background(), 0, "b.md")
	if err != nil || !strings.Contains(part.HTML, `id="`+AnchorID(1, "gl-introduction")+`"`) {
		t.Fatalf("ancre instable : %v", err)
	}
	checkContract(t, p, d)
}

func TestNamespaceChangesOnlyAnchorAttributes(t *testing.T) {
	h := []byte(`<p data-id="gl-mot" data-href="#gl-mot"><a href="#gl-mot" title="id=&quot;gl-mot&quot;">lien</a><span id="gl-mot" data-g="0:1,4">mot</span><code>id="gl-mot"</code></p>`)
	got := string(namespaceAnchors(h, 2))
	for _, want := range []string{`data-id="gl-mot"`, `data-href="#gl-mot"`, `title="id=&quot;gl-mot&quot;"`, `<code>id="gl-mot"</code>`, `id="gl-f2-a-gl-mot"`, `href="#gl-f2-a-gl-mot"`, `data-g="0:1,4"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("attribut incorrect : %s", got)
		}
	}
}
