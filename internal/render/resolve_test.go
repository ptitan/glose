package render

import (
	"context"
	"strings"
	"testing"
)

func TestResolveLinksAcrossUnshownFiles(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{
		"livre/a.md":        "# Introduction\n\nTexte.\n",
		"livre/b espace.md": "# Introduction\n\n## Unique {#cible}\n\nTexte et `id=\"gl-f2-a-gl-fantome\"`.\n",
		"autre.md":          "# Autre\n",
	})
	r := New(p)
	for _, tc := range []struct{ from, href, want, id string }{
		{"livre/a.md", "b%20espace.md#cible", "livre/b espace.md", "gl-cible"},
		{"livre/a.md", "#gl-cible", "livre/b espace.md", "gl-cible"},
		{"livre/b espace.md", "#gl-f2-a-gl-introduction", "livre/b espace.md", "gl-introduction"},
		{"livre/a.md", "../autre.md", "autre.md", ""},
	} {
		dst, err := r.Resolve(context.Background(), tc.from, tc.href, 0)
		if err != nil || dst.Path != tc.want || (tc.id != "" && !strings.HasSuffix(dst.Anchor, tc.id)) {
			t.Fatalf("%+v : %+v %v", tc, dst, err)
		}
	}
	for _, href := range []string{"../../secret.md", "https://example.org/a.md", "//example.org/a.md", "b%20espace.md#absent", "b%20espace.md#fantome"} {
		if _, err := r.Resolve(context.Background(), "livre/a.md", href, 0); err == nil {
			t.Errorf("lien accepté : %s", href)
		}
	}
	// The same anchor outside the source is ambiguous, never silently first.
	if _, err := r.Resolve(context.Background(), "autre.md", "#gl-introduction", 0); err == nil {
		t.Fatal("ancre ambiguë acceptée")
	}
}

func TestResolveWithAWholeProjectFilter(t *testing.T) {
	needPandoc(t)
	p := mkproject(t, map[string]string{"a.md": "# Début\n", "b.md": "Du code `id=\"gl-glose-f7\"`.\n\n# Suite {#suite}\n", "glose.json": `{"filter":"f.lua"}`, "f.lua": "function Para(p) return p end"})
	dst, err := New(p).Resolve(context.Background(), "a.md", "b.md#suite", 0)
	if err != nil || dst.Path != "b.md" || dst.Anchor != "gl-suite" {
		t.Fatalf("%+v %v", dst, err)
	}
}
