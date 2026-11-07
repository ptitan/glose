package sourcemap

import (
	"sync"
	"testing"
)

// A map is shared by the concurrent requests of the server (GET /api/passage
// is not serialised by the mutation lock): Check must not write into it.
// Run with -race.
func TestConcurrentChecksOnASharedMap(t *testing.T) {
	src := []byte("un **gras** mot\n")
	var m Map
	b := m.Add(Item{Kind: Block, Start: 0, End: len(src), Parent: -1})
	m.Add(Item{Kind: Word, Start: 0, End: 2, Certified: true, Parent: b})
	m.Add(Item{Kind: Space, Start: 2, End: 3, Certified: true, Parent: b})
	c := m.Add(Item{Kind: Container, Start: 3, End: 11, Parent: b})
	m.Add(Item{Kind: Word, Start: 5, End: 9, Certified: true, Parent: c})
	m.Add(Item{Kind: Space, Start: 11, End: 12, Certified: true, Parent: b})
	m.Add(Item{Kind: Word, Start: 12, End: 15, Certified: true, Parent: b})
	m.Finish()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				// Inside the bold words: kept as is.
				if r, err := m.Check(src, 5, 9); err != nil || r.Widened || r.Start != 5 || r.End != 9 {
					t.Errorf("Check(5, 9) = %+v, %v", r, err)
					return
				}
				// Cuts through the bold: widened to its delimiters.
				if r, err := m.Check(src, 0, 9); err != nil || !r.Widened || r.Start != 0 || r.End != 11 {
					t.Errorf("Check(0, 9) = %+v, %v", r, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
