package cli

import (
	"strings"
	"testing"
)

func TestListFiltersTaskAndReplyTask(t *testing.T) {
	t.Setenv("GLOSE_CONFIG_DIR", t.TempDir())
	dir := mkproject(t, map[string]string{"a.md": "{>>Ancien<<}{>>glose:v1 id=abcd by=author<<}{>>Réponse<<}{>>glose:v1 id=efgh re=abcd by=agent task=rewrite<<}\n\n{++Texte++}{>>glose:v1 id=ijkl by=agent task=proofread<<}\n"})
	code, out, err := run("list", "--json", "--task", "rewrite", dir)
	if code != ExitOK || !strings.Contains(out, `"task": "rewrite"`) || strings.Contains(out, "proofread") {
		t.Fatal(code, out, err)
	}
	code, out, err = run("list", "--json", "--task", "proofread", dir)
	if code != ExitOK || !strings.Contains(out, `"task": "proofread"`) || strings.Contains(out, "rewrite") {
		t.Fatal(code, out, err)
	}
	code, _, _ = run("list", "--task", "../x", dir)
	if code != ExitUsage {
		t.Fatal(code)
	}
}
