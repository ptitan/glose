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
	b, base, err := j.tail()
	if err != nil {
		return 0, err
	}
	events, _, _ := parse(b)
	if len(events) > 0 {
		return events[len(events)-1].Seq, nil
	}
	if base == 0 {
		return 0, nil
	}
	events, _, err = j.Read()
	if len(events) > 0 {
		return events[len(events)-1].Seq, err
	}
	return 0, err
}

// tailSize is how much of the end of the journal is read for its last event.
const tailSize = 64 << 10

// tail returns the end of the journal, from the start of a line (the whole
// journal if it is short), and the offset of that start in the file; nothing
// for a journal that does not exist yet.
func (j *Journal) tail() ([]byte, int64, error) {
	f, err := os.Open(j.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	start := max(int64(0), info.Size()-tailSize)
	b := make([]byte, info.Size()-start)
	n, err := f.ReadAt(b, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, err
	}
	b = b[:n]
	if start > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b, start = b[i+1:], start+int64(i+1)
		} else {
			b, start = nil, start+int64(len(b))
		}
	}
	return b, start, nil
}
