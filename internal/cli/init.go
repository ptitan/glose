package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

// NavetteSection is the section « Navette Glose » that « glose init » writes
// in the instructions of the project's agent (SPEC F21). It is enough for an
// agent with no other instruction to lead a turn.
const NavetteSection = `## Navette Glose

Ce texte est relu avec Glose. L'auteur le lit mis en forme dans son navigateur, le commente et propose des modifications. Vous répondez et proposez les vôtres. **L'auteur seul décide.** Tout s'écrit dans les fichiers Markdown, en CriticMarkup ; l'histoire est gardée par Glose.

Tant qu'une navette est en cours (` + "`glose status`" + ` indique un tour), ces règles l'emportent sur toute autre consigne de ce fichier relative aux annotations : vous ne les appliquez pas, vous ne les retirez pas.

### Mener votre tour

1. ` + "`glose status`" + ` dit à qui est la main. Si elle est à l'auteur, attendez : ` + "`glose wait`" + ` rend la main quand l'auteur vous remet son tour (avec ` + "`--timeout 10m`" + ` si votre environnement interrompt les commandes longues ; relancez-la tant qu'elle se termine par « délai écoulé »).
2. ` + "`glose list --json`" + ` donne les annotations. Chacune a un ` + "`id`" + `, un ` + "`kind`" + ` (` + "`comment`" + `, ` + "`highlight`" + `, ` + "`addition`" + `, ` + "`deletion`" + `, ` + "`substitution`" + `), le passage visé (` + "`text`" + `, et ` + "`new`" + ` pour un remplacement), son auteur (` + "`by`" + `), son fil (` + "`messages`" + `) et le paragraphe où elle se trouve, tel qu'il se lit (` + "`context`" + `). Les annotations de l'auteur sont votre travail.
3. Pour chaque annotation, selon ce qu'elle demande :
   - **répondre** dans son fil : ` + "`glose reply --id ID --text \"…\"`" + ` ;
   - **modifier le texte** : éditez directement le fichier avec vos outils habituels, comme si vous écriviez le texte final, hors des marques. Quand vous remettez votre tour, Glose convertit vos modifications en propositions mot à mot, que l'auteur acceptera ou rejettera. Pour ajouter du texte à l'endroit d'un commentaire, écrivez-le juste après le commentaire et son fil ;
   - **corriger un passage commenté** par l'auteur (` + "`{==passage==}{>>remarque<<}`" + `) : réécrivez le texte à l'intérieur de ` + "`{== ==}`" + `, et seulement lui, ou lancez ` + "`glose propose --id ID --new \"nouveau passage\" --note \"…\"`" + `. À la remise, le surlignage devient une proposition qui reprend le fil de l'auteur ;
   - **corriger une proposition de l'auteur** (` + "`{++…++}`" + `, ` + "`{--…--}`" + `, ` + "`{~~…~>…~~}`" + `, quand il vous demande d'en revoir le texte, ou qu'elle contient une erreur) : réécrivez seulement ce qu'elle propose (le texte d'un ajout, ce qui suit ` + "`~>`" + ` dans un remplacement), ou lancez ` + "`glose propose --id ID --new \"texte corrigé\" --note \"pourquoi\"`" + `. Sa proposition reste telle qu'elle est ; votre correction s'ajoute à son fil, et l'auteur peut l'accepter d'un clic. Vos propres propositions encore en attente, corrigez-les de la même façon : elles changent sur place ;
   - **proposer une modification précise avec son explication** : ` + "`glose propose --file F --find \"texte exact\" --new \"nouveau texte\" --note \"pourquoi\"`" + ` (` + "`--new \"\"`" + ` pour supprimer ; ` + "`--after \"texte exact\" --new \"…\"`" + ` pour ajouter après ce texte). Sans autre option, la réécriture devient des propositions mot à mot, que l'auteur décide une à une : c'est le bon choix pour des corrections ponctuelles et indépendantes. Pour une réécriture d'ensemble (une phrase, un paragraphe) qui n'a de sens qu'entière, ajoutez ` + "`--whole`" + ` : elle devient une seule proposition, que l'auteur accepte ou rejette en bloc ;
   - **commenter** un passage : ` + "`glose comment --file F --find \"passage\" --text \"…\"`" + `.
   - **notes de bas de page** : ajoutez l'appel ` + "`[^n]`" + ` et sa définition ` + "`[^n]: …`" + ` (en fin de fichier), ou écrivez ` + "`^[texte de la note]`" + ` dans le ` + "`--new`" + ` de ` + "`glose propose`" + `. Pour supprimer une note, effacez son appel : sa définition suit. L'auteur décide l'appel et la définition d'un seul geste.

   Le texte cité par ` + "`--find`" + ` ou ` + "`--after`" + ` doit figurer une seule fois dans le fichier, hors des annotations, tel qu'il y est écrit.

   Un texte long, sur plusieurs lignes ou avec des guillemets, passe sans échappement par l'entrée standard : donnez ` + "`-`" + ` comme valeur de l'option (une seule par commande ; le dernier saut de ligne est retiré), par exemple ` + "`glose reply --id ID --text - <<'FIN'`" + `, le texte, puis ` + "`FIN`" + `. Avec ` + "`--json`" + `, ` + "`reply`" + `, ` + "`propose`" + `, ` + "`comment`" + ` et ` + "`done`" + ` répondent en JSON (identifiants créés ; pour ` + "`done`" + `, le curseur ` + "`seq`" + ` de ` + "`glose wait --after`" + `).
4. Vérifiez avec ` + "`glose check`" + ` (il annonce ce que ` + "`glose done`" + ` refuserait), puis remettez votre tour : ` + "`glose done --note \"ce que vous avez fait, en une ou deux phrases\"`" + `. Si ` + "`glose done`" + ` refuse, il dit pourquoi et où : corrigez le fichier, puis relancez-le.

### Ce qui est interdit

- Accepter, rejeter ou résoudre une annotation : c'est la décision de l'auteur. Ne supprimez jamais une marque (` + "`{++ ++}`" + `, ` + "`{-- --}`" + `, ` + "`{~~ ~> ~~}`" + `, ` + "`{== ==}`" + `, ` + "`{>> <<}`" + `) et ne modifiez pas le texte à l'intérieur d'une marque existante, sauf le passage d'un surlignage ` + "`{== ==}`" + ` et ce que propose une proposition (voir plus haut).
- Écrire ou modifier un commentaire technique ` + "`{>>glose:v1 …<<}`" + ` : Glose les écrit.
- Écrire dans les fichiers quand la main est à l'auteur.
- Mêler au même endroit une modification libre et une marque écrite à la main : préférez les modifications libres, ou ` + "`glose propose`" + `.
- Modifier un mot collé contre un commentaire existant ` + "`{>>…<<}`" + ` (le commentaire deviendrait une réponse à votre proposition) : laissez au moins un mot inchangé entre les deux. Un ajout, lui, peut s'écrire juste après le commentaire et son fil.
`

