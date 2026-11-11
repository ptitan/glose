package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/trust"
)

type doctorOut struct {
	OK       bool            `json:"ok"`
	Findings []doctorFinding `json:"findings"`
}

func runDoctor(t *testing.T, dir string) (int, doctorOut) {
	t.Helper()
	code, out, errOut := run("doctor", "--json", dir)
	var d doctorOut
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("sortie illisible (%v) : %q %q", err, out, errOut)
	}
	return code, d
}

func finding(d doctorOut, subject string) *doctorFinding {
	for i := range d.Findings {
		if d.Findings[i].Subject == subject {
			return &d.Findings[i]
		}
	}
	return nil
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// snapshot lists every file of dir with its content: doctor must leave the
// project exactly as it found it.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err == nil {
			if fi.IsDir() {
				m[path] = "<dir>"
			} else {
				b, _ := os.ReadFile(path)
				m[path] = string(b)
			}
		}
		return nil
	})
	return m
}

func TestDoctorOnAPlainProject(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.md": "# Titre\n\nTexte.\n"})
	before := snapshot(t, dir)
	code, d := runDoctor(t, dir)
	if code != ExitOK || !d.OK {
		t.Fatalf("code %d : %+v", code, d)
	}
	for _, s := range []string{"glose", "pandoc", "profil", "rendu", ".glose", "serveur"} {
		if f := finding(d, s); f == nil || f.Level != "ok" {
			t.Errorf("%s : %+v", s, f)
		}
	}
	if f := finding(d, ".glose"); !strings.Contains(f.Message, "sera créé") {
		t.Errorf(".glose : %+v", f)
	}
	for _, f := range d.Findings {
		if f.Level != "ok" && f.Fix == "" {
			t.Errorf("problème sans correction : %+v", f)
		}
	}
	if after := snapshot(t, dir); len(after) != len(before) {
		t.Fatalf("doctor a laissé des fichiers : %v", after)
	}
}

func TestDoctorReportsABrokenFilterAndAnExternalStylesheet(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"a.md":       "Texte.\n",
		"glose.json": `{"filter": "f.lua", "css": "s.css"}`,
		"f.lua":      "function Para(el) return el.nimporte.quoi end\n",
		"s.css":      "@import url(\"https://example.org/x.css\");\nbody{}\n",
	})
	code, d := runDoctor(t, dir)
	if code != ExitProblem || d.OK {
		t.Fatalf("code %d : %+v", code, d)
	}
	if f := finding(d, "rendu"); f == nil || f.Level != "error" || !strings.Contains(f.Message, "f.lua") || f.Fix == "" {
		t.Errorf("rendu : %+v", f)
	}
	if f := finding(d, "feuille de style"); f == nil || f.Level != "warning" || !strings.Contains(f.Message, "extérieure") {
		t.Errorf("feuille de style : %+v", f)
	}
}

func TestDoctorReportsAMissingPandocAndAnUnreadableProfile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.md": "Texte.\n", "glose.json": `{"pandoc": "pandoc-qui-n-existe-pas"}`})
	code, d := runDoctor(t, dir)
	if code != ExitProblem {
		t.Fatalf("code %d", code)
	}
	if f := finding(d, "pandoc"); f == nil || f.Level != "error" || !strings.Contains(f.Fix, "glose.json") {
		t.Errorf("pandoc : %+v", f)
	}
	if finding(d, "rendu") != nil {
		t.Errorf("rendu tenté sans pandoc : %+v", d)
	}

	writeFiles(t, dir, map[string]string{"glose.json": `{"titre": "x"}`})
	code, d = runDoctor(t, dir)
	if f := finding(d, "projet"); code != ExitProblem || f == nil || f.Level != "error" || !strings.Contains(f.Message, "glose.json") || f.Fix == "" {
		t.Errorf("profil illisible : code %d, %+v", code, d)
	}
}

func TestDoctorReportsAStateDirectoryWithoutWriteAccess(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("droits POSIX d'un utilisateur ordinaire")
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.md": "Texte.\n"})
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	code, d := runDoctor(t, dir)
	if f := finding(d, ".glose"); code != ExitProblem || f == nil || f.Level != "error" || !strings.Contains(f.Fix, "chmod u+w") {
		t.Errorf("code %d, %+v", code, f)
	}
}

func TestQuoteArgNeverLetsTheShellExpand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("citation POSIX")
	}
	for in, want := range map[string]string{
		"/tmp/simple":       "/tmp/simple",
		"/tmp/a b":          "'/tmp/a b'",
		"/tmp/$HOME/`x`":    "'/tmp/$HOME/`x`'",
		"/tmp/l'apostrophe": `'/tmp/l'\''apostrophe'`,
	} {
		if got := quoteArg(in); got != want {
			t.Errorf("%q : %s, attendu %s", in, got, want)
		}
	}
}

