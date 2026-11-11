// Package trust keeps what the author has approved to run when a project is
// rendered (D-64): the pandoc program named by its profile, and its Lua
// filter, by content. The server of Glose runs nothing else: a profile or a
// filter changed by an agent waits for the author's approval.
//
// The approvals live in the configuration directory of Glose, outside every
// project, where the agents launched by Glose do not write.
package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/store"
)

// DefaultPandoc is the program used without a key « pandoc » in the profile:
// found in the PATH, it needs no approval.
const DefaultPandoc = "pandoc"

const fileName = "approbations.json"

// Need is what a project runs when it is rendered, beyond pandoc found in the
// PATH: its program, its filter (relative to the project) and the hash of
// the filter's content.
type Need struct {
	Pandoc     string `json:"pandoc,omitempty"`
	Filter     string `json:"filter,omitempty"`
	FilterHash string `json:"filterHash,omitempty"`
}

// Entry is the approval of one project.
type Entry struct {
	Need
	At string `json:"at"`
}

type file struct {
	Version  int              `json:"version"`
	Projects map[string]Entry `json:"projects"`
}

// Of returns what the project at root runs: the program of its profile
// (pandoc is "" or DefaultPandoc for none) and its filter (absolute path, or
// "") with the hash of its content as it is now. A filter that cannot be read
// has an empty hash: never approved.
func Of(root, pandoc, filter string) Need {
	n := Need{}
	if pandoc != "" && pandoc != DefaultPandoc {
		n.Pandoc = pandoc
	}
	if filter != "" {
		rel, err := filepath.Rel(root, filter)
		if err != nil {
			rel = filter
		}
		n.Filter = filepath.ToSlash(rel)
		if b, err := os.ReadFile(filter); err == nil {
			n.FilterHash = store.Hash(b)
		}
	}
	return n
}

// Empty reports whether nothing needs an approval.
func (n Need) Empty() bool { return n.Pandoc == "" && n.Filter == "" }

// InsideProject reports whether the program of the profile is a file of the
// project itself: an agent could replace it, it is never approved.
func InsideProject(root, pandoc string) bool {
	if pandoc == "" || pandoc == DefaultPandoc || !strings.ContainsAny(pandoc, `/\`) {
		return false
	}
	p := pandoc
	// Relative to the project only without a root: « /usr/bin/pandoc » is
	// not absolute for Windows (no volume), but it is rooted, not relative.
	if !filepath.IsAbs(p) && filepath.VolumeName(p) == "" && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, `\`) {
		p = filepath.Join(root, p)
	}
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	// The path as written, and as resolved (links, short names of Windows),
	// against the root as given and as resolved: inside if any says so.
	paths := []string{p}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		paths = append(paths, real)
	}
	for _, r := range []string{root, realRoot} {
		for _, x := range paths {
			if within(r, x) {
				return true
			}
		}
	}
	return false
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Check tells, for the project at root, whether its program and its filter
// are approved. A missing or unreadable file of approvals approves nothing.
func Check(root string, n Need) (pandocOK, filterOK bool) {
	pandocOK = n.Pandoc == ""
	filterOK = n.Filter == ""
	if pandocOK && filterOK {
		return
	}
	f, err := read()
	if err != nil {
		return
	}
	e, ok := f.Projects[key(root)]
	if !ok {
		return
	}
	if !pandocOK {
		pandocOK = e.Pandoc == n.Pandoc && !InsideProject(root, n.Pandoc)
	}
	if !filterOK {
		filterOK = n.FilterHash != "" && e.Filter == n.Filter && e.FilterHash == n.FilterHash
	}
	return
}

// Known reports whether something was ever approved for the project at root.
func Known(root string) bool {
	f, err := read()
	if err != nil {
		return true // unreadable: never taken for a first opening
	}
	_, ok := f.Projects[key(root)]
	return ok
}

// ErrInside refuses a program of the project itself.
var ErrInside = errors.New("le programme pandoc du profil est dans le projet : un agent pourrait le remplacer ; placez-le hors du projet")

// Approve records n as approved for the project at root, replacing the
// previous approval of that project. It returns true if something new was
// approved.
func Approve(root string, n Need) (bool, error) {
	if InsideProject(root, n.Pandoc) {
		return false, ErrInside
	}
	if n.Filter != "" && n.FilterHash == "" {
		return false, fmt.Errorf("filtre %s illisible : rien n'est approuvé", n.Filter)
	}
	if pOK, fOK := Check(root, n); pOK && fOK {
		return false, nil
	}
	dir, err := agents.ConfigDir()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := store.WaitLockNamed(ctx, dir, "approbations.lock")
	if err != nil {
		return false, err
	}
	defer lock.Unlock()
	f, err := read()
	if err != nil {
		return false, err
	}
	f.Projects[key(root)] = Entry{Need: n, At: time.Now().UTC().Format(time.RFC3339)}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return false, err
	}
	if err := store.WriteAtomic(filepath.Join(dir, fileName), append(b, '\n'), 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return false, err
	}
	return true, nil
}

// Describe says what n runs, for the author.
func (n Need) Describe() string {
	var parts []string
	if n.Filter != "" {
		parts = append(parts, "le filtre Lua "+n.Filter)
	}
	if n.Pandoc != "" {
		parts = append(parts, "le programme "+n.Pandoc)
	}
	return strings.Join(parts, " et ")
}

func key(root string) string {
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root
}

// read reads the approvals; none yet is an empty set. An unreadable file is
// an error, and is never overwritten.
func read() (file, error) {
	f := file{Version: 1, Projects: map[string]Entry{}}
	dir, err := agents.ConfigDir()
	if err != nil {
		return f, err
	}
	b, err := os.ReadFile(filepath.Join(dir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("%s illisible : %w", filepath.Join(dir, fileName), err)
	}
	if f.Projects == nil {
		f.Projects = map[string]Entry{}
	}
	return f, nil
}
