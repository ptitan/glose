package render

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/project"
)

// TestMeasure renders the project given by GLOSE_MEASURE (N6) and checks the
// provenance contract on it. Skipped without that variable.
func TestMeasure(t *testing.T) {
	dir := os.Getenv("GLOSE_MEASURE")
	if dir == "" {
		t.Skip("GLOSE_MEASURE non défini")
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := New(p)
	for i := 0; i < 2; i++ {
		start := time.Now()
		doc, err := r.Render(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		n := checkContract(t, p, doc)
		t.Logf("rendu %d : %v, %d mots repérés, HTML de %d Ko, %d annotations", i+1, time.Since(start), n, len(doc.HTML)/1024, len(doc.Annotations))
	}
}
