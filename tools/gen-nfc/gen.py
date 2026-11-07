#!/usr/bin/env python3
"""Génère internal/sourcepos/nfc_tables.go à partir des tables Unicode de
Python (module unicodedata) : classes combinantes, décompositions canoniques
complètes, compositions primaires et caractères attribués. Ces tables servent
à reproduire la normalisation NFC que pandoc applique au texte (D-29).

Usage : python3 tools/gen-nfc/gen.py | gofmt > internal/sourcepos/nfc_tables.go
Les vecteurs de test : python3 tools/gen-nfc/gen.py --vectors > internal/sourcepos/testdata/nfc.txt
"""
import random
import sys
import unicodedata as u

SBASE, LBASE, VBASE, TBASE = 0xAC00, 0x1100, 0x1161, 0x11A7
SCOUNT = 11172


def hangul(cp):
    return SBASE <= cp < SBASE + SCOUNT


def canonical(cp):
    d = u.decomposition(chr(cp))
    if not d or d.startswith("<"):
        return None
    return [int(x, 16) for x in d.split()]


def full(cp):
    d = canonical(cp)
    if d is None:
        return [cp]
    out = []
    for x in d:
        out += full(x)
    return out


def tables():
    ccc, decomp, compose = {}, {}, {}
    assigned = []
    for cp in range(0x110000):
        c = chr(cp)
        if 0xD800 <= cp <= 0xDFFF:
            continue
        if u.category(c) != "Cn":
            assigned.append(cp)
        k = u.combining(c)
        if k:
            ccc[cp] = k
        if hangul(cp):
            continue
        d = canonical(cp)
        if d is None:
            continue
        decomp[cp] = full(cp)
        # Primary composite: two-element decomposition that NFC recomposes.
        if len(d) == 2 and u.normalize("NFC", c) == c and u.normalize("NFC", chr(d[0]) + chr(d[1])) == c:
            compose[(d[0], d[1])] = cp
    return ccc, decomp, compose, assigned


def ranges(cps):
    out = []
    for cp in cps:
        if out and out[-1][1] == cp - 1:
            out[-1][1] = cp
        else:
            out.append([cp, cp])
    return out


def go():
    ccc, decomp, compose, assigned = tables()
    w = sys.stdout.write
    w("// Code généré par tools/gen-nfc/gen.py ; NE PAS MODIFIER À LA MAIN.\n")
    w("// Tables Unicode %s (module unicodedata de Python).\n\n" % u.unidata_version)
    w("package sourcepos\n\n")
    w("const nfcUnicodeVersion = \"%s\"\n\n" % u.unidata_version)
    w("// cccTable gives the canonical combining class of the characters whose class is not 0.\n")
    w("var cccTable = map[rune]uint8{\n")
    for cp in sorted(ccc):
        w("\t0x%04X: %d,\n" % (cp, ccc[cp]))
    w("}\n\n")
    w("// decompTable gives the full canonical decomposition of a character (Hangul syllables excepted: computed).\n")
    w("var decompTable = map[rune][]rune{\n")
    for cp in sorted(decomp):
        w("\t0x%04X: {%s},\n" % (cp, ", ".join("0x%04X" % x for x in decomp[cp])))
    w("}\n\n")
    w("// composeTable gives the primary composite of two characters (Hangul excepted: computed).\n")
    w("var composeTable = map[[2]rune]rune{\n")
    for (a, b) in sorted(compose):
        w("\t{0x%04X, 0x%04X}: 0x%04X,\n" % (a, b, compose[(a, b)]))
    w("}\n\n")
    w("// assignedRanges are the characters known to these tables (category other than Cn).\n")
    w("var assignedRanges = [][2]rune{\n")
    for a, b in ranges(assigned):
        w("\t{0x%04X, 0x%04X},\n" % (a, b))
    w("}\n")


def vectors():
    """Random strings rich in combining marks, with their NFC, one per line:
    hexadecimal code points of the input, a tab, those of the output."""
    rnd = random.Random(29)
    pool = [0x61, 0x65, 0x6F, 0x71, 0x20, 0x41, 0x55, 0x3B1, 0x3C9, 0x915, 0x958, 0x93C, 0x94D, 0xB47, 0xB3E, 0xB57,
            0x1100, 0x1161, 0x11A8, 0xAC00, 0xAC01, 0x300, 0x301, 0x302, 0x308, 0x323, 0x327, 0x328, 0x345, 0x5B0,
            0x5B7, 0x5BC, 0x5D0, 0x5E9, 0x5C1, 0x1E0B, 0x1E63, 0xE9, 0x212B, 0x2126, 0x30AB, 0x3099, 0x309A,
            0xFE0F, 0x200D, 0x1F44D, 0x1F3FD, 0x0F71, 0x0F72, 0x0F80, 0x0F40, 0x0FB5, 0x1D15E, 0x1D165, 0x1D16E]
    for _ in range(3000):
        s = "".join(chr(rnd.choice(pool)) for _ in range(rnd.randint(1, 8)))
        n = u.normalize("NFC", s)
        print(" ".join("%X" % ord(c) for c in s) + "\t" + " ".join("%X" % ord(c) for c in n))


if __name__ == "__main__":
    if "--vectors" in sys.argv:
        vectors()
    else:
        go()
