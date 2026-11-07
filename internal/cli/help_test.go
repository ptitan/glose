package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/project"
)

func TestProfileHelp(t *testing.T) {
	code, out, errOut := run("help", "profile")
	if code != ExitOK || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	// Every field of project.Profile, the filter contract and the example.
	for _, field := range []string{"title", "files", "filter", "css", "resources", "pandoc", "exclude", "filterPerFile"} {
		if !strings.Contains(out, "\n  "+field+" ") {
			t.Errorf("aide du profil sans le champ %s", field)
		}
	}
	for _, want := range []string{"glose-file", "data-g", "champ inconnu est refusé", "function Pandoc(doc)", ".encadre", "glose render"} {
		if !strings.Contains(out, want) {
			t.Errorf("aide du profil sans %q", want)
		}
	}
	if _, general, _ := run("help"); !strings.Contains(general, "glose help profile") {
		t.Error("l'aide générale ne renvoie pas à « glose help profile »")
	}
	if code, out, errOut := run("help", "inconnu"); code != ExitUsage || out != "" || !strings.Contains(errOut, "glose help profile") {
		t.Errorf("sujet inconnu : code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// The example printed by « glose help profile » works as printed: a profile
// with a file in a subdirectory, the example filter and stylesheet. The
// filter adds numbers and a frame, and leaves every data-g Span exactly as
// the rendering without filter gives it.
func TestProfileHelpExampleRenders(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc absent")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("introduction.md", "# Titre\n\nPremier paragraphe, *en partie* mis en valeur.\n\nAttention, second paragraphe.\n")
	write("chapitres/01.md", "Troisième paragraphe.\n")
	write("github.com/ptitan/glose/rendu.lua", exampleFilter)
	write("github.com/ptitan/glose/rendu.css", exampleCSS)
	write("glose.json", `{"files": ["introduction.md", "chapitres/01.md"]}`)
	code, plain, errOut := run("render", dir)
	if code != ExitOK || errOut != "" {
		t.Fatalf("sans filtre : code %d, stderr %q", code, errOut)
	}
	write("glose.json", `{"files": ["introduction.md", "chapitres/01.md"], "filter": "github.com/ptitan/glose/rendu.lua", "css": "github.com/ptitan/glose/rendu.css"}`)
	code, html, errOut := run("render", dir)
	if code != ExitOK || errOut != "" {
		t.Fatalf("avec filtre : code %d, stderr %q", code, errOut)
	}
	for _, want := range []string{`<span class="num">1. </span>`, `<span class="num">2. </span>`,
		`<span class="num">3. </span>`, `<div class="encadre">`} {
		if !strings.Contains(html, want) {
			t.Errorf("rendu filtré sans %q :\n%s", want, html)
		}
	}
	if strings.Count(html, `class="encadre"`) != 1 {
		t.Errorf("un seul paragraphe encadré attendu :\n%s", html)
	}
	spans := regexp.MustCompile(`<span data-g="[^"]*">[^<]*</span>`)
	before, after := spans.FindAllString(plain, -1), spans.FindAllString(html, -1)
	if len(before) == 0 || strings.Join(before, "") != strings.Join(after, "") {
		t.Errorf("Span data-g changés par le filtre :\navant %q\naprès %q", before, after)
	}
	if code, out, _ := run("check", dir); code != ExitOK {
		t.Errorf("glose check : code %d\n%s", code, out)
	}
}

// glose init sur une arborescence : les fichiers de la racine par leur nom,
// chaque sous-dossier en entier ; consignes d'agent et dossiers cachés
// écartés. Le profil écrit redonne les fichiers que Glose prenait sans lui.
func TestInitWritesDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"main.md", "p1/partie1.md", "p2/partie2.md", "p2/annexe/a.md", "AGENT/AGENTS.md",
		"AGENT/style.md", ".humanise/x.md"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("Texte.\n"), 0o644)
	}
	before, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run("init", dir)
	if code != ExitOK || !strings.Contains(out, "1 dans AGENT/") || !strings.Contains(out, "2 dans p2/") || !strings.Contains(out, "exclude") {
		t.Fatalf("init : %d %q %q", code, out, errOut)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "glose.json"))
	if !strings.Contains(string(b), `"main.md",
    "AGENT/",
    "p1/",
    "p2/"`) {
		t.Fatalf("profil :\n%s", b)
	}
	after, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"main.md", "AGENT/style.md", "p1/partie1.md", "p2/partie2.md", "p2/annexe/a.md"}
	if strings.Join(before.Files, " ") != strings.Join(want, " ") || strings.Join(after.Files, " ") != strings.Join(want, " ") {
		t.Fatalf("sans profil %q, avec %q ; attendu %q", before.Files, after.Files, want)
	}
}

// P6.7 : glose list sur un sous-dossier filtre ses annotations ; glose init
// dans un sous-dossier d'un projet est refusé.
func TestSubdirectoryOfAProject(t *testing.T) {
	dir := t.TempDir()
	for f, c := range map[string]string{"glose.json": `{"files":["a.md","livre/"]}`, "a.md": "Un {>>remarque a<<} texte.\n",
		"livre/01.md": "Deux {>>remarque livre<<} textes.\n"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(c), 0o644)
	}
	_, out, _ := run("list", filepath.Join(dir, "livre"))
	if !strings.Contains(out, "remarque livre") || strings.Contains(out, "remarque a") {
		t.Fatalf("list livre/ : %q", out)
	}
	if _, out, _ := run("list", "--file", "livre/", dir); !strings.Contains(out, "remarque livre") || strings.Contains(out, "remarque a") {
		t.Fatalf("list --file livre/ : %q", out)
	}
	if code, _, errOut := run("init", filepath.Join(dir, "livre")); code == ExitOK || !strings.Contains(errOut, "à sa racine") {
		t.Fatalf("init dans un sous-dossier : %d %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "livre", "glose.json")); err == nil {
		t.Fatal("profil imbriqué créé")
	}
}
