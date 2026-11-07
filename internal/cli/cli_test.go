package cli

import (
	"bytes"
	"encoding/json"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHelpIsShownWithoutArguments(t *testing.T) {
	code, out, errOut := run()
	if code != ExitOK {
		t.Fatalf("code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(out, "Usage :") || errOut != "" {
		t.Fatalf("stdout = %q, stderr = %q", out, errOut)
	}
}

func TestHelpAliases(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		code, out, _ := run(arg)
		if code != ExitOK || !strings.Contains(out, "Commandes de l'agent") {
			t.Errorf("%s: code = %d, stdout = %q", arg, code, out)
		}
	}
}

func TestVersionPrintsProgramAndVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		code, out, errOut := run(arg)
		if code != ExitOK || out != "glose "+Version+"\n" || errOut != "" {
			t.Errorf("%s: code = %d, stdout = %q, stderr = %q", arg, code, out, errOut)
		}
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	code, out, errOut := run("frobnicate")
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
	if out != "" || !strings.Contains(errOut, `"frobnicate"`) || !strings.Contains(errOut, "glose help") {
		t.Fatalf("stdout = %q, stderr = %q", out, errOut)
	}
}

func TestExtraArgumentsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"help", "x"}, {"version", "x"}} {
		code, out, _ := run(args...)
		if code != ExitUsage || out != "" {
			t.Errorf("%v: code = %d, stdout = %q", args, code, out)
		}
	}
}

