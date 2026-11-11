// Package project reads the description of a reviewed project: its optional
// profile glose.json (D-08), the ordered list of its Markdown files, its
// rendering filter and the directories whose images may be served.
package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ptitan/glose/internal/agents"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ProfileName is the name of the profile at the root of a project.
const ProfileName = "glose.json"

// ErrNoMarkdown means that the project has no Markdown file to review.
var ErrNoMarkdown = errors.New("aucun fichier Markdown")

// StateDir is the directory of Glose's state inside a project (server,
// lock, later the journal).
const StateDir = ".glose"

// ProgressFile is the file of the state directory that keeps the progress of
// the review (SPEC F2 quinquies).
const ProgressFile = "progression.json"

// ProgressRead is the status of a section marked as read in that file.
const ProgressRead = "relue"

// Profile is the content of glose.json. Every field is optional.
type Profile struct {
	Tasks     []agents.Task `json:"tasks"`
	Title     string        `json:"title"`
	Files     []string      `json:"files"`
	Filter    string        `json:"filter"`
	CSS       string        `json:"css"`
	Resources []string      `json:"resources"`
	Pandoc    string        `json:"pandoc"`
	Exclude   []string      `json:"exclude"`
	// FilterPerFile declares that the filter treats each file on its own
	// (no numbering across files): the HTML is then made and kept file by
	// file (P6.5). Without a filter, it always is.
	FilterPerFile bool `json:"filterPerFile"`
	// Agent is what this text asks of the agent (D-48): its short name,
	// instructions of its own, and the tools it requires. Never a path nor
	// a key: the repository may be public.
	Agent *AgentProfile `json:"agent"`
	// Project is what the author says of the project (D-51): read leniently
	// by parseMeta, an invalid value never prevents opening the text.
	Project json.RawMessage `json:"project"`
}

// AgentProfile is the key « agent » of glose.json.
type AgentProfile struct {
	// Alias is a short name of the general configuration (glose set).
	Alias string `json:"alias"`
	// Instructions are given to the agent with the rules of the navette.
	Instructions string `json:"instructions"`
	// Requires are the tools the agent must declare: « mcp:NAME » or
	// « skill:NAME ».
	Requires []string `json:"requires"`
}

var (
	aliasRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	requireRe = regexp.MustCompile(`^(mcp|skill):[A-Za-z0-9._-]{1,64}$`)
)

// check validates the key « agent » of the profile.
func (a *AgentProfile) check() error {
	if a.Alias != "" && !aliasRe.MatchString(a.Alias) {
		return fmt.Errorf("agent.alias %q : 1 à 32 lettres sans accent, chiffres, « . », « _ » ou « - »", a.Alias)
	}
	if !utf8.ValidString(a.Instructions) || utf8.RuneCountInString(a.Instructions) > 4000 {
		return errors.New("agent.instructions : 4 000 caractères au plus")
	}
	for _, r := range a.Requires {
		if !requireRe.MatchString(r) {
			return fmt.Errorf("agent.requires %q : « mcp:NOM » ou « skill:NOM » attendu (NOM : lettres, chiffres, « . », « _ », « - »)", r)
		}
	}
	return nil
}

// Project is a reviewed project, with every path checked and resolved.
type Project struct {
	Tasks     []agents.Task
	Root      string       // absolute path of the project directory
	Source    string       // absolute path given to Load: the directory, or a single file
	Title     string       // title shown in the interface
	Files     []string     // Markdown files, relative to Root, slash-separated, in order
	Filter    string       // absolute path of the Lua filter, or ""
	CSS       string       // absolute path of the project stylesheet, or ""
	Resources []string     // absolute paths of the directories whose images are served
	Pandoc    string       // pandoc program ("pandoc" by default)
	PerFile   bool         // HTML made file by file: no filter, or filterPerFile
	Profiled  bool         // true if glose.json exists
	Agent     AgentProfile // key « agent » of the profile (P10.3)
	Meta      Meta         // key « project » of the profile, its valid values (D-51)
	// MetaWarnings says what was left out of the key « project ».
	MetaWarnings []string
	// Focus is the part of the project asked for when a path inside it was
	// given (« livre-1/ » or « livre-1/01.md »), or "" (D-35): the navette
	// covers the whole project; the interface and « glose list » start there.
	Focus string
}

