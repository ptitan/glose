package cli

import (
	"bytes"
	"encoding/json"
	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/proc"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/server"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// P10.1: the commands of the agent leave a sign of life in .glose/agent.json,
// and nothing else changes.
func TestAgentSignOfLife(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	dir := mkproject(t, map[string]string{"a.md": "Un texte. Fin.\n"})
	presence := func() *navette.Presence { return navette.ReadPresence(filepath.Join(dir, ".glose")) }
	// The author's commands without a name leave no sign.
	run("status", dir)
	run("list", dir)
	if p := presence(); p != nil {
		t.Fatalf("signe sans agent : %+v", p)
	}
	if code, _, errOut := run("status", "--agent", "a\tb", dir); code != ExitUsage || !strings.Contains(errOut, "nom d'agent") {
		t.Fatalf("nom invalide : %d %s", code, errOut)
	}
	if code, _, _ := run("wait", "--agent", "OpenCode", "--timeout", "50ms", dir); code != ExitTimeout {
		t.Fatal(code)
	}
	if p := presence(); p == nil || p.Name != "OpenCode" || p.Activity != navette.ActWait {
		t.Fatalf("wait : %+v", p)
	}
	// GLOSE_AGENT names the agent of status, which leaves a sign.
	t.Setenv("GLOSE_AGENT", "Sol")
	run("status", dir)
	if p := presence(); p == nil || p.Name != "Sol" || p.Activity != navette.ActRead {
		t.Fatalf("status : %+v", p)
	}
	t.Setenv("GLOSE_AGENT", "")
	before, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	p, _ := project.Load(dir)
	if _, err := navette.New(p).Handoff("author", ""); err != nil {
		t.Fatal(err)
	}
	st, _, _, _ := navette.New(p).State()
	if st.Agent == nil || st.Agent.Taken {
		t.Fatalf("avant de prendre la main : %+v", st.Agent)
	}
	run("wait", "--agent", "OpenCode", "--timeout", "1s", dir)
	st, _, _, _ = navette.New(p).State()
	if st.Agent == nil || st.Agent.Name != "OpenCode" || !st.Agent.Taken || !st.Agent.Fresh {
		t.Fatalf("main prise : %+v", st.Agent)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !bytes.Equal(before, after) {
		t.Fatal("le manuscrit a changé")
	}
	if code, _, errOut := run("comment", "--file", "a.md", "--after", "Fin.", "--text", "Ajout ?", "--agent", "OpenCode", dir); code != ExitOK {
		t.Fatal(errOut)
	}
	if p := presence(); p.Activity != navette.ActWrite {
		t.Fatalf("comment : %+v", p)
	}
	// Without a name, the agent is « agent ».
	if code, _, errOut := run("done", dir); code != ExitOK {
		t.Fatal(errOut)
	}
	if p := presence(); p.Name != "agent" || p.Activity != navette.ActDone {
		t.Fatalf("done : %+v", p)
	}
}

// P10.2, P10.12: « glose join » binds a session, then waits for the hand;
// the same command waits for each turn.
func TestJoin(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	t.Setenv("OPENCODE_SESSION_ID", "")
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n"})
	state := filepath.Join(dir, ".glose")
	if code, _, errOut := run("join", dir); code != ExitUsage || !strings.Contains(errOut, "code") {
		t.Fatalf("sans code : %d %s", code, errOut)
	}
	if code, _, errOut := run("join", "ABCD-EF", dir); code != ExitProblem || !strings.Contains(errOut, "aucun code") {
		t.Fatalf("sans code en cours : %d %s", code, errOut)
	}
	l, err := navette.NewLink(state, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run("join", "ZZZZ-ZZ", "--agent", "Codex", dir); code != ExitProblem || !strings.Contains(errOut, "ne correspond pas") {
		t.Fatalf("faux code : %d %s", code, errOut)
	}
	if p := navette.ReadPresence(state); p != nil {
		t.Fatalf("présence après un faux code : %+v", p)
	}
	// The hand is at the author: it binds, waits, then ends with code 3 and
	// the command to run again.
	code, out, errOut := run("join", "--agent", "Claude Code", "--model", "claude-opus-5", "--timeout", "300ms", l.Code, dir)
	if code != ExitTimeout || !strings.Contains(out, `Lié à la navette`) || !strings.Contains(out, "(modèle claude-opus-5)") ||
		!strings.Contains(out, `Relancez exactement la même commande`) || !strings.Contains(out, `glose join `+l.Code+` --agent "Claude Code"`) {
		t.Fatalf("join : %d %s %s", code, out, errOut)
	}
	if p := navette.ReadPresence(state); p == nil || p.Name != "Claude Code" || p.Activity != navette.ActWait || p.Model != "claude-opus-5" || p.ModelSource != navette.ModelDeclared {
		t.Fatalf("présence : %+v", p)
	}
	// Another agent cannot take the code.
	if code, _, errOut := run("join", l.Code, "--agent", "Codex", dir); code != ExitProblem || !strings.Contains(errOut, "déjà servi") {
		t.Fatalf("autre agent : %d %s", code, errOut)
	}
	// Once the hand is handed over, the same command gives the turn.
	p, _ := project.Load(dir)
	navette.New(p).Handoff("author", "")
	code, out, _ = run("join", l.Code, "--agent", "Claude Code", dir)
	if code != ExitOK || strings.Contains(out, "Lié à la navette") || !strings.Contains(out, "La main est à vous") || !strings.Contains(out, "## Navette Glose") ||
		!strings.Contains(out, "Puis relancez glose join "+l.Code) {
		t.Fatalf("tour : %d %s", code, out)
	}
	if code, _, _ := run("join", "--model", "a b", l.Code, dir); code != ExitUsage {
		t.Fatal("modèle invalide accepté")
	}
}

// P10.9: a session bound while an agent launched by Glose works waits for
// the next turn, and never erases the presence of that agent.
func TestJoinWhileALaunchedAgentWorks(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	t.Setenv("OPENCODE_SESSION_ID", "")
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n"})
	state := filepath.Join(dir, ".glose")
	p, _ := project.Load(dir)
	navette.New(p).Handoff("author", "")
	pid := os.Getpid()
	navette.TouchLaunched(state, "opus", 2, pid, proc.Identity(pid), time.Now().Add(time.Minute), "claude-agent/claude-opus-5")
	l, _ := navette.NewLink(state, time.Now())
	code, out, _ := run("join", l.Code, "--agent", "OpenCode", "--timeout", "300ms", dir)
	if code != ExitTimeout || !strings.Contains(out, "opus, lancé par Glose, mène le tour en cours") {
		t.Fatalf("%d %s", code, out)
	}
	if pr := navette.ReadPresence(state); pr.Name != "opus" || !pr.Launched {
		t.Fatalf("présence de l'agent lancé effacée : %+v", pr)
	}
	// Its run over without handing back, the turn stays its own (reserved
	// by Glose before the handover): the bound session waits for the next
	// handover (Codex audit).
	h, _ := navette.New(p).History()
	navette.SetOwner(state, navette.Owner{Turn: h.Turn(), Kind: navette.OwnerLaunch, Name: "opus", ID: "r1"})
	navette.ClearLaunched(state, pid)
	if code, _, _ := run("join", l.Code, "--agent", "OpenCode", "--timeout", "300ms", dir); code != ExitTimeout {
		t.Fatalf("main prise dans le tour de l'agent lancé : %d", code)
	}
	navette.New(p).Reclaim("", true)
	navette.New(p).Handoff("author", "")
	if code, out, _ := run("join", l.Code, "--agent", "OpenCode", "--timeout", "2s", dir); code != ExitOK || !strings.Contains(out, "La main est à vous") {
		t.Fatalf("%d %s", code, out)
	}
	// A new code ends the binding: the old command stops at once.
	navette.NewLink(state, time.Now())
	if code, _, errOut := run("join", l.Code, "--agent", "OpenCode", "--timeout", "2s", dir); code != ExitProblem || !strings.Contains(errOut, "ne correspond pas") {
		t.Fatalf("ancien code : %d %s", code, errOut)
	}
}

// Codex audit: one wait at a time for a code; a new code ends the wait of
// the old one; binding and model are written once the launched agent is gone.
func TestJoinWaitIsAlone(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	t.Setenv("OPENCODE_SESSION_ID", "")
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n"})
	state := filepath.Join(dir, ".glose")
	l, _ := navette.NewLink(state, time.Now())
	first := make(chan string, 1)
	go func() {
		_, out, errOut := run("join", l.Code, "--agent", "Sol", "--timeout", "3s", dir)
		first <- out + errOut
	}()
	time.Sleep(700 * time.Millisecond)
	if code, _, errOut := run("join", l.Code, "--agent", "Sol", "--timeout", "1s", dir); code != ExitProblem || !strings.Contains(errOut, "attend déjà") {
		t.Fatalf("seconde attente : %d %s", code, errOut)
	}
	navette.NewLink(state, time.Now())
	if out := <-first; !strings.Contains(out, "nouveau code de liaison") {
		t.Fatalf("première attente : %s", out)
	}
}

// P10.3: until « glose set », Glose says that no agent is configured.
func TestNoAgentConfigured(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n"})
	if _, out, _ := run("status", dir); !strings.Contains(out, "Aucun agent configuré : lancez « glose set »") {
		t.Fatalf("status : %s", out)
	}
	if _, out, _ := run("set", "--list"); !strings.Contains(out, "Claude Code : non trouvé") || !strings.Contains(out, "Aucun agent configuré") {
		t.Fatalf("set --list : %s", out)
	}
	if _, out, _ := run("doctor", dir); !strings.Contains(out, "aucun agent configuré") || !strings.Contains(out, "lancez glose set") {
		t.Fatalf("doctor : %s", out)
	}
	if _, err := agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex", Model: "gpt-6.1-sol"}}}); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := run("status", dir); strings.Contains(out, "Aucun agent configuré") {
		t.Fatalf("status configuré : %s", out)
	}
	if _, out, _ := run("set", "--list"); !strings.Contains(out, "Sol : Codex, gpt-6.1-sol (par défaut)") {
		t.Fatalf("set --list configuré : %s", out)
	}
}

// P10.3: check warns when the short name asked by the text is unknown here.
func TestCheckWarnsOfAnUnknownAlias(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n", "glose.json": `{"agent":{"alias":"Sol"}}`})
	if _, out, _ := run("check", dir); !strings.Contains(out, `nom court "Sol", inconnu sur cette machine`) {
		t.Fatalf("%s", out)
	}
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "sol", Agent: "codex"}}})
	if _, out, _ := run("check", dir); strings.Contains(out, "inconnu sur cette machine") {
		t.Fatalf("%s", out)
	}
}

