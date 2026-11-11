package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApproveAndCheck(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("GLOSE_CONFIG_DIR", cfg)
	root := t.TempDir()
	filter := filepath.Join(root, "lua", "f.lua")
	os.MkdirAll(filepath.Dir(filter), 0o755)
	os.WriteFile(filter, []byte("return {}\n"), 0o644)

	if n := Of(root, "", ""); !n.Empty() {
		t.Fatalf("rien à approuver : %+v", n)
	}
	if p, f := Check(root, Of(root, "pandoc", "")); !p || !f {
		t.Fatal("pandoc du PATH à approuver")
	}
	n := Of(root, "/opt/pandoc/bin/pandoc", filter)
	if n.Filter != "lua/f.lua" || n.FilterHash == "" || n.Pandoc != "/opt/pandoc/bin/pandoc" {
		t.Fatalf("%+v", n)
	}
	if p, f := Check(root, n); p || f {
		t.Fatal("approuvé sans geste")
	}
	if ok, err := Approve(root, n); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, err := Approve(root, n); ok || err != nil {
		t.Fatalf("déjà approuvé : %v %v", ok, err)
	}
	if p, f := Check(root, n); !p || !f {
		t.Fatal("approbation non retrouvée")
	}
	// Another project: nothing.
	if p, f := Check(t.TempDir(), n); p || f {
		t.Fatal("approbation d'un autre projet")
	}
	// The filter changed: the program stays approved, not the filter.
	os.WriteFile(filter, []byte("os.execute('x')\n"), 0o644)
	if p, f := Check(root, Of(root, "/opt/pandoc/bin/pandoc", filter)); !p || f {
		t.Fatalf("filtre changé : %v %v", p, f)
	}
	// Unreadable filter: never approved.
	if _, err := Approve(root, Of(root, "", filepath.Join(root, "absent.lua"))); err == nil {
		t.Fatal("filtre illisible approuvé")
	}
	// An unreadable file of approvals approves nothing, and is not replaced.
	os.WriteFile(filepath.Join(cfg, fileName), []byte("{abîmé"), 0o600)
	if p, f := Check(root, n); p || f {
		t.Fatal("approbations illisibles")
	}
	if _, err := Approve(root, Of(root, "", filter)); err == nil {
		t.Fatal("approbations illisibles écrasées")
	}
}

func TestInsideProject(t *testing.T) {
	root := t.TempDir()
	for pandoc, want := range map[string]bool{
		"pandoc": false, "pandoc-3": false, "/usr/bin/pandoc": false,
		"./bin/pandoc": true, "bin/pandoc": true, filepath.Join(root, "x", "pandoc"): true, "../pandoc": false,
	} {
		if got := InsideProject(root, pandoc); got != want {
			t.Errorf("%s : %v", pandoc, got)
		}
	}
	if _, err := Approve(root, Need{Pandoc: "./bin/pandoc"}); err != ErrInside {
		t.Fatalf("%v", err)
	}
}
