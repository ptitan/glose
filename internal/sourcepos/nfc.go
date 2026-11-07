package sourcepos

import "sort"

// The canonical composition (NFC) that pandoc applies to its input (D-29),
// reproduced with the tables of nfc_tables.go, generated from the Unicode
// data by tools/gen-nfc/gen.py. It serves only to count the columns of
// pandoc: a stretch of text whose NFC differs from itself (an accent written
// apart, a Hangul syllable written in jamo, a character that NFC splits)
// has no exact position inside it, but the text after it does.

const (
	sBase, lBase, vBase, tBase = 0xAC00, 0x1100, 0x1161, 0x11A7
	lCount, vCount, tCount     = 19, 21, 28
	nCount                     = vCount * tCount
	sCount                     = lCount * nCount
)

// composeSecond holds the characters that may be the second element of a
// composition: they may join the character before them.
var composeSecond = func() map[rune]bool {
	m := map[rune]bool{}
	for k := range composeTable {
		m[k[1]] = true
	}
	for r := rune(vBase); r < vBase+vCount; r++ {
		m[r] = true
	}
	for r := rune(tBase + 1); r < tBase+tCount; r++ {
		m[r] = true
	}
	return m
}()

func ccc(r rune) uint8 { return cccTable[r] }

// assigned reports whether r is known to the tables: an unassigned character
// may be known to the pandoc in use, with properties of its own.
func assigned(r rune) bool {
	i := sort.Search(len(assignedRanges), func(i int) bool { return assignedRanges[i][1] >= r })
	return i < len(assignedRanges) && assignedRanges[i][0] <= r
}

func decompose(r rune, out []rune) []rune {
	if r >= sBase && r < sBase+sCount {
		s := r - sBase
		out = append(out, lBase+s/nCount, vBase+(s%nCount)/tCount)
		if t := s % tCount; t != 0 {
			out = append(out, tBase+t)
		}
		return out
	}
	if d, ok := decompTable[r]; ok {
		return append(out, d...)
	}
	return append(out, r)
}

// nfcBoundary reports whether NFC never joins r, nor what it decomposes
// into, with the text before it: a text may then be normalised in pieces cut
// before such characters. r is a starter (combining class 0), never the
// second element of a composition, and its canonical decomposition starts
// with a starter too (checked on the tables: that starter is never a second
// element either). r itself may change (U+0958 splits in two): the piece it
// starts is compared with its NFC form.
func nfcBoundary(r rune) bool {
	if r < 0x300 {
		return true // Latin and controls: starters, never second elements, unchanged by NFC
	}
	if ccc(r) != 0 || composeSecond[r] || !assigned(r) {
		return false
	}
	return ccc(decompose(r, nil)[0]) == 0
}

// nfc returns the canonical composition of s.
func nfc(s []rune) []rune {
	var d []rune
	for _, r := range s {
		d = decompose(r, d)
	}
	// Canonical ordering: a stable sort of each run of non-starters.
	for i := 0; i < len(d); {
		if ccc(d[i]) == 0 {
			i++
			continue
		}
		j := i
		for j < len(d) && ccc(d[j]) != 0 {
			j++
		}
		run := d[i:j]
		sort.SliceStable(run, func(a, b int) bool { return ccc(run[a]) < ccc(run[b]) })
		i = j
	}
	// Composition.
	out := make([]rune, 0, len(d))
	starter := -1
	var last uint8 // combining class of the last character appended after the starter
	for _, r := range d {
		c := ccc(r)
		if starter >= 0 {
			s := out[starter]
			blocked := len(out)-1 > starter && (last == 0 || last >= c)
			if !blocked {
				// Hangul: L + V, LV + T.
				if s >= lBase && s < lBase+lCount && r >= vBase && r < vBase+vCount {
					out[starter] = sBase + ((s-lBase)*vCount+(r-vBase))*tCount
					continue
				}
				if s >= sBase && s < sBase+sCount && (s-sBase)%tCount == 0 && r > tBase && r < tBase+tCount {
					out[starter] = s + (r - tBase)
					continue
				}
				if p, ok := composeTable[[2]rune{s, r}]; ok {
					out[starter] = p
					continue
				}
			}
		}
		if c == 0 {
			starter = len(out)
			last = 0
		} else {
			last = c
		}
		out = append(out, r)
	}
	return out
}
