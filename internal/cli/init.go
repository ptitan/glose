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

	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

// NavetteSection is the section « Navette Glose » (navette.Section).
const NavetteSection = navette.Section

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
