package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