// Markers around the section in the instructions file, so that « glose
// init » can replace it with a newer version.
const (
	sectionStart = "<!-- glose:navette (écrit par glose init) -->"
	sectionEnd   = "<!-- /glose:navette -->"
)

func cmdInit(args []string, stdout, stderr io.Writer) int {
	var print bool
	var to string
	path, ok := parse("init", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&print, "print", false, "")
		fs.StringVar(&to, "to", "", "")
	})
	if !ok {
		return ExitUsage
	}
	if print {
		io.WriteString(stdout, NavetteSection)
		return ExitOK
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return failure(stderr, err)
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return failure(stderr, fmt.Errorf("%s n'est pas un dossier", path))
	}
	// Inside a project already (a profile above): a profile here would make
	// a separate project, with its own navette (D-35).
	if real, err := filepath.EvalSymlinks(root); err == nil {
		if above, ok := project.FindRoot(filepath.Dir(real)); ok && above != real {
			if _, err := os.Stat(filepath.Join(real, project.ProfileName)); errors.Is(err, os.ErrNotExist) {
				return failure(stderr, fmt.Errorf("%s est dans le projet Glose de %s : lancez « glose init » à sa racine ; ce dossier s'ouvre déjà avec lui (glose open %s)", path, above, path))
			}
		}
	}
	// The profile, if there is none.
	prof := filepath.Join(root, project.ProfileName)
	if _, err := os.Stat(prof); errors.Is(err, os.ErrNotExist) {
		// What Glose would take without a list (project.Discover): the files
		// of the root by name, each subdirectory as a whole, so that a new
		// chapter is taken without changing the profile.
		found, err := project.Discover(root)
		if err != nil {
			return failure(stderr, err)
		}
		files := []string{}
		var dirs []string
		count := map[string]int{}
		for _, f := range found {
			d, _, sub := strings.Cut(f, "/")
			if !sub {
				files = append(files, f)
				continue
			}
			if count[d] == 0 {
				dirs = append(dirs, d)
			}
			count[d]++
		}
		top := len(files)
		for _, d := range dirs {
			files = append(files, d+"/")
		}
		data, _ := json.MarshalIndent(map[string]any{"title": filepath.Base(root), "files": files}, "", "  ")
		if err := store.WriteAtomic(prof, append(data, '\n'), 0o644); err != nil && !errors.Is(err, store.ErrNotSynced) {
			return failure(stderr, err)
		}
		fmt.Fprintf(stdout, "Profil créé : %s, %d fichier%s Markdown", project.ProfileName, len(found), plural(len(found)))
		if len(dirs) == 0 {
			fmt.Fprintln(stdout, " ; modifiez la liste « files » au besoin.")
		} else {
			fmt.Fprintf(stdout, " : %d à la racine", top)
			for _, d := range dirs {
				fmt.Fprintf(stdout, ", %d dans %s/", count[d], d)
			}
			fmt.Fprintf(stdout, ".\nRetirez de « files » les dossiers qui ne sont pas le texte (notes, consignes, sources), ou écartez-les par « exclude » ; voir « glose help profile ».\n")
		}
	} else {
		fmt.Fprintf(stdout, "Profil existant : %s, inchangé.\n", project.ProfileName)
	}
	// The section, in the instructions of the agent.
	if to == "" {
		to = "AGENTS.md"
		if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); errors.Is(err, os.ErrNotExist) {
			if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
				to = "CLAUDE.md"
			}
		}
	}
	target := filepath.Join(root, to)
	old, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failure(stderr, err)
	}
	block := sectionStart + "\n" + NavetteSection + sectionEnd + "\n"
	var out []byte
	if i := bytes.Index(old, []byte(sectionStart)); i >= 0 {
		j := bytes.Index(old[i:], []byte(sectionEnd))
		if j < 0 {
			return failure(stderr, fmt.Errorf("%s : section Glose sans sa fin (%s) ; corrigez le fichier", to, sectionEnd))
		}
		end := i + j + len(sectionEnd)
		if end < len(old) && old[end] == '\n' {
			end++
		}
		out = append(append(append([]byte{}, old[:i]...), block...), old[end:]...)
	} else {
		out = append([]byte{}, old...)
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n\n")) {
			if bytes.HasSuffix(out, []byte("\n")) {
				out = append(out, '\n')
			} else {
				out = append(out, '\n', '\n')
			}
		}
		out = append(out, block...)
	}
	if bytes.Equal(out, old) {
		fmt.Fprintf(stdout, "Section « Navette Glose » de %s : à jour.\n", to)
		return ExitOK
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(target); err == nil {
		perm = fi.Mode().Perm()
	}
	if len(old) > 0 {
		if _, err := store.WriteIfUnchanged(target, store.Hash(old), out); err != nil && !errors.Is(err, store.ErrNotSynced) {
			return failure(stderr, err)
		}
	} else if err := store.WriteAtomic(target, out, perm); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return failure(stderr, err)
	}
	fmt.Fprintf(stdout, "Section « Navette Glose » écrite dans %s : l'agent du projet y trouve ses consignes.\n", to)
	return ExitOK
}
