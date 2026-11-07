package navette

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/journal"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/review"
	"github.com/ptitan/glose/internal/store"
)

func mkProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, c := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func load(t *testing.T, path string) *Navette {
	t.Helper()
	p, err := project.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return New(p)
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Le défaut du 7 novembre 2026 : l'auteur remet le tour depuis un seul
// fichier du projet ; l'agent modifie un autre fichier. Sa modification doit
// devenir une proposition, et non rester dans le texte sans décision.
func TestHandoverFromOneFileCoversTheWholeProject(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b ancien.\n",
		"glose.json": `{"files":["a.md","b.md"]}`})
	if _, err := load(t, filepath.Join(dir, "a.md")).Handoff(review.Author, "Relis."); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Texte b nouveau.\n"), 0o644)
	// L'agent remet depuis le seul fichier a.md : la remise couvre aussi b.md.
	ho, err := load(t, filepath.Join(dir, "a.md")).Handoff(review.Agent, "Corrigé.")
	if err != nil {
		t.Fatal(err)
	}
	if got := noMeta(readFile(t, dir, "b.md")); got != "Texte b {~~ancien~>nouveau~~}.\n" || ho.Converted != 1 {
		t.Fatalf("b.md = %q, %d converties", got, ho.Converted)
	}
}

// Un fichier créé par l'agent pendant son tour (pris par un dossier du
// profil) devient une proposition d'ajout de tout son texte ; la rejeter le
// laisse vide.
func TestFileCreatedByTheAgentBecomesAnAddition(t *testing.T) {
	dir := mkProject(t, map[string]string{"livre/01.md": "Premier chapitre.\n", "glose.json": `{"files":["livre/"]}`})
	if _, err := load(t, dir).Handoff(review.Author, "Écris la suite."); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "livre", "02.md"), []byte("# Deuxième chapitre\n\nSon texte.\n"), 0o644)
	n := load(t, dir)
	if _, err := n.Handoff(review.Agent, "Chapitre 2."); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, dir, "livre/02.md")
	d := review.Read([]byte(got))
	if len(d.Annotations) == 0 {
		t.Fatalf("aucune proposition : %q", got)
	}
	for _, a := range d.Annotations {
		if a.Kind != critic.Addition || a.By != review.Agent {
			t.Fatalf("annotation inattendue : %v dans %q", a.Kind, got)
		}
	}
	// Tout rejeter rend le fichier vide : rien de l'agent n'entre sans décision.
	if cur := projection.Project([]byte(got), projection.Current); len(cur.Text) != 0 {
		t.Fatalf("vue Actuel non vide : %q", cur.Text)
	}
}

// Un fichier remis puis supprimé par l'agent : la remise est refusée (tout
// ou rien, D-28), avec l'endroit de la copie pour le restaurer.
func TestFileDeletedByTheAgentIsRefused(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b.\n"})
	if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "b.md"))
	n := load(t, dir)
	_, err := n.Handoff(review.Agent, "y")
	var te *TrackError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "b.md") || !strings.Contains(err.Error(), "supprimé") ||
		!strings.Contains(err.Error(), ".glose/snapshots/") {
		t.Fatalf("suppression non refusée : %v", err)
	}
	if st, _, _, _ := n.State(); st.Hand != review.Agent {
		t.Fatal("la main a changé malgré le refus")
	}
	if pbs, err := n.Preview(); err != nil || len(pbs) != 1 {
		t.Fatalf("glose check ne l'annonce pas : %v %v", pbs, err)
	}
}

// Une remise inscrite avant ce correctif, sur une partie du projet : un
// fichier sans copie est refusé, jamais ignoré.
func TestOldPartialHandoverIsRefused(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b.\n"})
	if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	// Réécrit l'événement de tour comme l'ancienne version : a.md seul, sans « complete ».
	jp := filepath.Join(dir, ".glose", "journal.jsonl")
	raw, _ := os.ReadFile(jp)
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e journal.Event
		json.Unmarshal([]byte(line), &e)
		if e.Type == journal.Turn {
			e.TurnEv.Complete = false
			e.TurnEv.Files = e.TurnEv.Files[:1]
			b, _ := json.Marshal(e)
			line = string(b)
		}
		out = append(out, line)
	}
	os.WriteFile(jp, []byte(strings.Join(out, "\n")+"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Texte b changé.\n"), 0o644)
	_, err := load(t, dir).Handoff(review.Agent, "y")
	if err == nil || !strings.Contains(err.Error(), "b.md") || !strings.Contains(err.Error(), "sans copie") {
		t.Fatalf("fichier sans copie non refusé : %v", err)
	}
	if got := readFile(t, dir, "b.md"); got != "Texte b changé.\n" {
		t.Fatalf("fichier modifié malgré le refus : %q", got)
	}
}

