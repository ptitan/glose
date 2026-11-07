package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// MaxFiles bounds the Markdown files found under a directory or by a
// pattern: beyond it, the profile must name its files (a home directory
// opened by mistake is not walked to the end).
const MaxFiles = 1000

// IsAgentFile reports whether name is the instructions file of an agent
// (AGENTS.md, CLAUDE.md, GEMINI.md, whatever the case). Such a file is never
// part of the text when it is found under a directory or by a pattern; it is
// taken only if the profile names it.
func IsAgentFile(name string) bool {
	switch strings.ToUpper(name) {
	case "AGENTS.MD", "CLAUDE.MD", "GEMINI.MD":
		return true
	}
	return false
}

// Discover returns the Markdown files that a project without a list of
// files is made of: every .md file of the directory and of its
// subdirectories, hidden ones and agent instructions excepted, in tree order
// (TreeLess). Paths are relative to root and slash-separated.
func Discover(root string) ([]string, error) {
	return walk(root, ".")
}

// TreeLess orders two slash-separated paths as a table of contents: in a
// directory, its files come first, in byte order, then its subdirectories,
// in byte order, each with its whole content. « introduction.md » thus comes
// before « chapitres/01.md », and « 01.05--a.md » before « 01.05.01--b.md ».
func TreeLess(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		if aFile, bFile := i == len(as)-1, i == len(bs)-1; aFile != bFile {
			return aFile
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}

// walk lists the Markdown files under the directory rel of root. Below rel,
// hidden entries (a name that begins with a dot: .git, .glose…) and agent
// instructions are skipped; links to directories are not followed.
func walk(root, rel string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(rel))
	var files []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == base {
				return err
			}
			return nil // an unreadable subdirectory: skipped
		}
		if p == base {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(name), ".md") || IsAgentFile(name) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(p); err != nil || fi.IsDir() {
				return nil
			}
		}
		r, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(r))
		if len(files) > MaxFiles {
			return errTooMany
		}
		return nil
	})
	if errors.Is(err, errTooMany) {
		where := rel
		if where == "." {
			where = "le dossier du projet"
		}
		return nil, fmt.Errorf("plus de %d fichiers Markdown sous %s : nommez les fichiers ou les dossiers du texte dans « files » de %s (voir « glose help profile »)", MaxFiles, where, ProfileName)
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return TreeLess(files[i], files[j]) })
	return files, nil
}

var errTooMany = errors.New("too many files")

// pattern is an entry of « files » or « exclude » that contains *, ? or [,
// split into its segments.
type pattern []string

func isPattern(s string) bool { return strings.ContainsAny(s, "*?[") }

// parsePattern checks a pattern of the profile: relative, slash-separated,
// without « .. », each segment valid for path.Match (« ** » stands for any
// number of directories).
func parsePattern(s string) (pattern, error) {
	if s == "" || strings.HasPrefix(s, "/") || filepath.VolumeName(s) != "" {
		return nil, fmt.Errorf("motif %q : il doit être relatif au projet", s)
	}
	var segs pattern
	for _, seg := range strings.Split(s, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			return nil, fmt.Errorf("motif %q hors du projet", s)
		}
		if _, err := path.Match(seg, ""); err != nil {
			return nil, fmt.Errorf("motif %q invalide", s)
		}
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("motif %q vide", s)
	}
	return segs, nil
}

// matchSegs matches the segments of a path against a pattern.
func matchSegs(pat pattern, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for k := 0; k <= len(name); k++ {
				if matchSegs(pat[1:], name[k:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// covers reports whether the pattern names the file or one of its
// directories: « livre-* » takes every file under livre-1/, livre-2/…
func (pat pattern) covers(file string) bool {
	segs := strings.Split(file, "/")
	for k := 1; k <= len(segs); k++ {
		if matchSegs(pat, segs[:k]) {
			return true
		}
	}
	return false
}

// expand turns the entries of « files » into the ordered list of files:
// a file is taken as named; a directory brings its Markdown files, and a
// pattern the Markdown files it covers, in tree order, minus those that
// « exclude » covers. A file already taken keeps its first place; a file
// named twice is an error.
func (p *Project) expand(entries, exclude []string) ([]string, error) {
	var excl []pattern
	for _, e := range exclude {
		pat, err := parsePattern(e)
		if err != nil {
			return nil, fmt.Errorf("exclude : %w", err)
		}
		excl = append(excl, pat)
	}
	excluded := func(f string) bool {
		for _, pat := range excl {
			if pat.covers(f) {
				return true
			}
		}
		return false
	}
	var out []string
	seen, named := map[string]bool{}, map[string]bool{}
	take := func(list []string) error {
		for _, f := range list {
			if seen[f] || excluded(f) {
				continue
			}
			if _, _, err := p.Inside(f); err != nil {
				return err
			}
			seen[f] = true
			out = append(out, f)
		}
		return nil
	}
	var all []string // the whole tree, walked once for the patterns
	for _, e := range entries {
		if isPattern(e) {
			pat, err := parsePattern(e)
			if err != nil {
				return nil, err
			}
			if all == nil {
				if all, err = walk(p.Root, "."); err != nil {
					return nil, err
				}
			}
			var got []string
			for _, f := range all {
				if pat.covers(f) {
					got = append(got, f)
				}
			}
			if err := take(got); err != nil {
				return nil, err
			}
			continue
		}
		abs, rel, err := p.Inside(e)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("fichier du profil introuvable : %s", e)
		}
		if fi.IsDir() {
			list, err := walk(p.Root, rel)
			if err != nil {
				return nil, err
			}
			if err := take(list); err != nil {
				return nil, err
			}
			continue
		}
		if named[rel] {
			return nil, fmt.Errorf("fichier cité deux fois dans le profil : %s", e)
		}
		named[rel] = true
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	return out, nil
}

// MaxInventory bounds the inventory of a handover.
const MaxInventory = 20000

// Inventory lists the Markdown files that exist under root, hidden
// directories included (a profile may name a file in one), except .git and
// the state directory, plus the files given (which may have another
// extension). It returns false beyond MaxInventory files or if the tree
// cannot be read: the inventory is then unknown (D-38).
func Inventory(root string, files []string) ([]string, bool) {
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, f := range files {
		add(f)
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == StateDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		r, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		add(filepath.ToSlash(r))
		if len(out) > MaxInventory {
			return errTooMany
		}
		return nil
	})
	if err != nil {
		return nil, false
	}
	sort.Strings(out)
	return out, true
}
