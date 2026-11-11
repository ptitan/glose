package review

import (
	"bytes"
	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/patch"
	"testing"
)

func TestTaskMetadataRoundtripAndFidelity(t *testing.T) {
	m := Meta{ID: "abcd", By: Agent, Via: "Sol", Task: "rewrite"}
	b := m.Bytes()
	got, e := ParseMeta(b[3 : len(b)-3])
	if e != nil || got != m {
		t.Fatal(got, e)
	}
	if _, e := ParseMeta([]byte("glose:v1 id=abcd task=../../x")); e == nil {
		t.Fatal("mission invalide")
	}
	src := []byte("\ufeffAvant\r\nUn mot.\n{>>Ancien<<}{>>glose:v1 id=efgh by=author<<}\r\n")
	at := bytes.Index(src, []byte("mot"))
	p, _, e := Propose(src, critic.Span{Start: at, End: at + 3}, "texte", "", Agent, NewGen(nil))
	if e != nil {
		t.Fatal(e)
	}
	p, e = LabelTask(src, p, "rewrite")
	if e != nil {
		t.Fatal(e)
	}
	out := p.Apply(src)
	a := Read(out).Annotations
	if a[0].Task != "rewrite" || a[1].Task != "" {
		t.Fatal(a)
	}
	pe, e := patch.Reject(out, Read(out).Marks, critic.Group(Read(out).Marks)[0])
	if e != nil || !bytes.Equal(pe.Apply(out), src) {
		t.Fatalf("fidélité %s %v", out, e)
	}
	p = patch.Edit{Start: 0, End: 0}
	same, e := LabelTask(src, p, "")
	if e != nil || !bytes.Equal(same.Apply(src), src) {
		t.Fatal("sans mission")
	}
}