// Revue de Codex (1) : une navette faite sur le dossier avant la création
// d'un fichier (le serveur, ouvert avant) le voit pourtant à la reprise de
// la main : le fichier créé par l'agent devient des propositions.
func TestNavetteMadeBeforeTheFileWasCreated(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n"})
	n := load(t, dir) // comme le serveur : chargé une fois, au démarrage
	if _, err := n.Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Nouveau texte.\n"), 0o644)
	if _, err := n.Reclaim("reprise", false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dir, "b.md"); !strings.Contains(got, "{++Nouveau texte.\n++}") {
		t.Fatalf("fichier créé sans proposition : %q", got)
	}
}

// Revue de Codex (2) : un fichier ouvert seul, hors du profil, reste
// couvert : la remise de l'agent depuis le dossier le compare à sa copie.
func TestFileOpenedAloneOutsideTheProfileStaysCovered(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b ancien.\n", "glose.json": `{"files":["a.md"]}`})
	if _, err := load(t, filepath.Join(dir, "b.md")).Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Texte b nouveau.\n"), 0o644)
	if _, err := load(t, dir).Handoff(review.Agent, "y"); err != nil {
		t.Fatal(err)
	}
	if got := noMeta(readFile(t, dir, "b.md")); got != "Texte b {~~ancien~>nouveau~~}.\n" {
		t.Fatalf("b.md = %q", got)
	}
}

// Revue de Codex (3) : un fichier qui existait sans être remis, et que le
// profil prend pendant le tour, n'est pas tenu pour créé (le rejet
// effacerait un texte ancien) : la remise est refusée, le fichier intact.
func TestExistingFileAddedToTheProfileIsRefused(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n", "notes/b.md": "Texte ancien.\n", "glose.json": `{"files":["a.md"]}`})
	if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"files":["a.md","notes/"]}`), 0o644)
	_, err := load(t, dir).Handoff(review.Agent, "y")
	if err == nil || !strings.Contains(err.Error(), "notes/b.md") || !strings.Contains(err.Error(), "entré dans le projet") {
		t.Fatalf("fichier ancien tenu pour créé : %v", err)
	}
	if got := readFile(t, dir, "notes/b.md"); got != "Texte ancien.\n" {
		t.Fatalf("fichier modifié : %q", got)
	}
}

// Revue de Codex (4) : un fichier devenu illisible fait échouer l'aperçu
// (glose check), comme la remise, au lieu d'être comparé à un texte vide.
func TestPreviewRefusesAnUnreadableFile(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n", "b.md": "Texte b.\n"})
	if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Texte \xff b.\n"), 0o644)
	n := load(t, dir)
	if _, err := n.Preview(); !errors.Is(err, ErrFiles) {
		t.Fatalf("aperçu : %v", err)
	}
	if _, err := n.Handoff(review.Agent, "y"); !errors.Is(err, ErrFiles) {
		t.Fatalf("remise : %v", err)
	}
}

// Revue de Codex : un fichier modifié par un autre programme pendant que la
// remise de l'agent convertit un autre fichier n'est pas adopté comme copie :
// la remise est refusée, puis la suivante convertit cette modification.
func TestFileChangedDuringTheHandoverIsNotAdopted(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a ancien.\n", "b.md": "Texte b ancien.\n"})
	if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("Texte a nouveau.\n"), 0o644)
	once := false
	restore := store.SetAfterRename(func() error {
		if !once {
			once = true // l'éditeur de l'agent écrit b.md pendant la conversion de a.md
			os.WriteFile(filepath.Join(dir, "b.md"), []byte("Texte b nouveau.\n"), 0o644)
		}
		return nil
	})
	_, err := load(t, dir).Handoff(review.Agent, "y")
	restore()
	if !errors.Is(err, ErrChangedDuringHandover) || !strings.Contains(err.Error(), "b.md") {
		t.Fatalf("modification concurrente non refusée : %v", err)
	}
	if _, err := load(t, dir).Handoff(review.Agent, "y"); err != nil {
		t.Fatal(err)
	}
	if got := noMeta(readFile(t, dir, "b.md")); got != "Texte b {~~ancien~>nouveau~~}.\n" {
		t.Fatalf("b.md adopté sans proposition : %q", got)
	}
	if got := noMeta(readFile(t, dir, "a.md")); got != "Texte a {~~ancien~>nouveau~~}.\n" {
		t.Fatalf("a.md : %q", got)
	}
}

// Revue de Codex : l'inventaire ne prouve une création que sans doute
// possible. Trois cas où un fichier ancien serait pris pour neuf : un nom
// qui ne diffère que par la casse (même fichier sous Windows), un chemin qui
// passe par un lien symbolique, un fichier modifié avant la remise.
func TestInventoryDoubtsAreRefused(t *testing.T) {
	refused := func(t *testing.T, dir, file string) {
		t.Helper()
		before := readFile(t, dir, file)
		_, err := load(t, dir).Handoff(review.Agent, "y")
		if err == nil || !strings.Contains(err.Error(), "entré dans le projet") {
			t.Fatalf("%s tenu pour créé : %v", file, err)
		}
		if readFile(t, dir, file) != before {
			t.Fatalf("%s modifié", file)
		}
	}
	t.Run("casse", func(t *testing.T) {
		dir := mkProject(t, map[string]string{"a.md": "a\n", "notes/b.md": "Ancien.\n", "glose.json": `{"files":["a.md"]}`})
		if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
			t.Fatal(err)
		}
		os.Rename(filepath.Join(dir, "notes", "b.md"), filepath.Join(dir, "notes", "B.md"))
		os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"files":["a.md","notes/B.md"]}`), 0o644)
		refused(t, dir, "notes/B.md")
	})
	t.Run("lien", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("liens symboliques")
		}
		dir := mkProject(t, map[string]string{"a.md": "a\n", ".cache/b.md": "Ancien.\n", "glose.json": `{"files":["a.md"]}`})
		if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
			t.Fatal(err)
		}
		os.Symlink(".cache", filepath.Join(dir, "alias"))
		os.WriteFile(filepath.Join(dir, "glose.json"), []byte(`{"files":["a.md","alias/b.md"]}`), 0o644)
		refused(t, dir, "alias/b.md")
	})
	t.Run("date", func(t *testing.T) {
		dir := mkProject(t, map[string]string{"a.md": "a\n", "glose.json": `{"files":["a.md","notes/"]}`})
		os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
		if _, err := load(t, dir).Handoff(review.Author, "x"); err != nil {
			t.Fatal(err)
		}
		// Un fichier ancien (restauré d'une sauvegarde avec sa date) apparaît.
		p := filepath.Join(dir, "notes", "b.md")
		os.WriteFile(p, []byte("Ancien.\n"), 0o644)
		old := time.Now().Add(-48 * time.Hour)
		os.Chtimes(p, old, old)
		refused(t, dir, "notes/b.md")
	})
}

