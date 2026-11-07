package project

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWithoutProfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b.md", "b")
	write(t, dir, "a.md", "a")
	write(t, dir, "notes.txt", "x")
	write(t, dir, "sous/c.md", "c")
	write(t, dir, "AGENTS.md", "consignes")
	write(t, dir, ".cache/d.md", "caché")
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Files, []string{"a.md", "b.md", "sous/c.md"}) || p.Profiled || p.Filter != "" || p.Pandoc != "pandoc" {
		t.Fatalf("projet : %+v", p)
	}
}

func TestLoadWithProfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "02.md", "x")
	write(t, dir, "parties/01.md", "y")
	write(t, dir, "github.com/ptitan/glose/livre.lua", "-- filtre")
	write(t, dir, "img/a.svg", "<svg/>")
	write(t, dir, ProfileName, `{"title":"Mon livre","files":["parties/01.md","02.md"],
		"filter":"github.com/ptitan/glose/livre.lua","resources":["img"]}`)
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Mon livre" || !reflect.DeepEqual(p.Files, []string{"parties/01.md", "02.md"}) ||
		!strings.HasSuffix(p.Filter, filepath.Join("glose", "livre.lua")) || !p.Profiled {
		t.Fatalf("projet : %+v", p)
	}
	if _, ctype, err := p.Resource("img/a.svg"); err != nil || ctype != "image/svg+xml" {
		t.Fatalf("ressource : %s, %v", ctype, err)
	}
}

func TestLoadRejectsBadProfiles(t *testing.T) {
	cases := map[string]string{
		"chemin absolu":    `{"files":["/etc/passwd"]}`,
		"sortie du projet": `{"files":["../x.md"]}`,
		"fichier absent":   `{"files":["absent.md"]}`,
		"doublon":          `{"files":["a.md","./a.md"]}`,
		"champ inconnu":    `{"fichiers":["a.md"]}`,
		"filtre absent":    `{"filter":"absent.lua"}`,
		"JSON invalide":    `{"files":`,
		"ressource dehors": `{"resources":["../"]}`,
	}
	for name, prof := range cases {
		dir := t.TempDir()
		write(t, dir, "a.md", "a")
		write(t, dir, ProfileName, prof)
		if _, err := Load(dir); err == nil {
			t.Errorf("%s : erreur attendue", name)
		}
	}
}

func TestResourceRefusals(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.png", "x")
	dir := t.TempDir()
	write(t, dir, "a.md", "a")
	write(t, dir, "img/ok.png", "x")
	write(t, dir, "img/texte.md", "x")
	write(t, dir, "autre/b.png", "x")
	write(t, dir, ProfileName, `{"resources":["img"]}`)
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(dir, "img", "lien.png")); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Resource("img/ok.png"); err != nil {
		t.Fatalf("ressource permise refusée : %v", err)
	}
	for _, rel := range []string{"img/texte.md", "autre/b.png", "../x.png", "img/../../x.png", "/etc/x.png", "img/lien.png", ".glose/server.json"} {
		if _, _, err := p.Resource(rel); err == nil {
			t.Errorf("Resource(%q) : refus attendu", rel)
		}
	}
}

func TestLoadASingleFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "a")
	write(t, dir, "notes.md", "n")
	p, err := Load(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Files, []string{"notes.md"}) || p.Title != "notes.md" {
		t.Fatalf("projet : %+v", p)
	}
}

func TestStateOf(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "a")
	want := filepath.Join(dir, StateDir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		want = filepath.Join(real, StateDir)
	}
	for _, p := range []string{dir, filepath.Join(dir, "a.md"), filepath.Join(dir, "efface.md")} {
		if got, err := StateOf(p); err != nil || got != want {
			t.Errorf("StateOf(%q) = %q, %v ; attendu %q", p, got, err, want)
		}
	}
}

func TestCSSMustBeACSSFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "secret")
	write(t, dir, ProfileName, `{"css":"a.md"}`)
	if _, err := Load(dir); err == nil {
		t.Fatal("une feuille de style qui n'est pas un .css doit être refusée")
	}
}