// Codex review: after « done », the agent is not taken for one that waits.
func TestDoneIsNotListening(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n"})
	p, _ := project.Load(dir)
	navette.New(p).Handoff("author", "")
	if code, _, errOut := run("done", "--agent", "Codex", dir); code != ExitOK {
		t.Fatal(errOut)
	}
	st, _, _, _ := navette.New(p).State()
	if st.Agent == nil || !st.Agent.Fresh || st.Agent.Listening {
		t.Fatalf("%+v", st.Agent)
	}
	run("wait", "--agent", "Codex", "--timeout", "50ms", dir)
	st, _, _, _ = navette.New(p).State()
	if !st.Agent.Listening {
		t.Fatalf("après wait : %+v", st.Agent)
	}
}

// P10.6 (D-49): recipients and signatures in the command line.
func TestRecipientsAndPasses(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("GLOSE_AGENT", "")
	t.Setenv(navette.PassEnv, "")
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}, {Name: "Luna", Agent: "opencode"}}})
	dir := mkproject(t, map[string]string{"a.md": "Premier{>>Défaut.<<}{>>glose:v1 id=aaaa by=author<<} et second{>>Pour Luna.<<}{>>glose:v1 id=bbbb by=author to=Luna<<} et troisième{>>Inconnu.<<}{>>glose:v1 id=cccc by=author to=Mars<<}.\n"})
	ids := func(args ...string) string {
		_, out, errOut := run(append([]string{"list", "--json"}, append(args, dir)...)...)
		var l struct{ Annotations []struct{ ID, To string } }
		if err := json.Unmarshal([]byte(out), &l); err != nil {
			t.Fatalf("%s %s", out, errOut)
		}
		var s []string
		for _, a := range l.Annotations {
			s = append(s, a.ID+":"+a.To)
		}
		return strings.Join(s, ",")
	}
	// Without recipient, or with one unknown here (Mars): the default, Sol.
	if got := ids("--to", "sol"); got != "aaaa:,cccc:Mars" {
		t.Fatalf("--to sol : %s", got)
	}
	if got := ids("--to", "Luna"); got != "bbbb:Luna" {
		t.Fatalf("--to Luna : %s", got)
	}
	if code, _, _ := run("list", "--to", "Sol Levant", dir); code != ExitUsage {
		t.Fatal("nom invalide accepté")
	}
	p, _ := project.Load(dir)
	navette.New(p).Handoff("author", "")
	// The agent signs what it writes (via=), from its name.
	if code, _, errOut := run("reply", "--id", "bbbb", "--text", "Vu.", "--agent", "Claude Code", dir); code != ExitOK {
		t.Fatal(errOut)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !strings.Contains(string(b), "via=Claude-Code<<}") {
		t.Fatalf("%s", b)
	}
	// A turn with passes: Luna is launched by Glose; the bound session sees
	// the rest (--mine), and its done closes its part.
	h, _ := navette.New(p).History()
	navette.WritePasses(p.State(), &navette.Passes{ID: "plan", Turn: h.Turn(), Linked: "Claude Code",
		Passes: []navette.Pass{{Alias: "Luna", Agent: "opencode", State: navette.PassRunning, IDs: []string{"bbbb"}}}})
	if got := ids("--mine"); strings.Contains(got, "bbbb") || !strings.Contains(got, "aaaa") || !strings.Contains(got, "cccc") {
		t.Fatalf("--mine : %s", got)
	}
	// The routing is fixed: Luna removed from glose set meanwhile still
	// sees her annotation.
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}}})
	if got := ids("--to", "Luna"); got != "bbbb:Luna" {
		t.Fatalf("--to Luna figé : %s", got)
	}
	agents.Save(agents.Config{Aliases: []agents.Alias{{Name: "Sol", Agent: "codex"}, {Name: "Luna", Agent: "opencode"}}})
	if _, out, _ := run("wait", "--agent", "Claude Code", "--timeout", "1s", dir); !strings.Contains(out, "glose list --json --mine") {
		t.Fatalf("wait : %s", out)
	}
	t.Setenv(navette.PassEnv, "Luna")
	if code, out, errOut := run("done", "--agent", "Luna", "--note", "Fait.", dir); code != ExitOK || !strings.Contains(out, "passage est clos") {
		t.Fatalf("done du passage : %d %s %s", code, out, errOut)
	}
	if code, _, errOut := run("done", "--agent", "Luna", dir); code != ExitProblem || !strings.Contains(errOut, "aucun passage Luna") {
		t.Fatalf("second done du passage : %d %s", code, errOut)
	}
	t.Setenv(navette.PassEnv, "")
	if code, out, _ := run("done", "--agent", "Claude Code", "--note", "Ma part.", dir); code != ExitOK || !strings.Contains(out, "passage est clos") {
		t.Fatalf("done de la session liée : %s", out)
	}
	if h, _ := navette.New(p).History(); h.Hand() != "agent" {
		t.Fatal("la main a été remise par un passage")
	}
	if ps := navette.ReadPasses(p.State()); ps.Passes[0].Note != "Fait." || !ps.LinkedDone || ps.LinkedNote != "Ma part." {
		t.Fatalf("%+v", ps)
	}
	// Its part closed, the bound session does not hand over the whole turn.
	if code, _, errOut := run("done", "--agent", "Claude Code", dir); code != ExitProblem || !strings.Contains(errOut, "déjà close") {
		t.Fatalf("second done de la session liée : %d %s", code, errOut)
	}
	// Without passes, done hands over as usual.
	navette.WritePasses(p.State(), nil)
	if code, out, _ := run("done", "--agent", "Claude Code", dir); code != ExitOK || !strings.Contains(out, "remise à l'auteur") {
		t.Fatalf("done ordinaire : %s", out)
	}
}