// Revue de Codex : un fichier ouvert seul que la découverte écarte
// (AGENTS.md), dans un dossier sans autre Markdown, se remet normalement.
func TestHandoverOfAFileThatDiscoverySkips(t *testing.T) {
	dir := mkProject(t, map[string]string{"AGENTS.md": "Consignes.\n"})
	n := load(t, filepath.Join(dir, "AGENTS.md"))
	if _, err := n.Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Consignes revues.\n"), 0o644)
	if _, err := n.Handoff(review.Agent, "y"); err != nil {
		t.Fatal(err)
	}
	if got := noMeta(readFile(t, dir, "AGENTS.md")); got != "Consignes {++revues++}.\n" && !strings.Contains(got, "revues") {
		t.Fatalf("AGENTS.md : %q", got)
	}
	if !strings.Contains(readFile(t, dir, "AGENTS.md"), "{++") {
		t.Fatalf("modification non convertie : %q", readFile(t, dir, "AGENTS.md"))
	}
}

// Revue de Codex : ouvert sur un seul fichier, l'état signale les
// annotations des autres fichiers (un chapitre créé par l'agent), et ne les
// tient pas pour disparues.
func TestFileOpenedAloneSaysWhatIsElsewhere(t *testing.T) {
	dir := mkProject(t, map[string]string{"a.md": "Texte a.\n"})
	alone := load(t, filepath.Join(dir, "a.md"))
	if _, err := alone.Handoff(review.Author, "x"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("Nouveau.\n"), 0o644)
	if _, err := alone.Handoff(review.Agent, "y"); err != nil {
		t.Fatal(err)
	}
	st, _, _, err := alone.State()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(st.Problems, " | ")
	if !strings.Contains(joined, "autres fichiers du projet (b.md)") || strings.Contains(joined, "disparue") {
		t.Fatalf("état : %q", joined)
	}
}