func TestGloseCommandOnAPath(t *testing.T) {
	got := gloseCommand("glose open", "/tmp/a b")
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(gloseCommand("glose open", `C:\%TEMP%\x`), "glose open (lancée dans le dossier") {
			t.Fatal("chemin avec % cité")
		}
		return
	}
	if got != "glose open '/tmp/a b'" {
		t.Fatal(got)
	}
}

// Audit A15: temporary files left by an interrupted write are reported, and
// not removed.
func TestDoctorReportsLeftTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.md": "T\n", "ch/b.md": "U\n", "ch/.glose-123.tmp": "moitié", "notes/.glose-9.tmp": "hors du texte"})
	_, d := runDoctor(t, dir)
	f := finding(d, "fichiers temporaires")
	if f == nil || f.Level != "warning" || !strings.Contains(f.Message, "ch/.glose-123.tmp") || strings.Contains(f.Message, "notes/") {
		t.Fatalf("%+v", f)
	}
	if _, err := os.Stat(filepath.Join(dir, "ch", ".glose-123.tmp")); err != nil {
		t.Fatal("effacé")
	}
	os.Remove(filepath.Join(dir, "ch", ".glose-123.tmp"))
	if _, d = runDoctor(t, dir); finding(d, "fichiers temporaires") != nil {
		t.Fatal("signalé sans fichier")
	}
}

// D-64: glose open without a terminal (an agent, a script) approves nothing,
// and says what awaits; the pandoc of the PATH is used meanwhile.
func TestOpenWithoutATerminalApprovesNothing(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "projet")
	writeFiles(t, dir, map[string]string{"a.md": "T\n", "glose.json": `{"filter":"f.lua","pandoc":"/opt/autre/pandoc"}`, "f.lua": "return {}\n"})
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if prog := approveProfile(p, &out, &errOut); prog != "pandoc" {
		t.Fatalf("programme : %s", prog)
	}
	if !strings.Contains(out.String(), "En attente de votre accord : le filtre Lua f.lua et le programme /opt/autre/pandoc") {
		t.Fatalf("%q %q", out.String(), errOut.String())
	}
	if pOK, fOK := trust.Check(dir, trust.Of(dir, p.Pandoc, p.Filter)); pOK || fOK {
		t.Fatal("approuvé sans terminal")
	}
}

// D-64: in a terminal, glose open approves at the first opening only; a
// filter changed since (by an agent) waits for glose approve, which a launched
// agent, or a command without a terminal, cannot run.
func TestApproveOnlyByTheAuthorInATerminal(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "projet")
	writeFiles(t, dir, map[string]string{"a.md": "T\n", "glose.json": `{"filter":"f.lua"}`, "f.lua": "return {}\n"})
	p, _ := project.Load(dir)
	prev := interactive
	defer func() { interactive = prev }()
	interactive = func() bool { return true }
	var out, errOut strings.Builder
	approveProfile(p, &out, &errOut)
	if !strings.Contains(out.String(), "Première ouverture") {
		t.Fatalf("%q %q", out.String(), errOut.String())
	}
	need := func() trust.Need { return trust.Of(dir, p.Pandoc, p.Filter) }
	if _, fOK := trust.Check(dir, need()); !fOK {
		t.Fatal("non approuvé à la première ouverture")
	}
	// Changed by an agent: the next opening does not approve it.
	os.WriteFile(filepath.Join(dir, "f.lua"), []byte("os.execute('x')\n"), 0o644)
	out.Reset()
	approveProfile(p, &out, &errOut)
	if _, fOK := trust.Check(dir, need()); fOK || !strings.Contains(out.String(), "glose approve") {
		t.Fatalf("filtre changé approuvé à l'ouverture : %q", out.String())
	}
	// glose approve: refused to a launched agent, and without a terminal.
	t.Setenv("GLOSE_AGENT", "Sol")
	if code, _, errOut := run("approve", dir); code != ExitProblem || !strings.Contains(errOut, "réservé à l'auteur") {
		t.Fatalf("agent : %d %q", code, errOut)
	}
	t.Setenv("GLOSE_AGENT", "")
	interactive = func() bool { return false }
	if code, _, errOut := run("approve", dir); code != ExitProblem || !strings.Contains(errOut, "terminal") {
		t.Fatalf("sans terminal : %d %q", code, errOut)
	}
	if _, fOK := trust.Check(dir, need()); fOK {
		t.Fatal("approuvé")
	}
	interactive = func() bool { return true }
	if code, out, errOut := run("approve", dir); code != ExitOK || !strings.Contains(out, "Approuvé : le filtre Lua f.lua") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if _, fOK := trust.Check(dir, need()); !fOK {
		t.Fatal("non approuvé par glose approve")
	}
}
