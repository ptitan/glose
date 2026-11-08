package home

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

// Markers around the section in the instructions file, so that « glose
// init » can replace it with a newer version.
const (
	SectionStart = "<!-- glose:navette (écrit par glose init) -->"
	SectionEnd   = "<!-- /glose:navette -->"
)

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

// registerOrSay records a project in the registry (D-50). A failure is said,
// and never stops anything: the registry is not normative.
func registerOrSay(root string, stderr io.Writer) {
	if err := Register(root); err != nil {
		fmt.Fprintf(stderr, "glose : projet non inscrit à l'accueil (%v)\n", err)
	}
}

// Init prepares the project of the directory path, for « glose init » and for
// « Nouveau projet » of the overview (P11.7, P11.9): the template kind if there
// is one, without overwriting anything; the profile if there is none; the
// section « Navette Glose » in the instructions file to (by default AGENTS.md,
// or CLAUDE.md if it is alone). What it does is said on stdout and stderr.
func Init(path, to, kind string, stdout, stderr io.Writer) error {
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s n'est pas un dossier", path)
	}
	// Inside a project already (a profile above): a profile here would make
	// a separate project, with its own navette (D-35).
	if real, err := filepath.EvalSymlinks(root); err == nil {
		if above, ok := project.FindRoot(filepath.Dir(real)); ok && above != real {
			if _, err := os.Stat(filepath.Join(real, project.ProfileName)); errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s est dans le projet Glose de %s : lancez « glose init » à sa racine ; ce dossier s'ouvre déjà avec lui (glose open %s)", path, above, path)
			}
		}
	}
	// The template, copied first: its profile and its instructions count for
	// what follows (D-52). Nothing exists yet that a refusal would leave half
	// done: a template that cannot serve is refused before any write.
	templated := false
	if kind != "" {
		copied, err := ApplyTemplate(kind, root)
		if err != nil {
			return err
		}
		templated = copied.Profile
		fmt.Fprintf(stdout, "Gabarit « %s » : %d élément%s créé%s", kind, len(copied.Created), plural(len(copied.Created)), plural(len(copied.Created)))
		if len(copied.Skipped) > 0 {
			fmt.Fprintf(stdout, ", %d sauté%s (rien n'est écrasé) :\n", len(copied.Skipped), plural(len(copied.Skipped)))
			for _, sk := range copied.Skipped {
				fmt.Fprintf(stdout, "  %s : %s\n", sk.Path, sk.Reason)
			}
		} else {
			fmt.Fprintln(stdout, ".")
		}
	}
	// The profile, if there is none.
	prof := filepath.Join(root, project.ProfileName)
	createdProfile := false
	if templated {
		createdProfile = true
		fmt.Fprintf(stdout, "Profil repris du gabarit : %s.\n", project.ProfileName)
	} else if _, err := statForInit(prof, kind); errors.Is(err, os.ErrNotExist) {
		createdProfile = true
		// What Glose would take without a list (project.Discover): the files
		// of the root by name, each subdirectory as a whole, so that a new
		// chapter is taken without changing the profile.
		found, err := project.Discover(root)
		if err != nil {
			return err
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
			return err
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
	if kind != "" && createdProfile {
		setDefaultType(root, kind, stderr)
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
	// With a template, nothing is written through a link of the project (F31),
	// nor outside it.
	if kind != "" {
		if why := unsafeTarget(root, to); why != "" {
			fmt.Fprintf(stdout, "Section « Navette Glose » non écrite : %s %s ; glose init sans --type écrit à son emplacement.\n", to, why)
			registerOrSay(root, stderr)
			warnMissingTools(root, stderr)
			return nil
		}
	}
	old, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	block := SectionStart + "\n" + navette.Section + SectionEnd + "\n"
	var out []byte
	if i := bytes.Index(old, []byte(SectionStart)); i >= 0 {
		j := bytes.Index(old[i:], []byte(SectionEnd))
		if j < 0 {
			return fmt.Errorf("%s : section Glose sans sa fin (%s) ; corrigez le fichier", to, SectionEnd)
		}
		end := i + j + len(SectionEnd)
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
	// The profile exists by now: the project enters the registry whatever
	// becomes of the section (D-50).
	registerOrSay(root, stderr)
	if kind != "" {
		defer warnMissingTools(root, stderr)
	}
	if bytes.Equal(out, old) {
		fmt.Fprintf(stdout, "Section « Navette Glose » de %s : à jour.\n", to)
		return nil
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(target); err == nil {
		perm = fi.Mode().Perm()
	}
	if len(old) > 0 {
		if _, err := store.WriteIfUnchanged(target, store.Hash(old), out); err != nil && !errors.Is(err, store.ErrNotSynced) {
			return err
		}
	} else if err := store.WriteAtomic(target, out, perm); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return err
	}
	fmt.Fprintf(stdout, "Section « Navette Glose » écrite dans %s : l'agent du projet y trouve ses consignes.\n", to)
	return nil
}

// unsafeTarget says why the file rel, given relative to root, should not be
// written with a template: it leaves the project, or a component of its path
// is a symbolic link. "" if it is safe.
func unsafeTarget(root, rel string) string {
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "est hors du projet"
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "est hors du projet"
	}
	cur := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "passe par un lien symbolique, laissé tel quel"
		}
	}
	return ""
}

