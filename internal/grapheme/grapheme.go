// Package grapheme tells where a text may be cut without splitting what a
// reader sees as one character: a letter and its combining accents, an emoji
// and its skin tone or variation selector, a sequence joined by U+200D, a
// flag (two regional indicators), a Hangul syllable written in jamo, CR LF.
//
// It follows the rules of Unicode extended grapheme clusters (UAX #29),
// simplified, without tables outside the standard library (D-17): no break
// before a combining or spacing mark, a variation selector, an emoji modifier
// or a tag character; no break after U+200D; regional indicators go by pairs;
// the Hangul rules on jamo. The prepended concatenation marks of a few scripts
// (GB9b) are not handled. A boundary that this package refuses is never a
// real one; a few that it accepts may not be.
package grapheme

import (
	"unicode"
	"unicode/utf8"
)

const zwj = 0x200D

// extends reports whether r attaches to the character before it.
func extends(r rune) bool {
	switch {
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc):
		return true
	case r == zwj, r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF: // joiner, variation selectors
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // emoji modifiers (skin tones)
		return true
	case r >= 0xE0020 && r <= 0xE007F: // tags (subdivision flags)
		return true
	}
	return false
}

func regional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// Hangul syllable types.
const (
	none = iota
	hL
	hV
	hT
	hLV
	hLVT
)

func hangul(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F, r >= 0xA960 && r <= 0xA97C:
		return hL
	case r >= 0x1160 && r <= 0x11A7, r >= 0xD7B0 && r <= 0xD7C6:
		return hV
	case r >= 0x11A8 && r <= 0x11FF, r >= 0xD7CB && r <= 0xD7FB:
		return hT
	case r >= 0xAC00 && r <= 0xD7A3:
		if (r-0xAC00)%28 == 0 {
			return hLV
		}
		return hLVT
	}
	return none
}

// Boundary reports whether b may be cut at byte offset i: i is the start of
// a character (UTF-8) and does not fall inside a grapheme cluster. The start
// and the end of b are boundaries.
func Boundary(b []byte, i int) bool {
	if i <= 0 || i >= len(b) {
		return i == 0 || i == len(b)
	}
	if !utf8.RuneStart(b[i]) {
		return false
	}
	r, _ := utf8.DecodeRune(b[i:])
	p, _ := utf8.DecodeLastRune(b[:i])
	switch {
	case p == '\r' && r == '\n':
		return false
	case p == '\r' || p == '\n' || r == '\r' || r == '\n':
		return true
	case extends(r):
		return false
	case p == zwj:
		return false
	case regional(r) && regional(p):
		// Pairs: no break inside a pair, counted from the start of the run.
		n := 0
		for j := i; j > 0; {
			q, size := utf8.DecodeLastRune(b[:j])
			if !regional(q) {
				break
			}
			n++
			j -= size
		}
		return n%2 == 0
	}
	switch hp, hr := hangul(p), hangul(r); {
	case hp == hL && (hr == hL || hr == hV || hr == hLV || hr == hLVT),
		(hp == hLV || hp == hV) && (hr == hV || hr == hT),
		(hp == hLVT || hp == hT) && hr == hT:
		return false
	}
	return true
}
