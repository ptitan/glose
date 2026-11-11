package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReportsInvalidTasks(t *testing.T) {
	dir := mkproject(t, map[string]string{"a.md": "Texte ordinaire.\r\n", "glose.json": `{"tasks":[{"id":"rewrite","name":"Réécrire","icon":"introuvable-icone","prompt":"Relis.","place":["bar"]}]}`})
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	code, out, _ := run("check", "--json", dir)
	if code != ExitProblem || !strings.Contains(out, "icône Lucide inconnue") {
		t.Fatal(code, out)
	}
}
func TestCheckReportsInvalidGlobalTasks(t *testing.T) {
	dir := mkproject(t, map[string]string{"a.md": "Texte ordinaire.\r\n"})
	cfg := t.TempDir()
	t.Setenv("GLOSE_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "agents.json"), []byte(`{"tasks":[{"id":"rewrite","name":"Réécrire","icon":"introuvable-icone","prompt":"Relis.","place":["bar"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run("check", "--json", dir)
	if code != ExitProblem || !strings.Contains(out, "consignes globales") || !strings.Contains(out, "icône Lucide inconnue") {
		t.Fatal(code, out)
	}
}