// statForInit tells whether the profile exists. With a template (kind), a link
// counts even without a target: a link that the copy said it kept is not then
// replaced by a new profile.
func statForInit(path, kind string) (os.FileInfo, error) {
	if kind != "" {
		return os.Lstat(path)
	}
	return os.Stat(path)
}

// setDefaultType writes the name of the template as the type of a profile just
// created, unless the template says otherwise. A failure costs only the type.
func setDefaultType(root, kind string, stderr io.Writer) {
	raw, err := os.ReadFile(filepath.Join(root, project.ProfileName))
	if err != nil {
		return
	}
	var prof struct {
		Project struct {
			Type string `json:"type"`
		} `json:"project"`
	}
	if json.Unmarshal(raw, &prof) == nil && prof.Project.Type != "" {
		return
	}
	if err := project.CheckMeta(project.MetaType, kind); err != nil {
		return
	}
	if _, _, err := WriteMeta(root, "", []MetaChange{{Key: project.MetaType, Value: kind}}); err != nil {
		fmt.Fprintf(stderr, "glose : type du projet non écrit (%v)\n", err)
	}
}

// toolsTimeout bounds the check of the tools required by a template.
const toolsTimeout = 90 * time.Second

// warnMissingTools says which tools required by the profile the agent of the
// text does not declare. It only warns: the agent keeps its own configuration.
func warnMissingTools(root string, stderr io.Writer) {
	// Read in the profile: a project without any text yet does not load.
	wanted, err := project.ReadAgent(root)
	if err != nil || len(wanted.Requires) == 0 {
		return
	}
	cfg, err := agents.Load()
	if err != nil {
		fmt.Fprintf(stderr, "glose : outils requis non vérifiés (%v)\n", err)
		return
	}
	alias, ok := cfg.Alias(wanted.Alias)
	if !ok {
		alias, ok = cfg.Alias(cfg.Default)
	}
	if !ok {
		fmt.Fprintf(stderr, "glose : outils requis non vérifiés (%s) : aucun agent configuré (glose set).\n", strings.Join(wanted.Requires, ", "))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolsTimeout)
	defer cancel()
	checks, err := agents.CheckTools(ctx, alias.Agent, root, wanted.Requires)
	if err != nil {
		fmt.Fprintf(stderr, "glose : outils requis non vérifiés (%v)\n", err)
		return
	}
	for _, c := range checks {
		if !c.Found {
			fmt.Fprintf(stderr, "glose : outil requis absent chez %s : %s (%s)\n", alias.Name, c.Require, c.Detail)
		}
	}
}