// FindRoot looks for the root of a project from the directory start: the
// nearest directory, start included, that holds a profile glose.json
// (D-35). The search stops at a repository (a directory holding .git,
// checked first) and at the root of the file system. Only a profile marks a
// root: the .glose directory of a file reviewed alone does not.
func FindRoot(start string) (string, bool) {
	for d := start; ; {
		if fi, err := os.Stat(filepath.Join(d, ProfileName)); err == nil && !fi.IsDir() {
			return d, true
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return "", false
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// Load reads the project at path. Without a list of files, the project is
// made of the .md files of the directory and of its subdirectories, in tree
// order (Discover); the entries of the list may be files, directories or
// patterns (expand).
//
// A path inside a project (a subdirectory, or a file, below a directory
// that holds glose.json: FindRoot) opens that whole project, one server and
// one journal, with Focus naming the part asked for (D-35). A file that is
// not a file of that project is reviewed alone, in that project. Outside any
// project, a directory is its own project, and a file is reviewed alone in
// its directory (whose .glose keeps its navette).
func Load(path string) (*Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	dir := abs
	if !fi.IsDir() {
		dir = filepath.Dir(abs)
	}
	root, found := FindRoot(dir)
	if !found {
		if fi.IsDir() {
			return load(abs, "")
		}
		return load(dir, filepath.Base(abs))
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	if fi.IsDir() {
		p, err := load(root, "")
		if err != nil {
			return nil, err
		}
		if rel != "." {
			p.Focus = rel + "/"
		}
		return p, nil
	}
	whole, err := load(root, "")
	if err == nil {
		for _, f := range whole.Files {
			if f == rel {
				whole.Focus = rel
				return whole, nil
			}
		}
	}
	return load(root, rel)
}

// parseProfile reads the content of a profile; an unknown key is refused.
func parseProfile(raw []byte) (Profile, error) {
	var prof Profile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&prof)
	return prof, err
}

// CheckProfile tells whether raw can serve as a profile: its keys and those of
// its key « agent ». The paths are checked when a project is loaded.
func CheckProfile(raw []byte) error {
	prof, err := parseProfile(raw)
	if err != nil {
		return err
	}
	if err := agents.CheckTasks(prof.Tasks, true); err != nil {
		return err
	}
	if prof.Agent != nil {
		return prof.Agent.check()
	}
	return nil
}

// ReadAgent returns the key « agent » of the profile of the directory root,
// without loading the project: one without any text yet does not load.
func ReadAgent(root string) (AgentProfile, error) {
	raw, err := os.ReadFile(filepath.Join(root, ProfileName))
	if err != nil {
		return AgentProfile{}, err
	}
	prof, err := parseProfile(raw)
	if err != nil {
		return AgentProfile{}, err
	}
	if prof.Agent == nil {
		return AgentProfile{}, nil
	}
	return *prof.Agent, prof.Agent.check()
}

// load reads the project of the directory root; with single (a path
// relative to root), the project is that file alone.
func load(root, single string) (*Project, error) {
	p := &Project{Root: root, Source: root, Title: filepath.Base(root), Pandoc: "pandoc"}
	if single != "" {
		p.Source = filepath.Join(root, filepath.FromSlash(single))
	}
	var prof Profile
	raw, err := os.ReadFile(filepath.Join(root, ProfileName))
	switch {
	case err == nil:
		if prof, err = parseProfile(raw); err != nil {
			return nil, fmt.Errorf("%s illisible : %w", ProfileName, err)
		}
		p.Profiled = true
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if prof.Title != "" {
		p.Title = prof.Title
	}
	if prof.Agent != nil {
		if err := prof.Agent.check(); err != nil {
			return nil, fmt.Errorf("%s : %w", ProfileName, err)
		}
		p.Agent = *prof.Agent
	}
	if err := agents.CheckTasks(prof.Tasks, true); err != nil {
		return nil, err
	}
	p.Tasks = prof.Tasks
	p.Meta, p.MetaWarnings = parseMeta(prof.Project)
	if prof.Pandoc != "" {
		p.Pandoc = prof.Pandoc
	}
	if single != "" {
		prof.Files = []string{single}
		if prof.Title == "" {
			p.Title = path.Base(single)
		}
	}
	if len(prof.Files) == 0 {
		prof.Files = []string{"."}
	}
	files, err := p.expand(prof.Files, prof.Exclude)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w dans %s", ErrNoMarkdown, root)
	}
	p.Files = files
	p.PerFile = prof.Filter == "" || prof.FilterPerFile
	if prof.Filter != "" {
		abs, _, err := p.Inside(prof.Filter)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("filtre introuvable : %s", prof.Filter)
		}
		p.Filter = abs
	}
	if prof.CSS != "" {
		if !strings.EqualFold(filepath.Ext(prof.CSS), ".css") {
			return nil, fmt.Errorf("feuille de style %q : un fichier .css est attendu", prof.CSS)
		}
		abs, _, err := p.Inside(prof.CSS)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("feuille de style introuvable : %s", prof.CSS)
		}
		p.CSS = abs
	}
	if len(prof.Resources) == 0 {
		prof.Resources = []string{"."}
	}
	for _, r := range prof.Resources {
		abs, _, err := p.Inside(r)
		if err != nil {
			return nil, err
		}
		// A link to a directory of the project (Inside has checked that it
		// stays inside): its target is compared with the images requested,
		// whose links are resolved too.
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		p.Resources = append(p.Resources, abs)
	}
	return p, nil
}

// StateOf returns the state directory of the project at path (a directory or
// a file) without loading the project: « glose stop » must work even when the
// profile or the files have become unreadable. Inside a project (FindRoot),
// it is the state of that project.
func StateOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		// A file deleted: its directory is resolved all the same, so that
		// the state is named as the server names it (links, and the short
		// names of Windows, « RUNNER~1 » for « runneradmin »).
		abs = filepath.Join(real, filepath.Base(abs))
	}
	// A file, or a path that no longer exists (a file deleted while its
	// server runs): the state is in its directory.
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		abs = filepath.Dir(abs)
	}
	if root, ok := FindRoot(abs); ok {
		abs = root
	}
	return filepath.Join(abs, StateDir), nil
}