func TestViaName(t *testing.T) {
	for in, want := range map[string]string{"agent": "", "Sol": "Sol", "Claude Code": "Claude-Code", "Évite": "vite", "a/b": "ab", strings.Repeat("x", 40): strings.Repeat("x", 32)} {
		if got := viaName(in); got != want {
			t.Errorf("%q : %q, attendu %q", in, got, want)
		}
	}
}

// Real use (P10.7): the token of the server is never shown to an agent.
func TestStatusHidesTheTokenFromAnAgent(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	t.Setenv("GLOSE_NO_BROWSER", "1")
	dir := mkproject(t, map[string]string{"a.md": "Un texte.\n"})
	p, _ := project.Load(dir)
	done := make(chan error, 1)
	go func() { done <- server.Serve(p) }()
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := server.Running(p.State()); err == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("serveur non lancé")
		}
	}
	defer func() { run("stop", dir); <-done }()
	if _, out, _ := run("status", dir); !strings.Contains(out, "#t=") {
		t.Fatalf("auteur : %s", out)
	}
	if _, out, _ := run("status", "--agent", "Sol", dir); strings.Contains(out, "#t=") || !strings.Contains(out, "Serveur : en marche, http://127.0.0.1:") {
		t.Fatalf("agent : %s", out)
	}
	if _, out, _ := run("status", "--json", "--agent", "Sol", dir); strings.Contains(out, "#t=") {
		t.Fatalf("agent, JSON : %s", out)
	}
}

