package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ptitan/glose/internal/home"
	"github.com/ptitan/glose/internal/project"
)

// isolateHome gives the test its own registry, and moves the temporary
// directory of the system elsewhere: a project under t.TempDir() then counts
// as an ordinary directory.
func isolateHome(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	other := filepath.Join(base, "systeme-tmp")
	os.MkdirAll(other, 0o700)
	t.Setenv("GLOSE_CONFIG_DIR", filepath.Join(base, "config"))
	t.Setenv("TMPDIR", other)
	t.Setenv("TMP", other)
	t.Setenv("TEMP", other)
	t.Setenv("GLOSE_AGENT", "")
	return base
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// glose init inscribes the project; glose init on a subdirectory is refused
// and inscribes nothing; glose home lists it and forgets it.
func TestInitRegistersAndHomeForgets(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "livre")
	writeTree(t, dir, map[string]string{"a.md": "Texte.\n"})
	if code, _, errOut := run("init", dir); code != ExitOK {
		t.Fatalf("init : %d %q", code, errOut)
	}
	if code, out, _ := run("home", "--list"); code != ExitOK || !strings.Contains(out, dir) || !strings.Contains(out, home.ID(dir)) {
		t.Fatalf("home : %d %q", code, out)
	}
	if code, out, errOut := run("home", "--forget", dir); code != ExitOK || !strings.Contains(out, "Retiré") {
		t.Fatalf("forget : %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "glose.json")); err != nil {
		t.Fatal("projet touché par --forget")
	}
	if code, out, _ := run("home", "--list"); code != ExitOK || !strings.Contains(out, "Aucun projet") {
		t.Fatalf("home vide : %d %q", code, out)
	}
	if code, _, errOut := run("home", "--forget", dir); code != ExitProblem || !strings.Contains(errOut, "pas inscrit") {
		t.Fatalf("forget d'un inconnu : %d %q", code, errOut)
	}
}

// A registry that cannot be read never stops « glose init », nor is it
// overwritten.
func TestUnreadableRegistryDoesNotStopInit(t *testing.T) {
	base := isolateHome(t)
	path := filepath.Join(base, "config", "projets.json")
	writeTree(t, filepath.Dir(path), map[string]string{"projets.json": "{ abîmé"})
	dir := filepath.Join(base, "livre")
	writeTree(t, dir, map[string]string{"a.md": "Texte.\n"})
	code, out, errOut := run("init", dir)
	if code != ExitOK || !strings.Contains(errOut, "non inscrit") || !strings.Contains(out, "Profil créé") {
		t.Fatalf("init : %d %q %q", code, out, errOut)
	}
	if b, _ := os.ReadFile(path); string(b) != "{ abîmé" {
		t.Fatalf("registre écrasé : %q", b)
	}
	if code, _, errOut := run("home", "--list"); code != ExitProblem || !strings.Contains(errOut, "illisible") {
		t.Fatalf("home : %d %q", code, errOut)
	}
}

// An agent launched by Glose does not see the other projects.
func TestHomeRefusedToAgent(t *testing.T) {
	base := isolateHome(t)
	t.Setenv("GLOSE_AGENT", "Sol")
	for _, args := range [][]string{{"home"}, {"home", "--json"}, {"home", "--forget", base}} {
		if code, out, errOut := run(args...); code != ExitProblem || out != "" || !strings.Contains(errOut, "réservé à l'auteur") {
			t.Errorf("%v : %d %q %q", args, code, out, errOut)
		}
	}
}

func keysOf(t *testing.T, v any) []string {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("objet attendu : %v", v)
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The schema of « glose home --json » (docs/SPEC.md, F28), with a server in
// its state: the address appears without the token, the code of a binding of
// an agent does not appear, and nothing is written in the projects.
func TestHomeJSONSchemaWithoutToken(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "livre")
	writeTree(t, dir, map[string]string{
		"a.md":       "# Titre\n\nUn {>>remarque<<} texte.\n",
		"glose.json": `{"title":"Livre","project":{"type":"article","status":"review","deadline":"2099-01-01","nextAction":"Relire"}}`,
	})
	gone := filepath.Join(base, "disparu")
	writeTree(t, gone, map[string]string{"a.md": "T\n", "glose.json": "{}"})
	for _, d := range []string{dir, gone} {
		if code, _, errOut := run("init", d); code != ExitOK {
			t.Fatal(errOut)
		}
	}
	os.RemoveAll(gone)
	const token = "6a6574f06e2d7365637265742d6465"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"source":"x","build":"y"}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	state := filepath.Join(dir, ".glose")
	writeTree(t, state, map[string]string{
		"server.json": fmt.Sprintf(`{"pid":42,"port":%s,"token":%q,"url":"http://%s/#t=%s"}`, u.Port(), token, u.Host, token),
		"agent.json":  fmt.Sprintf(`{"name":"Sol","since":%[1]q,"seen":%[1]q,"activity":"joined","turn":1,"link":"LIEN-CODE"}`, "2026-11-08T09:00:00Z"),
	})
	before, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	code, out, errOut := run("home", "--json")
	if code != ExitOK || errOut != "" {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, secret := range []string{token, "#t=", "LIEN-CODE"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q dans la sortie :\n%s", secret, out)
		}
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(after) != string(before) {
		t.Error("texte touché")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keysOf(t, doc), " "); got != "projects totals" {
		t.Fatalf("clés : %s", got)
	}
	if got := strings.Join(keysOf(t, doc["totals"]), " "); got != "agentHand annotations dueSoon inReview overdue projects unreadable" {
		t.Fatalf("clés de totals : %s", got)
	}
	projects := doc["projects"].([]any)
	if len(projects) != 2 {
		t.Fatalf("%d projets", len(projects))
	}
	p := projects[0].(map[string]any)
	if got := strings.Join(keysOf(t, p), " "); got != "agent annotations hand id issues lastActivity lastHandover meta opened problems profileHash progress root round server state title warnings" {
		t.Fatalf("clés d'un projet : %s", got)
	}
	if got := strings.Join(keysOf(t, p["meta"]), " "); got != "deadline nextAction status type" {
		t.Fatalf("clés de meta : %s", got)
	}
	if got := strings.Join(keysOf(t, p["agent"]), " "); got != "activity fresh launched listening name seen taken" {
		t.Fatalf("clés de agent : %s", got)
	}
	if p["title"] != "Livre" || p["state"] != "ok" || p["hand"] != "author" || p["annotations"] != 1.0 || p["id"] != home.ID(dir) {
		t.Fatalf("%v", p)
	}
	s := p["server"].(map[string]any)
	if s["url"] != srv.URL || s["pid"] != 42.0 {
		t.Fatalf("serveur %v", s)
	}
	missing := projects[1].(map[string]any)
	if missing["state"] != "missing" || missing["message"] == nil || missing["server"] != nil {
		t.Fatalf("%v", missing)
	}
	if code, out, _ := run("home", "--list"); code != ExitOK || !strings.Contains(out, "dossier introuvable") || !strings.Contains(out, "Prochaine action : Relire") || strings.Contains(out, token) {
		t.Fatalf("liste : %d %q", code, out)
	}
}

// glose project shows the metadata, changes the values given and nothing else,
// clears with an empty value, and refuses an invalid value without writing.
func TestProjectCommand(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "livre")
	before := "{\r\n  \"title\": \"Livre\",\r\n  \"files\": [\"a.md\"]\r\n}\r\n"
	writeTree(t, dir, map[string]string{"a.md": "T\n", "glose.json": before})
	if code, out, _ := run("project", dir); code != ExitOK || !strings.Contains(out, "statut : —") {
		t.Fatalf("%d %q", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "glose.json")); string(b) != before {
		t.Fatalf("lecture : écrit %q", b)
	}
	code, out, errOut := run("project", "--status", "review", "--deadline", "2026-12-31", "--next-action", "Relire la partie 2", "--type", "article", dir)
	if code != ExitOK || !strings.Contains(out, "en révision (review)") || !strings.Contains(out, "Relire la partie 2") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	want := "{\r\n  \"title\": \"Livre\",\r\n  \"files\": [\"a.md\"],\r\n  \"project\": {\r\n    \"type\": \"article\",\r\n    \"status\": \"review\",\r\n    \"deadline\": \"2026-12-31\",\r\n    \"nextAction\": \"Relire la partie 2\"\r\n  }\r\n}\r\n"
	if b, _ := os.ReadFile(filepath.Join(dir, "glose.json")); string(b) != want {
		t.Fatalf("\nattendu %q\nobtenu  %q", want, b)
	}
	// From a subdirectory, a value starting with a dash, an empty value.
	os.MkdirAll(filepath.Join(dir, "sous"), 0o755)
	if code, _, errOut := run("project", "--next-action", "-h pas une aide", "--deadline", "", filepath.Join(dir, "sous")); code != ExitOK {
		t.Fatalf("%d %q", code, errOut)
	}
	code, out, _ = run("project", "--json", dir)
	var doc struct {
		Meta     map[string]string
		Warnings []string
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || code != ExitOK {
		t.Fatalf("%d %v %q", code, err, out)
	}
	if doc.Meta["nextAction"] != "-h pas une aide" || doc.Meta["deadline"] != "" || doc.Meta["status"] != "review" || doc.Warnings == nil {
		t.Fatalf("%+v", doc)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "glose.json"))
	for _, args := range [][]string{{"--status", "fini"}, {"--deadline", "demain"}, {"--next-action", "deux\nlignes"}, {"--type", strings.Repeat("x", 41)}} {
		if code, _, errOut := run(append(append([]string{"project"}, args...), dir)...); code != ExitUsage || errOut == "" {
			t.Errorf("%v : %d %q", args, code, errOut)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "glose.json")); string(b) != string(saved) {
		t.Fatalf("refus : écrit %q", b)
	}
	// An agent launched by Glose reads, and does not write.
	t.Setenv("GLOSE_AGENT", "Sol")
	if code, _, errOut := run("project", "--status", "done", dir); code != ExitProblem || !strings.Contains(errOut, "réservé à l'auteur") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, _ := run("project", dir); code != ExitOK {
		t.Fatal("lecture refusée à l'agent")
	}
	t.Setenv("GLOSE_AGENT", "")
	// No profile: said, and nothing created.
	bare := filepath.Join(base, "nu")
	writeTree(t, bare, map[string]string{"a.md": "T\n", ".git": ""})
	if code, _, errOut := run("project", "--status", "done", bare); code != ExitProblem || !strings.Contains(errOut, "glose init") {
		t.Fatalf("%d %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(bare, "glose.json")); err == nil {
		t.Fatal("profil créé")
	}
}

// templateTree writes a template in the configuration of the test.
func templateTree(t *testing.T, base, name string, files map[string]string) {
	t.Helper()
	writeTree(t, filepath.Join(base, "config", "gabarits", name), files)
}

func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		} else {
			out[p] = "<dossier>"
		}
		return nil
	})
	return out
}

