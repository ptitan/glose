package journal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLastSeqMatchesTheFullReader(t *testing.T) {
	j := Open(t.TempDir())
	if seq, err := j.LastSeq(); err != nil || seq != 0 {
		t.Fatalf("absent : %d %v", seq, err)
	}
	line := func(n int, note string) string {
		b, _ := json.Marshal(Event{Seq: int64(n), Type: Turn, TurnEv: &TurnInfo{Note: note}})
		return string(b) + "\n"
	}
	long := line(2, strings.Repeat("message long ", 10000))
	for _, src := range []string{"", line(1, "") + line(2, ""), strings.TrimSuffix(line(1, ""), "\n"), line(1, "") + `{"seq":2`, line(1, "") + long, line(1, "") + long + line(3, "") + "cassé\n", line(1, "") + long + line(3, ""), line(9, "")} {
		if err := os.WriteFile(j.Path(), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(j.Path())
		events, _, _ := j.Read()
		var want int64
		if len(events) > 0 {
			want = events[len(events)-1].Seq
		}
		got, err := j.LastSeq()
		if err != nil || got != want {
			t.Fatalf("curseur : %d, attendu %d, %v", got, want, err)
		}
		after, _ := os.ReadFile(j.Path())
		if !bytes.Equal(before, after) {
			t.Fatal("journal modifié")
		}
	}
}

// Reproducible measurement: go test ./internal/journal -run '^$' -bench
// BenchmarkLongJournalPolling -benchmem -benchtime=5x.
func BenchmarkLongJournalPolling(b *testing.B) {
	j := Open(b.TempDir())
	var data bytes.Buffer
	for i := 1; i <= 20000; i++ {
		fmt.Fprintf(&data, "{\"seq\":%d,\"type\":\"committed\",\"reason\":%q}\n", i, strings.Repeat("Archive de discussion. ", 20))
	}
	if err := os.WriteFile(j.Path(), data.Bytes(), 0o600); err != nil {
		b.Fatal(err)
	}
	b.Run("Read", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := j.Read(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("LastSeq", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if seq, err := j.LastSeq(); err != nil || seq != 20000 {
				b.Fatalf("%d %v", seq, err)
			}
		}
	})
}