// P10.11: the messages of the agent carry its model: GLOSE_MODEL (an agent
// launched by Glose), else the one recorded at glose join.
func TestAgentSignsItsModel(t *testing.T) {
	t.Setenv("GLOSE_AGENT", "")
	t.Setenv("GLOSE_MODEL", "")
	dir := mkproject(t, map[string]string{"a.md": "Un{>>Pourquoi ?<<}{>>glose:v1 id=aaaa by=author<<} texte.\n"})
	p, _ := project.Load(dir)
	navette.New(p).Handoff("author", "")
	navette.TouchJoined(p.State(), "OpenCode", 2, "ABCDEF", "claude-agent/opus#high", navette.ModelByAgent)
	if code, _, errOut := run("reply", "--id", "aaaa", "--text", "Parce que.", "--agent", "OpenCode", dir); code != ExitOK {
		t.Fatal(errOut)
	}
	t.Setenv("GLOSE_MODEL", "gpt-6.1-sol")
	if code, _, errOut := run("reply", "--id", "aaaa", "--text", "Et encore.", "--agent", "Sol", dir); code != ExitOK {
		t.Fatal(errOut)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	if !strings.Contains(string(b), "via=OpenCode model=claude-agent/opus#high<<}") || !strings.Contains(string(b), "via=Sol model=gpt-6.1-sol<<}") {
		t.Fatalf("%s", b)
	}
	if _, out, _ := run("list", dir); !strings.Contains(out, "(OpenCode, claude-agent/opus#high) Parce que.") {
		t.Fatalf("%s", out)
	}
}
