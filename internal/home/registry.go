// Package home is the overview of the projects of an author (lot P11, D-50):
// a registry of their roots, kept next to the settings of the agents, and a
// summary of each project, read again in the project at every display
// (invariant 4). The registry is never normative: losing it loses nothing.
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
	"sort"
	"strings"
	"time"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

const (
	registryFile = "projets.json"
	registryLock = "projets.lock"
	// lockWait bounds the wait for the registry lock: a holder only reads
	// and writes one small file, and the system frees the lock if it dies.
	lockWait = 10 * time.Second
)

// Entry is one project of the registry: its resolved root and the date of
// its last opening, nothing else (D-50).
type Entry struct {
	Root   string `json:"root"`
	Opened string `json:"opened"`
}

// ID is the identifier of the entry: the page of the overview sends it, never
// a path (D-50). It is derived from the root, so that nothing more is stored.
func (e Entry) ID() string { return ID(e.Root) }

// ID returns the identifier of a root in the registry.
func ID(root string) string { return store.Hash([]byte(root))[:16] }

type registry struct {
	Version  int     `json:"version"`
	Projects []Entry `json:"projects"`
}

// ErrNotRegistered means that a project asked for is not in the registry.
var ErrNotRegistered = errors.New("ce projet n'est pas inscrit dans le registre")

// RegistryPath returns the path of the registry.
func RegistryPath() (string, error) {
	dir, err := agents.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, registryFile), nil
}

// parseRegistry reads the content of the registry strictly: whatever it does
// not understand makes it unreadable, and an unreadable registry is never
// overwritten (it may come from a newer version, or be edited by hand).
func parseRegistry(b []byte) (registry, error) {
	r := registry{Version: 1}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return r, errors.New("contenu après la fin du document")
	}
	if r.Version != 1 {
		return r, fmt.Errorf("version %d inconnue", r.Version)
	}
	seen := map[string]bool{}
	for _, e := range r.Projects {
		if !filepath.IsAbs(e.Root) {
			return r, fmt.Errorf("racine %q : un chemin absolu est attendu", e.Root)
		}
		if seen[e.Root] {
			return r, fmt.Errorf("racine %q en double", e.Root)
		}
		seen[e.Root] = true
		if _, err := time.Parse(time.RFC3339Nano, e.Opened); err != nil {
			return r, fmt.Errorf("date %q de %q illisible", e.Opened, e.Root)
		}
	}
	return r, nil
}

func read(path string) (registry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return registry{Version: 1}, nil
	}
	if err != nil {
		return registry{}, err
	}
	r, err := parseRegistry(b)
	if err != nil {
		return registry{}, fmt.Errorf("%s illisible (%v) : corrigez-le ou supprimez-le ; il n'est pas écrasé, et le perdre ne perd rien", path, err)
	}
	return r, nil
}

// Load returns the entries of the registry, the last opened first. An
// unreadable registry is an error.
func Load() ([]Entry, error) {
	path, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	r, err := read(path)
	if err != nil {
		return nil, err
	}
	out := r.Projects
	if out == nil {
		out = []Entry{}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, out[i].Opened)
		b, _ := time.Parse(time.RFC3339Nano, out[j].Opened)
		return a.After(b)
	})
	return out, nil
}

// update reads the registry, changes it and writes it, under a lock shared by
// every process of Glose: simultaneous openings lose no entry.
func update(change func(*registry) error) error {
	path, err := RegistryPath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	lock, err := store.WaitLockNamed(ctx, filepath.Dir(path), registryLock)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	r, err := read(path)
	if err != nil {
		return err
	}
	if err := change(&r); err != nil {
		return err
	}
	if r.Projects == nil {
		r.Projects = []Entry{}
	}
	data, _ := json.MarshalIndent(r, "", "  ")
	data = append(data, '\n')
	if err := store.WriteAtomic(path, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return err
	}
	return nil
}

// underTemp reports whether path is the temporary directory of the system or
// lies inside it. Copies made for a trial do not belong in the registry.
func underTemp(path string) bool {
	tmp := os.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	return inside(tmp, path)
}

// inside reports whether path is root or lies inside it.
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Register records the project of the directory root as opened now. Only a
// directory that holds a profile is a project of the registry (a file
// reviewed alone is not), and what lives under the temporary directory of the
// system stays out (D-50). The caller never lets an error stop the opening of
// a text.
func Register(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if fi, err := os.Stat(filepath.Join(abs, project.ProfileName)); err != nil || fi.IsDir() {
		return nil
	}
	if underTemp(abs) {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return update(func(r *registry) error {
		for i := range r.Projects {
			if r.Projects[i].Root == abs {
				r.Projects[i].Opened = now
				return nil
			}
		}
		r.Projects = append(r.Projects, Entry{Root: abs, Opened: now})
		return nil
	})
}

// Forget removes a project from the registry, named by its identifier, its
// root, or a path inside it; the directory may have disappeared. It returns
// the entry removed. Nothing is touched in the project.
func Forget(target string) (Entry, error) {
	var removed Entry
	err := update(func(r *registry) error {
		i := find(r.Projects, target)
		if i < 0 {
			return fmt.Errorf("%w : %s", ErrNotRegistered, target)
		}
		removed = r.Projects[i]
		r.Projects = append(r.Projects[:i:i], r.Projects[i+1:]...)
		return nil
	})
	return removed, err
}

// find returns the index of the entry named by target: its identifier, its
// root, or a path inside it. Among the entries that contain the path, the
// deepest; and none if the path lies in another project, not registered, below
// that entry: a parent is never removed in place of the project asked for.
func find(entries []Entry, target string) int {
	for i, e := range entries {
		if e.ID() == target {
			return i
		}
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return -1
	}
	path := abs
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		path = real
	}
	best := -1
	for i, e := range entries {
		if e.Root == abs || e.Root == path {
			return i
		}
		if inside(e.Root, path) && (best < 0 || len(e.Root) > len(entries[best].Root)) {
			best = i
		}
	}
	if best < 0 {
		return -1
	}
	dir := path
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		dir = filepath.Dir(path)
	}
	if root, ok := project.FindRoot(dir); ok && root != entries[best].Root && inside(entries[best].Root, root) {
		return -1
	}
	return best
}