func TestInitTypeInAnEmptyDirectory(t *testing.T) {
	base := isolateHome(t)
	templateTree(t, base, "article", map[string]string{
		"glose.json":        `{"title":"Article","files":["plan.md","parties/"],"project":{"status":"draft"},"agent":{"instructions":"Soyez bref."}}`,
		"AGENTS.md":         "# Consignes de l'article\n",
		"plan.md":           "# Plan\n",
		"parties/01.md":     "# Partie\n",
		"sources/lisez.txt": "Sources.\n",
	})
	dir := filepath.Join(base, "projet")
	os.MkdirAll(dir, 0o755)
	code, out, errOut := run("init", "--type", "article", dir)
	if code != ExitOK || errOut != "" {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if !strings.Contains(out, "Gabarit « article »") || !strings.Contains(out, "Profil repris du gabarit") {
		t.Fatalf("%q", out)
	}
	for f, want := range map[string]string{"plan.md": "# Plan\n", "parties/01.md": "# Partie\n", "sources/lisez.txt": "Sources.\n"} {
		if b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f))); string(b) != want {
			t.Errorf("%s : %q", f, b)
		}
	}
	// The type defaults to the name of the template; the rest of the fragment stays.
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.Type != "article" || p.Meta.Status != "draft" || p.Agent.Instructions != "Soyez bref." || p.Title != "Article" {
		t.Fatalf("%+v", p)
	}
	// The instructions of the template, then the section, managed as usual.
	b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(b), "# Consignes de l'article\n") || !strings.Contains(string(b), sectionStart) {
		t.Fatalf("%q", b)
	}
	// Inscribed in the registry.
	if _, list, _ := run("home", "--list"); !strings.Contains(list, dir) {
		t.Fatalf("non inscrit : %q", list)
	}
	// Run again: no effect, and it says so.
	before := snapshotTree(t, dir)
	code, out, errOut = run("init", "--type", "article", dir)
	if code != ExitOK || !strings.Contains(out, "0 élément créé") || !strings.Contains(out, "existe déjà") || !strings.Contains(out, "à jour") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	after := snapshotTree(t, dir)
	if len(before) != len(after) {
		t.Fatal("relance : entrées changées")
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("relance : %s changé", k)
		}
	}
}

