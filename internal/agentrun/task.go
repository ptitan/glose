package agentrun

import (
	"encoding/json"
	"fmt"
	"github.com/ptitan/glose/internal/journal"
)

// TaskPrompt appends an author-selected instruction to the existing turn rules.
func TaskPrompt(base, root string, t journal.TaskInfo) string {
	files, _ := json.Marshal(t.Files)
	return base + fmt.Sprintf("\n\nConsigne « %s » :\n%s\nPortée (%s), fichiers relatifs à %s : %s.\nLis d'abord AGENTS.md et CLAUDE.md du dossier de lancement %s et du dossier du texte %s, s'ils existent.\nPour une consigne qui modifie le texte, les modifications libres des fichiers sont permises : elles deviennent des propositions à glose done (D-25) ; glose propose --whole sert aux réécritures d'un bloc.\nDepuis un autre dossier, passe explicitement le dossier du texte aux commandes glose.\n", t.Name, t.Prompt, t.Scope, root, files, t.Workdir, root)
}