// Revue générale (agy) : un dossier de ressources qui est un lien vers un
// dossier du projet sert ses images.
func TestResourceDirectoryThroughALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques")
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "fig.png"), []byte("png"), 0o644)
	os.Symlink("assets", filepath.Join(dir, "img"))
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("![](img/fig.png)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"resources": ["img"]}`), 0o644)
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Resource("img/fig.png"); err != nil {
		t.Fatal(err)
	}
}

// Sans liste de fichiers, l'ordre est celui d'une table des matières : les
// fichiers d'un dossier, puis ses sous-dossiers ; « 01.05--a.md » avant
// « 01.05.01--b.md ».
func TestTreeOrder(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"introduction.md", "chapitres/02.md", "chapitres/01.md", "chapitres/annexes/x.md",
		"chapitres/zz.md", "Zèbre.md", "livre-1/01.05.01--b.md", "livre-1/01.05--a.md", "livre-1/01.06--c.md"} {
		write(t, dir, f, "x")
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Zèbre.md", "introduction.md", "chapitres/01.md", "chapitres/02.md", "chapitres/zz.md",
		"chapitres/annexes/x.md", "livre-1/01.05--a.md", "livre-1/01.05.01--b.md", "livre-1/01.06--c.md"}
	if !reflect.DeepEqual(p.Files, want) {
		t.Fatalf("ordre :\n%q\nattendu :\n%q", p.Files, want)
	}
}

// Dossiers, motifs et exclusions dans « files » : l'arborescence du
// ouvrage en plusieurs parties, réduite.
func TestFilesDirectoriesPatternsAndExclude(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"chantiers.md", "plana2.md", "AGENT/AGENTS.md", "AGENT/style.md",
		"introduction/introduction-generale.md", "introduction/sources/notes.md",
		"livre-1/01--regles.md", "livre-1/01.01--communes.md", "livre-1/.humanise/sauvegardes/01.md",
		"livre-2/02--droits-reels.md", "_to_delete/vieux.md", "livre-1/notes.txt"} {
		write(t, dir, f, "x")
	}
	cases := []struct {
		profile string
		want    []string
	}{
		{`{"files": ["introduction/", "livre-*"]}`, []string{"introduction/introduction-generale.md",
			"introduction/sources/notes.md", "livre-1/01--regles.md", "livre-1/01.01--communes.md", "livre-2/02--droits-reels.md"}},
		{`{"exclude": ["AGENT", "_to_delete", "*.md", "**/sources"]}`, []string{"introduction/introduction-generale.md",
			"livre-1/01--regles.md", "livre-1/01.01--communes.md", "livre-2/02--droits-reels.md"}},
		// Un fichier nommé garde sa place ; le motif qui le reprend ne le double pas.
		{`{"files": ["livre-2/02--droits-reels.md", "livre-*/*.md"]}`, []string{"livre-2/02--droits-reels.md",
			"livre-1/01--regles.md", "livre-1/01.01--communes.md"}},
		{`{"files": ["**/0*.md"], "exclude": ["livre-2"]}`, []string{"livre-1/01--regles.md", "livre-1/01.01--communes.md"}},
		// Un dossier caché nommé est parcouru ; un fichier d'agent nommé est pris.
		{`{"files": ["livre-1/.humanise", "AGENT/AGENTS.md"]}`, []string{"livre-1/.humanise/sauvegardes/01.md", "AGENT/AGENTS.md"}},
		// Un motif qui ne trouve rien n'est pas une erreur (livre encore vide).
		{`{"files": ["livre-9*", "plana2.md"]}`, []string{"plana2.md"}},
	}
	for _, c := range cases {
		write(t, dir, ProfileName, c.profile)
		p, err := Load(dir)
		if err != nil {
			t.Errorf("%s : %v", c.profile, err)
			continue
		}
		if !reflect.DeepEqual(p.Files, c.want) {
			t.Errorf("%s :\n%q\nattendu :\n%q", c.profile, p.Files, c.want)
		}
	}
	for _, bad := range []string{`{"files": ["../*.md"]}`, `{"files": ["/x/*.md"]}`, `{"files": ["[.md"]}`,
		`{"exclude": ["../x"]}`, `{"files": ["absent/"]}`, `{"files": ["livre-9*"]}`} {
		write(t, dir, ProfileName, bad)
		if _, err := Load(dir); err == nil {
			t.Errorf("%s : erreur attendue", bad)
		}
	}
}

func TestTooManyFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= MaxFiles; i++ {
		write(t, dir, filepath.Join("notes", strings.Repeat("n", 1+i%7)+"-"+strconv.Itoa(i)+".md"), "x")
	}
	write(t, dir, "a.md", "a")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "files") {
		t.Fatalf("erreur attendue qui renvoie à « files » : %v", err)
	}
	write(t, dir, ProfileName, `{"files": ["a.md"]}`)
	if p, err := Load(dir); err != nil || len(p.Files) != 1 {
		t.Fatalf("liste explicite : %v", err)
	}
}

// Un fichier par lien symbolique hors du projet est refusé, même trouvé
// par un motif.
func TestPatternRefusesLinksOutside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques")
	}
	outside := t.TempDir()
	write(t, outside, "secret.md", "s")
	dir := t.TempDir()
	write(t, dir, "a.md", "a")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "lien.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("lien vers l'extérieur accepté")
	}
}

// P6.7 (D-35) : un sous-dossier ou un fichier d'un projet ouvre le projet
// entier, avec la partie demandée ; un fichier hors du profil est relu seul,
// dans ce projet ; hors de tout projet, rien ne change ; la recherche
// s'arrête à un dépôt.
func TestSingleRoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ProfileName, `{"files":["intro.md","livre-1/"]}`)
	write(t, dir, "intro.md", "i")
	write(t, dir, "livre-1/01.md", "a")
	write(t, dir, "livre-1/02.md", "b")
	write(t, dir, "notes/n.md", "n")
	real, _ := filepath.EvalSymlinks(dir)
	cases := []struct {
		path, focus, source string
		files               int
	}{
		{dir, "", real, 3},
		{filepath.Join(dir, "livre-1"), "livre-1/", real, 3},
		{filepath.Join(dir, "livre-1", "02.md"), "livre-1/02.md", real, 3},
		{filepath.Join(dir, "notes", "n.md"), "", filepath.Join(real, "notes", "n.md"), 1},
		{filepath.Join(dir, "notes"), "notes/", real, 3},
	}
	for _, c := range cases {
		p, err := Load(c.path)
		if err != nil {
			t.Fatalf("%s : %v", c.path, err)
		}
		if p.Root != real || p.Focus != c.focus || p.Source != c.source || len(p.Files) != c.files {
			t.Errorf("%s : racine %s, partie %q, source %s, %d fichiers", c.path, p.Root, p.Focus, p.Source, len(p.Files))
		}
		if st, _ := StateOf(c.path); st != filepath.Join(real, StateDir) {
			t.Errorf("%s : état %s", c.path, st)
		}
	}
	// Un dépôt arrête la recherche : son sous-dossier reste son propre projet.
	write(t, dir, "depot/.git/HEAD", "x")
	write(t, dir, "depot/x.md", "x")
	if p, err := Load(filepath.Join(dir, "depot")); err != nil || p.Root != filepath.Join(real, "depot") {
		t.Fatalf("dépôt : %+v, %v", p, err)
	}
	// Hors de tout projet : un fichier seul, dans son dossier.
	other := t.TempDir()
	write(t, other, "a.md", "a")
	write(t, other, "b.md", "b")
	if p, err := Load(filepath.Join(other, "a.md")); err != nil || len(p.Files) != 1 || p.Focus != "" {
		t.Fatalf("fichier seul : %+v, %v", p, err)
	}
}