// In a directory already filled, nothing is overwritten and everything skipped
// is said; a profile that exists keeps every byte.
func TestInitTypeInAFilledDirectory(t *testing.T) {
	base := isolateHome(t)
	templateTree(t, base, "cours", map[string]string{
		"glose.json":      `{"project":{"type":"cours"}}`,
		"plan.md":         "# Plan du gabarit\n",
		"seances/01.md":   "# Séance\n",
		"lien/fichier.md": "# À travers un lien\n",
		"bloque/x.md":     "# Sous un fichier\n",
	})
	dir := filepath.Join(base, "projet")
	outside := filepath.Join(base, "dehors")
	os.MkdirAll(outside, 0o755)
	profile := "{\r\n \"title\":  \"À moi\"\r\n}\r\n"
	writeTree(t, dir, map[string]string{"glose.json": profile, "plan.md": "# Mon plan\r\nÉcrit à la main\n", "bloque": "un fichier\n"})
	link := true
	if err := os.Symlink(outside, filepath.Join(dir, "lien")); err != nil {
		link = false
		t.Log("liens symboliques indisponibles")
	}
	code, out, errOut := run("init", "--type", "cours", dir)
	if code != ExitOK {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	for _, want := range []string{"glose.json : existe déjà", "plan.md : existe déjà", "bloque/ : un fichier porte ce nom", "Profil existant"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q absent de %q", want, out)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "glose.json")); string(b) != profile {
		t.Fatalf("profil touché : %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "plan.md")); string(b) != "# Mon plan\r\nÉcrit à la main\n" {
		t.Fatalf("plan touché : %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "seances", "01.md")); string(b) != "# Séance\n" {
		t.Fatalf("séance : %q", b)
	}
	if link {
		if !strings.Contains(out, "lien/ : lien symbolique") {
			t.Errorf("lien non signalé : %q", out)
		}
		if _, err := os.Stat(filepath.Join(outside, "fichier.md")); err == nil {
			t.Fatal("écrit à travers un lien")
		}
	}
}

// A name without a template lists those that exist, and writes nothing; a
// template that cannot serve is refused as a whole.
func TestInitTypeRefusals(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "projet")
	writeTree(t, dir, map[string]string{"a.md": "T\n"})
	before := snapshotTree(t, dir)
	if code, _, errOut := run("init", "--type", "cours", dir); code != ExitProblem || !strings.Contains(errOut, "n'en contient aucun") {
		t.Fatalf("%d %q", code, errOut)
	}
	templateTree(t, base, "cours", map[string]string{"a.md": "T\n"})
	templateTree(t, base, "article", map[string]string{"a.md": "T\n"})
	code, _, errOut := run("init", "--type", "seminaire", dir)
	if code != ExitProblem || !strings.Contains(errOut, "article, cours") {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, name := range []string{"../cours", "..", ".cache", "a/b"} {
		if code, _, errOut := run("init", "--type", name, dir); code != ExitProblem || !strings.Contains(errOut, "aucun gabarit") {
			t.Errorf("%q : %d %q", name, code, errOut)
		}
	}
	templateTree(t, base, "git", map[string]string{"a.md": "T\n", ".git/hooks/pre-commit": "#!/bin/sh\n"})
	templateTree(t, base, "etat", map[string]string{"a.md": "T\n", ".glose/journal.jsonl": "{}\n"})
	templateTree(t, base, "imbrique", map[string]string{"a.md": "T\n", "sous/glose.json": "{}"})
	templateTree(t, base, "abime", map[string]string{"a.md": "T\n", "glose.json": "{ abîmé"})
	templateTree(t, base, "inconnu", map[string]string{"a.md": "T\n", "glose.json": `{"inconnu":1}`})
	for _, name := range []string{"git", "etat", "imbrique", "abime", "inconnu"} {
		if code, _, errOut := run("init", "--type", name, dir); code != ExitProblem || !strings.Contains(errOut, "gabarit refusé") {
			t.Errorf("%s : %d %q", name, code, errOut)
		}
	}
	gab := filepath.Join(base, "config", "gabarits")
	templateTree(t, base, "lien", map[string]string{"a.md": "T\n"})
	if os.Symlink(filepath.Join(base, "dehors"), filepath.Join(gab, "lien", "b.md")) == nil {
		if code, _, errOut := run("init", "--type", "lien", dir); code != ExitProblem || !strings.Contains(errOut, "lien symbolique") {
			t.Errorf("lien : %d %q", code, errOut)
		}
		os.Symlink(filepath.Join(gab, "cours"), filepath.Join(gab, "alias"))
		if code, _, errOut := run("init", "--type", "alias", dir); code != ExitProblem || !strings.Contains(errOut, "lien symbolique") {
			t.Errorf("gabarit en lien : %d %q", code, errOut)
		}
	}
	after := snapshotTree(t, dir)
	if len(before) != len(after) {
		t.Fatalf("refus : écrit quelque chose %v", after)
	}
	if code, _, _ := run("init", "--print", "--type", "cours"); code != ExitUsage {
		t.Fatal("--print avec --type accepté")
	}
}

// At the end, the tools required by the profile that the agent does not declare
// are said; the project is ready all the same.
func TestInitTypeWarnsOfMissingTools(t *testing.T) {
	base := isolateHome(t)
	templateTree(t, base, "note", map[string]string{
		"glose.json": `{"agent":{"requires":["skill:outil-qui-n-existe-pas"]}}`,
		"a.md":       "T\n",
	})
	dir := filepath.Join(base, "sans-agent")
	os.MkdirAll(dir, 0o755)
	code, _, errOut := run("init", "--type", "note", dir)
	if code != ExitOK || !strings.Contains(errOut, "aucun agent configuré") {
		t.Fatalf("%d %q", code, errOut)
	}
	writeTree(t, filepath.Join(base, "config"), map[string]string{"agents.json": `{"version":1,"default":"Sol","aliases":[{"name":"Sol","agent":"opencode"}]}`})
	dir = filepath.Join(base, "avec-agent")
	os.MkdirAll(dir, 0o755)
	code, _, errOut = run("init", "--type", "note", dir)
	if code != ExitOK || !strings.Contains(errOut, "outil requis absent chez Sol : skill:outil-qui-n-existe-pas") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// With a template, nothing goes through a link of the project: neither the
// section in an instructions file that is a link, nor a new profile in place of
// a link without a target. Without a template, the link is followed as ever.
func TestInitTypeWritesNothingThroughALink(t *testing.T) {
	base := isolateHome(t)
	templateTree(t, base, "note", map[string]string{"AGENTS.md": "# Du gabarit\n", "a.md": "T\n"})
	outside := filepath.Join(base, "dehors.md")
	os.WriteFile(outside, []byte("Extérieur.\n"), 0o644)
	dir := filepath.Join(base, "projet")
	os.MkdirAll(dir, 0o755)
	if err := os.Symlink(outside, filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Skip("liens symboliques indisponibles")
	}
	code, out, errOut := run("init", "--type", "note", dir)
	if code != ExitOK || !strings.Contains(out, "lien symbolique") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if b, _ := os.ReadFile(outside); string(b) != "Extérieur.\n" {
		t.Fatalf("écrit à travers le lien : %q", b)
	}
	dangling := filepath.Join(base, "pendant")
	os.MkdirAll(dangling, 0o755)
	profile := filepath.Join(dangling, "glose.json")
	os.Symlink(filepath.Join(base, "absent.json"), profile)
	code, out, errOut = run("init", "--type", "note", dangling)
	if code != ExitOK {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if fi, err := os.Lstat(profile); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("lien remplacé : %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(base, "absent.json")); err == nil {
		t.Fatal("écrit à travers le lien")
	}
	// --to through a linked directory, and out of the project.
	linked := filepath.Join(base, "lie")
	ext := filepath.Join(base, "consignes-dehors")
	os.MkdirAll(linked, 0o755)
	os.MkdirAll(ext, 0o755)
	os.Symlink(ext, filepath.Join(linked, "consignes"))
	if code, out, errOut := run("init", "--type", "note", "--to", filepath.Join("consignes", "AGENTS.md"), linked); code != ExitOK || !strings.Contains(out, "non écrite") {
		t.Fatalf("dossier lié : %d %q %q", code, out, errOut)
	}
	// Out of the project: refused before anything is written (audit A6).
	for _, to := range []string{filepath.Join("..", "hors.md"), filepath.Join(ext, "abs.md")} {
		if code, out, errOut := run("init", "--type", "note", "--to", to, linked); code != ExitProblem || !strings.Contains(errOut, "--to") {
			t.Fatalf("%s : %d %q %q", to, code, out, errOut)
		}
	}
	if entries, _ := os.ReadDir(ext); len(entries) != 0 {
		t.Fatalf("écrit à travers le dossier lié : %v", entries)
	}
	if _, err := os.Stat(filepath.Join(base, "hors.md")); err == nil {
		t.Fatal("écrit hors du projet")
	}
	// Sans gabarit, inchangé.
	if code, _, errOut := run("init", dir); code != ExitOK {
		t.Fatalf("%d %q", code, errOut)
	}
	if b, _ := os.ReadFile(outside); !strings.Contains(string(b), sectionStart) {
		t.Fatalf("sans --type, le lien n'est plus suivi : %q", b)
	}
}

// A template without any text yet: the required tools are still checked.
func TestInitTypeChecksToolsWithoutAText(t *testing.T) {
	base := isolateHome(t)
	templateTree(t, base, "vide", map[string]string{"glose.json": `{"agent":{"requires":["skill:outil-absent"]}}`})
	dir := filepath.Join(base, "projet")
	os.MkdirAll(dir, 0o755)
	if code, _, errOut := run("init", "--type", "vide", dir); code != ExitOK || !strings.Contains(errOut, "skill:outil-absent") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// Without --type, glose init is unchanged: no gabarit read, nothing more said.
func TestInitWithoutTypeIsUnchanged(t *testing.T) {
	base := isolateHome(t)
	templateTree(t, base, "cours", map[string]string{"plan.md": "# Plan\n", "glose.json": `{"project":{"type":"cours"}}`})
	dir := filepath.Join(base, "projet")
	writeTree(t, dir, map[string]string{"a.md": "T\n"})
	code, out, errOut := run("init", dir)
	if code != ExitOK || errOut != "" || strings.Contains(out, "Gabarit") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "plan.md")); err == nil {
		t.Fatal("gabarit recopié")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "glose.json"))
	if strings.Contains(string(b), "project") {
		t.Fatalf("profil : %s", b)
	}
}

// glose init writes the profile and the instructions: the author's alone, and
// never outside the project (audit A6).
func TestInitRefusedToAnAgentAndOutsideTheProject(t *testing.T) {
	base := isolateHome(t)
	dir := filepath.Join(base, "projet")
	writeTree(t, dir, map[string]string{"a.md": "T\n"})
	before := snapshotTree(t, dir)
	t.Setenv("GLOSE_AGENT", "Sol")
	if code, _, errOut := run("init", dir); code != ExitProblem || !strings.Contains(errOut, "réservé à l'auteur") {
		t.Fatalf("agent : %d %q", code, errOut)
	}
	if code, out, _ := run("init", "--print"); code != ExitOK || !strings.Contains(out, "Navette") {
		t.Fatalf("--print refusé à l'agent : %d", code)
	}
	t.Setenv("GLOSE_AGENT", "")
	outside := filepath.Join(base, "ailleurs.md")
	for _, to := range []string{"../ailleurs.md", "sous/../../ailleurs.md", outside} {
		if code, _, errOut := run("init", "--to", to, dir); code != ExitProblem || !strings.Contains(errOut, "--to") {
			t.Errorf("%s : %d %q", to, code, errOut)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("écrit hors du projet")
	}
	if after := snapshotTree(t, dir); len(after) != len(before) {
		t.Fatalf("refus : écrit quelque chose %v", after)
	}
	if code, _, errOut := run("init", "--to", "consignes/AGENTS.md", dir); code != ExitOK {
		t.Fatalf("--to dans le projet : %d %q", code, errOut)
	}
}