// Inside resolves a path given relative to the project and checks that it
// stays inside the project, symbolic links included. It returns the absolute
// path and the clean, slash-separated relative path.
func (p *Project) Inside(rel string) (abs, clean string, err error) {
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", "", fmt.Errorf("chemin %q : il doit être relatif au projet", rel)
	}
	abs = filepath.Join(p.Root, filepath.FromSlash(rel))
	if !within(p.Root, abs) {
		return "", "", fmt.Errorf("chemin %q hors du projet", rel)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil && !within(p.Root, real) {
		return "", "", fmt.Errorf("chemin %q : lien vers l'extérieur du projet", rel)
	}
	r, _ := filepath.Rel(p.Root, abs)
	return abs, filepath.ToSlash(r), nil
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}

// Abs returns the absolute path of a path relative to the project
// (slash-separated, as in the journal).
func (p *Project) Abs(rel string) string { return filepath.Join(p.Root, filepath.FromSlash(rel)) }

// Path returns the absolute path of the file of index i.
func (p *Project) Path(i int) string {
	return filepath.Join(p.Root, filepath.FromSlash(p.Files[i]))
}

// State returns the absolute path of the state directory.
func (p *Project) State() string { return filepath.Join(p.Root, StateDir) }

// imageExts are the only files served as resources.
var imageExts = map[string]string{
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
}

// Resource resolves a resource requested by the interface (a slash-separated
// path relative to the project). It must be an image inside one of the
// resource directories. It returns the absolute path and the content type.
func (p *Project) Resource(rel string) (string, string, error) {
	abs, _, err := p.Inside(rel)
	if err != nil {
		return "", "", err
	}
	ctype, ok := imageExts[strings.ToLower(filepath.Ext(abs))]
	if !ok {
		return "", "", fmt.Errorf("%q n'est pas une image", rel)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", err
	}
	for _, r := range p.Resources {
		if within(r, real) {
			return real, ctype, nil
		}
	}
	return "", "", fmt.Errorf("%q hors des dossiers de ressources", rel)
}