func mkproject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestListJSON(t *testing.T) {
	dir := mkproject(t, map[string]string{
		"a.md": "Un {==passage==}{>>question<<}{>>réponse<<} et {~~ancien~>nouveau~~}.\n",
		"b.md": "Rien.\n",
	})
	code, out, errOut := run("list", "--json", dir)
	if code != ExitOK || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var got struct {
		Annotations []struct {
			File     string   `json:"file"`
			Line     int      `json:"line"`
			Column   int      `json:"column"`
			Kind     string   `json:"kind"`
			Text     string   `json:"text"`
			New      string   `json:"new"`
			Comments []string `json:"comments"`
		} `json:"annotations"`
		Issues []any `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON illisible : %v\n%s", err, out)
	}
	if len(got.Annotations) != 2 || len(got.Issues) != 0 {
		t.Fatalf("sortie : %+v", got)
	}
	a, b := got.Annotations[0], got.Annotations[1]
	if a.File != "a.md" || a.Line != 1 || a.Column != 4 || a.Kind != "highlight" || a.Text != "passage" ||
		len(a.Comments) != 2 || a.Comments[1] != "réponse" {
		t.Errorf("première annotation : %+v", a)
	}
	if b.Kind != "substitution" || b.Text != "ancien" || b.New != "nouveau" {
		t.Errorf("seconde annotation : %+v", b)
	}
	if code, out, _ := run("list", "--kind", "substitution", dir); code != ExitOK || !strings.Contains(out, "remplacement") || strings.Contains(out, "passage commenté") {
		t.Errorf("filtre par type : %d %q", code, out)
	}
	if code, _, _ := run("list", "--kind", "inconnu", dir); code != ExitUsage {
		t.Errorf("type inconnu : code %d", code)
	}
}

func TestCheckReportsMalformedMarks(t *testing.T) {
	dir := mkproject(t, map[string]string{"a.md": "Texte {++ jamais fermé.\n"})
	code, out, _ := run("check", dir)
	if code != ExitProblem || !strings.Contains(out, "a.md:1:7") || !strings.Contains(out, "marque non fermée") {
		t.Fatalf("code %d, sortie %q", code, out)
	}
	ok := mkproject(t, map[string]string{"a.md": "Texte {>>bien<<} formé.\n"})
	if _, err := exec.LookPath("pandoc"); err == nil {
		if code, out, _ := run("check", "--json", ok); code != ExitOK || !strings.Contains(out, `"ok": true`) {
			t.Fatalf("projet sain : code %d, sortie %q", code, out)
		}
	}
}

func TestUnknownProject(t *testing.T) {
	if code, _, errOut := run("list", filepath.Join(t.TempDir(), "absent")); code != ExitProblem || errOut == "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if code, _, _ := run("list", "a", "b"); code != ExitUsage {
		t.Fatalf("deux chemins : code %d", code)
	}
}

// The agent's side of the exchange, by commands only: init, wait, list,
// reply, propose (--after next to a comment, --id on a highlight), check,
// done.
func TestAgentCommands(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) { os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644) }
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(dir, name)); return string(b) }
	write("a.md", "Un {==mot juste==}{>>Corrige.<<} ici. Fin.{>>Une transition ?<<}\n")
	write("CLAUDE.md", "# Consignes\n")
	if code, out, errOut := run("init", dir); code != ExitOK || !strings.Contains(out, "CLAUDE.md") {
		t.Fatalf("init : %d %s %s", code, out, errOut)
	}
	if !strings.Contains(read("CLAUDE.md"), "## Navette Glose") || strings.Contains(read("glose.json"), "CLAUDE.md") {
		t.Fatalf("init : %s / %s", read("CLAUDE.md"), read("glose.json"))
	}
	// The author's turn: the agent may not write.
	if code, _, _ := run("reply", "--id", "xxxx", "--text", "x", dir); code != ExitProblem {
		t.Fatalf("écriture pendant le tour de l'auteur : %d", code)
	}
	if code, _, _ := run("wait", "--timeout", "100ms", dir); code != ExitTimeout {
		t.Fatalf("wait : %d", code)
	}
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", "À toi."); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run("wait", "--timeout", "1s", dir); code != ExitOK || !strings.Contains(out, "Tour 1 : la main est à l'agent") {
		t.Fatalf("wait : %d %s", code, out)
	}
	var list struct {
		Annotations []struct {
			ID, Kind, Status string
		} `json:"annotations"`
	}
	_, out, _ := run("list", "--json", dir)
	json.Unmarshal([]byte(out), &list)
	if len(list.Annotations) != 2 || list.Annotations[0].Kind != "highlight" || list.Annotations[0].Status != "open" {
		t.Fatalf("list : %s", out)
	}
	hl, cm := list.Annotations[0].ID, list.Annotations[1].ID
	if code, out, errOut := run("propose", "--id", hl, "--new", "terme exact", "--note", "Plus précis.", dir); code != ExitOK {
		t.Fatalf("propose --id : %d %s %s", code, out, errOut)
	}
	if code, out, errOut := run("propose", "--file", "a.md", "--after", "Fin.", "--new", " Voici la transition.", dir); code != ExitOK {
		t.Fatalf("propose --after : %d %s %s", code, out, errOut)
	}
	if code, _, errOut := run("reply", "--id", cm, "--text", "Ajoutée.", dir); code != ExitOK {
		t.Fatalf("reply : %s", errOut)
	}
	// A hand-written technical comment: check announces the refusal of done.
	good := read("a.md")
	write("a.md", strings.Replace(good, " ici.", " ici{>>glose:v1 id=zzzz by=author<<}.", 1))
	if code, out, _ := run("check", dir); code != ExitProblem || !strings.Contains(out, "glose done refusera") {
		t.Fatalf("check : %d %s", code, out)
	}
	if code, _, _ := run("done", dir); code != ExitProblem {
		t.Fatal("done accepté")
	}
	write("a.md", good)
	if code, out, errOut := run("done", "--note", "Fait.", dir); code != ExitOK {
		t.Fatalf("done : %d %s %s", code, out, errOut)
	}
	got := read("a.md")
	if !strings.Contains(got, "{~~mot juste~>terme exact~~}{>>glose:v1 id="+hl+" by=agent<<}{>>Corrige.<<}") ||
		!strings.Contains(got, "{>>Plus précis.<<}") || !strings.Contains(got, "{++ Voici la transition.++}") {
		t.Fatalf("%s", got)
	}
	if _, err := exec.LookPath("pandoc"); err == nil {
		if code, out, _ := run("check", dir); code != ExitOK {
			t.Fatalf("check final : %s", out)
		}
	}
	if code, out, _ := run("log", dir); code != ExitOK || !strings.Contains(out, "Tour 1 — réponse de l'agent, remise le") {
		t.Fatalf("log : %s", out)
	}
}

// Codex review (phase 4): « glose propose --id » on a proposal of the agent
// itself corrects it in place (D-31), instead of refusing.
func TestProposeIDCorrectsTheAgentsOwnProposal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	os.WriteFile(path, []byte("Un texte.\n"), 0o644)
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", "À toi."); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := run("propose", "--file", "a.md", "--find", "texte", "--new", "écrit", dir); code != ExitOK {
		t.Fatalf("propose : %d %s %s", code, out, errOut)
	}
	_, out, _ := run("list", "--json", "--by", "agent", dir)
	var list struct{ Annotations []struct{ ID string } }
	json.Unmarshal([]byte(out), &list)
	if len(list.Annotations) != 1 || list.Annotations[0].ID == "" {
		t.Fatalf("list : %s", out)
	}
	id := list.Annotations[0].ID
	if code, out, errOut := run("propose", "--id", id, "--new", "propos", "--note", "Plus juste.", dir); code != ExitOK {
		t.Fatalf("propose --id sur sa proposition : %d %s %s", code, out, errOut)
	}
	got, _ := os.ReadFile(path)
	want := "Un {~~texte~>propos~~}{>>glose:v1 id=" + id + " by=agent<<}{>>Plus juste.<<}"
	if !strings.HasPrefix(string(got), want) {
		t.Fatalf("%s", got)
	}
	if code, out, errOut := run("done", dir); code != ExitOK {
		t.Fatalf("done : %d %s %s", code, out, errOut)
	}
}

// D-32 : « glose propose --whole » écrit une seule proposition, que
// « glose done » remet telle quelle.
func TestProposeWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	os.WriteFile(path, []byte("Avant de présenter les décisions essentielles, il convient de s'arrêter le principe.\n"), 0o644)
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", "Réécris la phrase."); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run("propose", "--whole", "--file", "a.md", "--after", "principe", "--new", "x", dir); code != ExitUsage {
		t.Fatalf("--whole avec --after : %d %s", code, errOut)
	}
	if code, out, errOut := run("propose", "--file", "a.md", "--whole", "--find", "Avant de présenter les décisions essentielles, il convient de s'arrêter le principe.",
		"--new", "Il faut d'abord présenter le principe.", "--note", "Plus direct.", dir); code != ExitOK {
		t.Fatalf("propose --whole : %d %s %s", code, out, errOut)
	}
	if code, out, errOut := run("done", dir); code != ExitOK {
		t.Fatalf("done : %d %s %s", code, out, errOut)
	}
	_, out, _ := run("list", "--json", dir)
	var list struct {
		Annotations []struct{ Kind, Text, New string }
	}
	json.Unmarshal([]byte(out), &list)
	if len(list.Annotations) != 1 || list.Annotations[0].Kind != "substitution" ||
		list.Annotations[0].New != "Il faut d'abord présenter" {
		t.Fatalf("une seule proposition attendue : %s", out)
	}
}

// Revue de P4.10 : une réécriture d'un tenant sur deux paragraphes passe
// glose done.
func TestProposeWholeAcrossParagraphs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	os.WriteFile(path, []byte("Avant.\n\nUn chat.\n\nUn chien.\n\nFin.\n"), 0o644)
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", "x"); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := run("propose", "--file", "a.md", "--whole", "--find", "Un chat.\n\nUn chien.", "--new", "Un lion.\n\nUn loup.", dir); code != ExitOK {
		t.Fatalf("propose : %d %s %s", code, out, errOut)
	}
	before, _ := os.ReadFile(path)
	if code, out, errOut := run("done", dir); code != ExitOK {
		t.Fatalf("done : %d %s %s\n%s", code, out, errOut, before)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("proposition changée :\n%s\n%s", before, after)
	}
}

// Revue générale (agy) : « -h » valeur d'une option n'affiche pas l'aide ;
// --file accepte « ./ » ; --by est contrôlé ; glose init écarte claude.md.
func TestArgumentsThatLookLikeOptions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Un mot -h ici.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "claude.md"), []byte("# consignes\n"), 0o644)
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", "x"); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run("comment", "--file", "./a.md", "--find", "-h", "--text", "--help", dir)
	if code != ExitOK || strings.Contains(out, "Usage :") {
		t.Fatalf("comment : %d %s %s", code, out, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.md")); !strings.Contains(string(b), "{>>--help<<}") {
		t.Fatalf("%s", b)
	}
	if code, out, _ := run("propose", "--help"); code != ExitOK || !strings.Contains(out, "Usage :") {
		t.Fatalf("aide : %d", code)
	}
	if code, out, _ := run("done", "--note", "--file", "-h", dir); code != ExitOK || !strings.Contains(out, "Usage :") {
		t.Fatalf("done --note --file -h : %d", code)
	}
	if code, _, _ := run("list", "--by", "autor", dir); code != ExitUsage {
		t.Fatalf("--by inconnu : %d", code)
	}
	if code, _, _ := run("list", "--file", "./a.md", dir); code != ExitOK {
		t.Fatalf("--file ./a.md : %d", code)
	}
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "a.md"), []byte("Texte.\n"), 0o644)
	os.WriteFile(filepath.Join(dir2, "claude.md"), []byte("# consignes\n"), 0o644)
	run("init", dir2)
	if b, _ := os.ReadFile(filepath.Join(dir2, "glose.json")); strings.Contains(string(b), "claude.md") {
		t.Fatalf("claude.md dans le profil : %s", b)
	}
}

// An option at « - » is read on the standard input: a long text with quotes,
// backticks and a blank line passes without shell quoting; done --json gives
// the cursor of « glose wait --after »; list --json gives the paragraph of
// each annotation.
func TestAgentConveniences(t *testing.T) {
	dir := mkproject(t, map[string]string{"a.md": "\ufeffPremier paragraphe.\r\n\r\nLe contrat est nul si une condition manque.{>>Ajouter la nullité relative.<<}\r\n\r\nFin.\r\n"})
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", "À toi."); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Annotations []struct{ ID, Context string } `json:"annotations"`
	}
	_, out, _ := run("list", "--json", dir)
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Annotations) != 1 {
		t.Fatalf("list : %v %s", err, out)
	}
	if c := list.Annotations[0].Context; c != "Le contrat est nul si une condition manque." {
		t.Fatalf("contexte : %q", c)
	}
	id := list.Annotations[0].ID

	long := "Réponse « longue » avec `code` et \"guillemets\".\n\nSecond paragraphe.\n"
	defer func(r io.Reader) { stdin = r }(stdin)
	stdin = strings.NewReader(long)
	if code, out, errOut := run("reply", "--id", id, "--text", "-", "--json", dir); code != ExitOK || !strings.Contains(out, `"created"`) {
		t.Fatalf("reply --text - : %d %s %s", code, out, errOut)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !strings.Contains(string(b), "{>>"+strings.TrimSuffix(long, "\n")+"<<}") {
		t.Fatalf("réponse lue sur l'entrée standard : %q", b)
	}
	stdin = strings.NewReader("une condition manque.\n")
	if code, _, errOut := run("propose", "--file", "a.md", "--find", "-", "--new", "-", dir); code != ExitUsage || !strings.Contains(errOut, "une seule option") {
		t.Fatalf("deux options à « - » : %d %s", code, errOut)
	}
	stdin = strings.NewReader("Fait, avec « soin ».\n")
	code, out, errOut := run("done", "--note", "-", "--json", dir)
	var done struct {
		Round int   `json:"round"`
		Seq   int64 `json:"seq"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &done) != nil || done.Round != 1 || done.Seq == 0 {
		t.Fatalf("done --json : %d %s %s", code, out, errOut)
	}
	if _, out, _ := run("log", dir); !strings.Contains(out, "« Fait, avec « soin ». »") {
		t.Fatalf("note lue sur l'entrée standard : %s", out)
	}
}

// A long paragraph is cut around the annotation, never inside a character.
func TestAnnotationContextOfALongParagraph(t *testing.T) {
	src := []byte(strings.Repeat("é", 2000) + "{>>ici<<}" + strings.Repeat("à", 2000) + "\n\nAutre.\n")
	at := 4000
	c := annotationContext(src, at, at+len("{>>ici<<}"))
	if !utf8.ValidString(c) || !strings.HasPrefix(c, "…") || !strings.HasSuffix(c, "…") || len(c) > contextMax+8 || strings.Contains(c, "Autre") {
		t.Fatalf("contexte : %d octets, %q…", len(c), c[:min(len(c), 20)])
	}
}
