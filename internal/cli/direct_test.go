package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
)

// D-57 : après la remise, l'agent trouve les modifications directes de
// l'auteur dans glose status et glose list, sans rien à décider ; glose log
// les montre.
func TestImposedChangesShownToTheAgent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Le chat dort.\n\nFin.\n"), 0o644)
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := navette.New(p)
	spec := &navette.Spec{Kind: navette.Direct}
	_, err = n.Run(review.Author, 0, "", spec, func(src []byte, gen *review.Gen, _ *navette.History) (patch.Edit, error) {
		e, _, err := review.Propose(src, critic.Span{Start: 0, End: 13}, "Le chien dort.", "", review.Author, gen)
		if err != nil {
			return patch.Edit{}, err
		}
		out, copies, err := navette.Impose(src, e.Apply(src))
		spec.Direct = copies
		return review.Diff(src, out), err
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(b) != "Le chien dort.\n\nFin.\n" {
		t.Fatalf("%q", b)
	}
	if _, err := n.Handoff("author", ""); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run("status", "--json", dir)
	var st struct {
		State navette.State `json:"state"`
	}
	json.Unmarshal([]byte(out), &st)
	if code != ExitOK || len(st.State.Imposed) != 1 || st.State.Imposed[0].New != "chien" || st.State.Imposed[0].File != "a.md" {
		t.Fatalf("status : %d %s", code, out)
	}
	if code, out, _ := run("status", dir); code != ExitOK || !strings.Contains(out, "Modifications imposées par l'auteur : 1") {
		t.Fatalf("status : %d %s", code, out)
	}
	code, out, _ = run("list", "--json", dir)
	var list struct {
		Annotations []any            `json:"annotations"`
		Imposed     []map[string]any `json:"imposed"`
	}
	json.Unmarshal([]byte(out), &list)
	if code != ExitOK || len(list.Annotations) != 0 || len(list.Imposed) != 1 || list.Imposed[0]["line"] != 1.0 {
		t.Fatalf("list : %d %s", code, out)
	}
	if code, out, _ := run("list", dir); code != ExitOK || !strings.Contains(out, "a.md:1  [imposé par l'auteur]  « chat » → « chien »") {
		t.Fatalf("list : %d %s", code, out)
	}
	if code, out, _ := run("log", dir); code != ExitOK || !strings.Contains(out, "modification directe de l'auteur") || !strings.Contains(out, "imposé") {
		t.Fatalf("log : %d %s", code, out)
	}
}
