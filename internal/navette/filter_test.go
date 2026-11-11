package navette

import (
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/projection"
)

// D-65: the batch decision under the filter of a mission decides the
// proposals of that mission only, those whose thread has a message of it,
// and the definition of a note with its reference.
func TestDecideFilteredByMission(t *testing.T) {
	src := []byte("Un{++ ajout A++}{>>glose:v1 id=aaaa by=agent task=relire<<} et{++ ajout B++}{>>glose:v1 id=bbbb by=agent task=autre<<}" +
		" et{-- retrait C--}{>>glose:v1 id=cccc by=agent<<}{>>Pour la mission.<<}{>>glose:v1 id=dddd re=cccc by=agent task=relire<<}.\n")
	e, err := DecideFiltered(src, 0, len(src), Accept, "", "relire")
	if err != nil {
		t.Fatal(err)
	}
	out := e.Apply(src)
	cur := string(projection.Project(out, projection.Current).Text)
	if !strings.Contains(cur, "Un ajout A et") || strings.Contains(cur, "ajout B") || strings.Contains(cur, "retrait C") {
		t.Fatalf("%q", cur)
	}
	if !strings.Contains(string(out), "{++ ajout B++}") {
		t.Fatalf("autre mission décidée : %s", out)
	}
	// Without a filter, as before: everything.
	e, _ = DecideFiltered(src, 0, len(src), Accept, "", "")
	if got := string(projection.Project(e.Apply(src), projection.Current).Text); !strings.Contains(got, "ajout B") || strings.Contains(got, "retrait C") {
		t.Fatalf("%q", got)
	}
	// A note added by the mission: its definition, without a mission of its
	// own, goes with its reference.
	note := []byte("Texte{++[^n]++}{>>glose:v1 id=eeee by=agent task=relire<<}.\n\n{++[^n]: La note.++}{>>glose:v1 id=ffff by=agent<<}\n")
	e, err = DecideFiltered(note, 0, len(note), Accept, "", "relire")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(e.Apply(note)); strings.Contains(got, "{++") {
		t.Fatalf("note coupée : %q", got)
	}
}
