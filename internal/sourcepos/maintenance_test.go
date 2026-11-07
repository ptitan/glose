package sourcepos

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// P5.7 : les tables NFC et leurs vecteurs sont exactement ce que produit
// le générateur (tools/gen-nfc/gen.py) avec le Python de la machine, quand
// son Unicode est celui des tables. Une modification à la main, ou un
// générateur changé sans régénérer, est ainsi détectée. Procédure :
// docs/UNICODE.md.
func TestNFCTablesMatchTheGenerator(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 absent")
	}
	out, err := exec.Command(py, "-I", "-c", "import unicodedata; print(unicodedata.unidata_version)").Output()
	if err != nil {
		t.Skip("python3 inutilisable")
	}
	if v := strings.TrimSpace(string(out)); v != nfcUnicodeVersion {
		t.Skipf("Python a Unicode %s, les tables %s : régénérer seulement si pandoc suit (docs/UNICODE.md)", v, nfcUnicodeVersion)
	}
	gen := filepath.Join("..", "..", "tools", "gen-nfc", "gen.py")
	raw, err := exec.Command(py, "-I", gen).Output()
	if err != nil {
		t.Fatal(err)
	}
	tables, err := format.Source(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile("nfc_tables.go"); !bytes.Equal(got, tables) {
		t.Error("nfc_tables.go diffère de la sortie du générateur : régénérer (docs/UNICODE.md)")
	}
	vectors, err := exec.Command(py, "-I", gen, "--vectors").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join("testdata", "nfc.txt")); !bytes.Equal(got, vectors) {
		t.Error("testdata/nfc.txt diffère de la sortie du générateur : régénérer (docs/UNICODE.md)")
	}
}

// P5.7 : un caractère qu'une version d'Unicode plus récente que les tables
// a attribué (U+1CC00, Unicode 16) n'est pas deviné : la suite de sa ligne
// est inexacte, donc annotable au paragraphe seulement. Le test vérifie
// aussi que ce caractère est bien inconnu des tables et connu de Go, dont le
// découpage en graphèmes suit les tables plus récentes.
func TestCharacterNewerThanTheTablesIsNotGuessed(t *testing.T) {
	const r = 0x1CC00
	if assigned(r) {
		t.Skip("les tables connaissent désormais U+1CC00 : choisir un caractère plus récent")
	}
	if !unicode.IsGraphic(r) {
		t.Skipf("Unicode %s de Go ne connaît pas U+1CC00", unicode.Version)
	}
	x, err := NewIndex([]byte("\U0001CC00 z"))
	if err != nil {
		t.Fatal(err)
	}
	if _, exact, err := x.Offset(Pos{1, 3}); err != nil || exact {
		t.Fatalf("position après U+1CC00 tenue pour exacte (err %v)", err)
	}
	t.Logf("tables NFC : Unicode %s ; Go (graphèmes) : Unicode %s", nfcUnicodeVersion, unicode.Version)
}
