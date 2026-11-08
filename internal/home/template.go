package home

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

// Templates of a kind of project (D-52): they live in the configuration of the
// author, <ConfigDir>/gabarits/NAME/, never in the executable. « glose init
// --type NAME » copies one, without ever overwriting anything.

const templatesDir = "gabarits"

var templateNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// TemplatesPath returns the directory of the templates.
func TemplatesPath() (string, error) {
	dir, err := agents.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, templatesDir), nil
}

// Templates returns the names of the existing templates, sorted.
func Templates() ([]string, error) {
	dir, err := TemplatesPath()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() && templateNameRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// ErrNoTemplate means that no template has the name asked for.
type ErrNoTemplate struct {
	Name     string
	Dir      string
	Existing []string
}

func (e *ErrNoTemplate) Error() string {
	if len(e.Existing) == 0 {
		return fmt.Sprintf("aucun gabarit %q : %s n'en contient aucun. Un gabarit est un dossier de ce répertoire, recopié dans le projet", e.Name, e.Dir)
	}
	return fmt.Sprintf("aucun gabarit %q ; gabarits de %s : %s", e.Name, e.Dir, strings.Join(e.Existing, ", "))
}

// Skipped is an entry of a template that was not copied, and why.
type Skipped struct {
	Path   string
	Reason string
}

// Copied is what a template did in a project.
type Copied struct {
	Created []string // files and directories, relative and slash-separated
	Skipped []Skipped
	// Profile: the template provided the profile of the project.
	Profile bool
}

// reservedName: such an entry cannot be in a template: copied, they would forge the state of
// Glose, or a repository (and its hooks), in the project.
func reservedName(name string) bool {
	return strings.EqualFold(name, project.StateDir) || strings.EqualFold(name, ".git")
}

// checkTemplate refuses a template as a whole before anything is written: a
// symbolic link or a special file, a reserved name, a profile below its root,
// or a profile that cannot serve.
func checkTemplate(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		show := filepath.ToSlash(rel)
		if reservedName(d.Name()) {
			return fmt.Errorf("gabarit refusé : %s ne peut pas être recopié dans un projet", show)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("gabarit refusé : %s est un lien symbolique", show)
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return fmt.Errorf("gabarit refusé : %s n'est ni un fichier ni un dossier", show)
		}
		if d.Name() == project.ProfileName && !d.IsDir() {
			if filepath.Dir(rel) != "." {
				return fmt.Errorf("gabarit refusé : %s ferait un projet dans le projet ; le profil va à la racine du gabarit", show)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := project.CheckProfile(b); err != nil {
				return fmt.Errorf("gabarit refusé : son %s est illisible : %w", project.ProfileName, err)
			}
		}
		return nil
	})
}

// ApplyTemplate copies the template name in the directory root: what exists
// there is never overwritten, and is said. A template that cannot serve is
// refused before anything is written (the link of the template itself
// included). Run again, it creates nothing.
func ApplyTemplate(name, root string) (Copied, error) {
	var out Copied
	base, err := TemplatesPath()
	if err != nil {
		return out, err
	}
	dir := filepath.Join(base, name)
	fi, lerr := os.Lstat(dir)
	if lerr == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return out, fmt.Errorf("gabarit refusé : %s est un lien symbolique", dir)
	}
	if !templateNameRe.MatchString(name) || lerr != nil || !fi.IsDir() {
		existing, err := Templates()
		if err != nil {
			return out, err
		}
		return out, &ErrNoTemplate{Name: name, Dir: base, Existing: existing}
	}
	if err := checkTemplate(dir); err != nil {
		return out, err
	}
	err = copyDir(dir, root, "", &out)
	return out, err
}

// copyDir copies the entries of the directory src into dst, which exists and
// is not a link; prefix is the path of dst in the project.
func copyDir(src, dst, prefix string, out *Copied) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		rel := prefix + e.Name()
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		info, err := e.Info()
		if err != nil {
			return err
		}
		have, statErr := os.Lstat(to)
		exists := statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if e.IsDir() {
			switch {
			case exists && have.Mode()&fs.ModeSymlink != 0:
				out.Skipped = append(out.Skipped, Skipped{rel + "/", "lien symbolique dans le projet : rien n'est écrit à travers lui"})
				continue
			case exists && !have.IsDir():
				out.Skipped = append(out.Skipped, Skipped{rel + "/", "un fichier porte ce nom"})
				continue
			case !exists:
				if err := store.EnsureDir(to, info.Mode().Perm()|0o700); err != nil && !errors.Is(err, store.ErrNotSynced) {
					return err
				}
				out.Created = append(out.Created, rel+"/")
			}
			if err := copyDir(from, to, rel+"/", out); err != nil {
				return err
			}
			continue
		}
		if exists {
			out.Skipped = append(out.Skipped, Skipped{rel, "existe déjà : gardé tel quel"})
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		err = store.CreateAtomic(to, data, info.Mode().Perm())
		switch {
		case errors.Is(err, store.ErrConflict):
			out.Skipped = append(out.Skipped, Skipped{rel, "existe déjà : gardé tel quel"})
			continue
		case err != nil && !errors.Is(err, store.ErrNotSynced):
			return err
		}
		out.Created = append(out.Created, rel)
		if rel == project.ProfileName {
			out.Profile = true
		}
	}
	return nil
}
