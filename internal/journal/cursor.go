package journal

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// LastSeq reads the cursor used by the interface's disk watcher. The usual
// case reads at most 64 KiB, rather than decoding years of archived messages
// every two seconds. No state is cached: replacements and truncations are seen.
// A very large last event falls back to the complete reader. The authoritative
// history and all mutations still use Read and its full anomaly checks.
func (j *Journal) LastSeq() (int64, error) {
	f, err := os.Open(j.Path())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	start := max(int64(0), info.Size()-(64<<10))
	b := make([]byte, info.Size()-start)
	n, err := f.ReadAt(b, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	b = b[:n]
	if start > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			b = nil
		}
	}
	events, _, _ := parse(b)
	if len(events) > 0 {
		return events[len(events)-1].Seq, nil
	}
	if start == 0 {
		return 0, nil
	}
	events, _, err = j.Read()
	if len(events) > 0 {
		return events[len(events)-1].Seq, err
	}
	return 0, err
}
