// Package agents knows the agents in command line that Glose can work with
// (lot P10, D-46 to D-48): which are installed, which models they offer, and
// the general configuration of the author (« glose set »): short names
// (aliases) for a pair agent + model, and the default one. It holds no key
// and no secret: each agent keeps its own authentication (D-47).
//
// The configuration is general, the same for every project (D-48): it lives
// in the configuration directory of Glose, next to the display settings.
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ptitan/glose/internal/store"
)

// Kind is an agent that Glose knows.
type Kind struct {
	ID   string // claude, codex, opencode
	Name string // shown to the author
	Bin  string // program looked for in the PATH
	// Known are model names offered when the agent gives no list.
	Known []string
	// list asks the agent for its models; nil if it has no such command.
	list func(ctx context.Context, path string) ([]string, error)
}

// Kinds are the agents known to Glose, in the order of the interface. Their
// options were checked on claude 2.1, codex 0.166 and opencode 2.2
// (7 November 2026).
var Kinds = []Kind{
	{ID: "claude", Name: "Claude Code", Bin: "claude", Known: []string{"fable", "opus", "sonnet", "haiku"}},
	{ID: "codex", Name: "Codex", Bin: "codex", list: codexModels},
	{ID: "opencode", Name: "OpenCode", Bin: "opencode", list: opencodeModels},
}

// KindOf returns the agent of identifier id.
func KindOf(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Detected is what Glose found of one agent.
type Detected struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	// Windows: under WSL, a program of Windows (/mnt/c/…): shown, but not
	// launched from WSL (it would not see the files as Linux paths).
	Windows bool   `json:"windows,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Detection is the result of Detect, kept in the configuration directory.
type Detection struct {
	At     string     `json:"at"`
	Agents []Detected `json:"agents"`
}

// runTimeout bounds the commands run to know an agent (version, models).
var runTimeout = 10 * time.Second

// output runs path with args, without a shell, and returns its standard
// output. The standard input is empty: an agent never waits for an answer.
func output(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("pas de réponse en %s", runTimeout)
		}
		msg := strings.TrimSpace(errOut.String())
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg != "" {
			return nil, fmt.Errorf("%v : %s", err, msg)
		}
		return nil, err
	}
	return out.Bytes(), nil
}

// onWindows reports whether path is a program of Windows seen from WSL.
func onWindows(path string) bool {
	return runtime.GOOS == "linux" && regexp.MustCompile(`^/mnt/[a-z]/`).MatchString(path)
}

// Detect looks for each known agent in the PATH and asks for its version,
// all at once.
func Detect(ctx context.Context) Detection {
	out := make([]Detected, len(Kinds))
	var wg sync.WaitGroup
	for i, k := range Kinds {
		out[i] = Detected{ID: k.ID, Name: k.Name}
		path, err := exec.LookPath(k.Bin)
		if err != nil {
			continue
		}
		out[i].Found, out[i].Path, out[i].Windows = true, path, onWindows(path)
		if out[i].Windows {
			continue
		}
		wg.Add(1)
		go func(d *Detected) {
			defer wg.Done()
			b, err := output(ctx, d.Path, "--version")
			if err != nil {
				d.Error = "version illisible : " + err.Error()
				return
			}
			d.Version = firstLine(string(b))
		}(&out[i])
	}
	wg.Wait()
	return Detection{At: time.Now().UTC().Format(time.RFC3339), Agents: out}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

// Models returns the models of the agent id: the list given by the agent
// when it has one (fromAgent), else the names known to Glose. The author may
// always type another name.
func Models(ctx context.Context, id string) (models []string, fromAgent bool, err error) {
	k, ok := KindOf(id)
	if !ok {
		return nil, false, fmt.Errorf("agent inconnu : %q", id)
	}
	if k.list == nil {
		return append([]string(nil), k.Known...), false, nil
	}
	path, err := exec.LookPath(k.Bin)
	if err != nil {
		return nil, false, fmt.Errorf("%s introuvable dans le PATH", k.Bin)
	}
	if onWindows(path) {
		return nil, false, fmt.Errorf("%s est installé côté Windows : saisissez le modèle", k.Name)
	}
	m, err := k.list(ctx, path)
	if err != nil {
		return append([]string(nil), k.Known...), false, fmt.Errorf("liste des modèles de %s illisible : %v", k.Name, err)
	}
	return m, true, nil
}

// opencodeModels reads « opencode models »: one provider/model per line.
func opencodeModels(ctx context.Context, path string) ([]string, error) {
	b, err := output(ctx, path, "models")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && CheckModel(l) == nil {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("aucun modèle")
	}
	return out, nil
}

// codexModels reads « codex debug models », a JSON catalog: the models
// listed to the user (visibility « list »).
func codexModels(ctx context.Context, path string) ([]string, error) {
	b, err := output(ctx, path, "debug", "models")
	if err != nil {
		return nil, err
	}
	var cat struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &cat); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range cat.Models {
		if m.Slug != "" && (m.Visibility == "" || m.Visibility == "list") && CheckModel(m.Slug) == nil {
			out = append(out, m.Slug)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("aucun modèle")
	}
	return out, nil
}

// ---------- Configuration ----------

// Alias is a short name for a pair agent + model (« Sol » = codex +
// gpt-6.1-sol). An empty model is the agent's own default.
type Alias struct {
	Name  string `json:"name"`
	Agent string `json:"agent"`
	Model string `json:"model,omitempty"`
}

// Config is the general configuration of the agents (agents.json).
type Config struct {
	Tasks   []Task  `json:"tasks,omitempty"`
	Access  Access  `json:"access,omitempty"`
	Version int     `json:"version"`
	Default string  `json:"default"` // name of the default alias
	Aliases []Alias `json:"aliases"`
	// MaxMinutes bounds a turn of an agent launched by Glose (P10.5);
	// 0 is DefaultMaxMinutes.
	MaxMinutes int `json:"maxMinutes,omitempty"`
}

// DefaultMaxMinutes is the maximum duration of a launched agent's turn.
const DefaultMaxMinutes = 30

// Max returns the maximum duration of a launched agent's turn.
func (c Config) Max() time.Duration {
	if c.MaxMinutes <= 0 {
		return DefaultMaxMinutes * time.Minute
	}
	return time.Duration(c.MaxMinutes) * time.Minute
}

// Configured reports whether a default agent is recorded: until then, Glose
// says « Aucun agent configuré » and leads to « glose set ».
func (c Config) Configured() bool {
	_, ok := c.Alias(c.Default)
	return ok
}

// Alias returns the alias of name (case is ignored).
func (c Config) Alias(name string) (Alias, bool) {
	for _, a := range c.Aliases {
		if name != "" && strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return Alias{}, false
}

var aliasRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
var modelRe = regexp.MustCompile(`^[A-Za-z0-9._:/#@+-]{1,200}$`)

// ErrAlias is returned for a short name that is not 1 to 32 letters,
// digits, « . », « _ » or « - ».
var ErrAlias = errors.New("nom court invalide : 1 à 32 lettres sans accent, chiffres, « . », « _ » ou « - »")

// CheckAlias validates a short name. The same characters are allowed in the
// keys to= and via= of the files (D-49).
func CheckAlias(name string) error {
	if !aliasRe.MatchString(name) {
		return ErrAlias
	}
	return nil
}

// CheckModel validates the name of a model.
func CheckModel(m string) error {
	if !modelRe.MatchString(m) {
		return fmt.Errorf("nom de modèle invalide : %q", m)
	}
	return nil
}

// Check validates a configuration and returns it cleaned: version set, a
// default chosen if there is one alias at least.
func (c Config) Check() (Config, error) {
	c.Version = 1
	if err := CheckTasks(c.Tasks, false); err != nil {
		return c, err
	}
	if err := c.Access.Check(false); err != nil {
		return c, err
	}
	seen := map[string]bool{}
	for _, a := range c.Aliases {
		if err := CheckAlias(a.Name); err != nil {
			return c, fmt.Errorf("%q : %w", a.Name, err)
		}
		if seen[strings.ToLower(a.Name)] {
			return c, fmt.Errorf("nom court %q en double", a.Name)
		}
		seen[strings.ToLower(a.Name)] = true
		if _, ok := KindOf(a.Agent); !ok {
			return c, fmt.Errorf("%s : agent inconnu %q", a.Name, a.Agent)
		}
		if a.Model != "" {
			if err := CheckModel(a.Model); err != nil {
				return c, fmt.Errorf("%s : %w", a.Name, err)
			}
		}
	}
	if c.Aliases == nil {
		c.Aliases = []Alias{}
	}
	if c.MaxMinutes < 0 || c.MaxMinutes > 240 {
		return c, errors.New("durée maximale d'un tour : de 1 à 240 minutes")
	}
	if c.Default != "" && !seen[strings.ToLower(c.Default)] {
		return c, fmt.Errorf("agent par défaut inconnu : %q", c.Default)
	}
	if c.Default == "" && len(c.Aliases) > 0 {
		c.Default = c.Aliases[0].Name
	}
	if a, ok := c.Alias(c.Default); ok {
		c.Default = a.Name
	}
	return c, nil
}

// ConfigDir is the configuration directory of Glose: GLOSE_CONFIG_DIR, or
// glose in the configuration directory of the user.
func ConfigDir() (string, error) {
	if dir := os.Getenv("GLOSE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "glose"), nil
}

const (
	configFile    = "agents.json"
	detectionFile = "agents-detectes.json"
)

// Load reads the configuration; none yet is an empty configuration. An
// unreadable file is an error: it is never overwritten silently.
func Load() (Config, error) {
	c, _, err := LoadWithHash()
	return c, err
}

// LoadWithHash reads the configuration and the hash of its file ("" when
// there is none), for SaveIfUnchanged.
func LoadWithHash() (Config, string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return Config{}, "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return Config{Version: 1, Aliases: []Alias{}}, "", nil
	}
	if err != nil {
		return Config{}, "", err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, "", fmt.Errorf("%s illisible : %w", filepath.Join(dir, configFile), err)
	}
	c, err = c.Check()
	return c, store.Hash(b), err
}

// ErrConfigChanged means that the configuration was saved elsewhere (another
// page, another project) since it was read: nothing is overwritten.
var ErrConfigChanged = errors.New("les réglages des agents ont été modifiés ailleurs depuis leur lecture")

// anyHash makes Save write whatever the file holds.
const anyHash = "*"

// Save validates and writes the configuration, whatever the file holds
// (tests, first configuration).
func Save(c Config) (Config, error) { return SaveIfUnchanged(c, anyHash) }

// SaveIfUnchanged validates and writes the configuration if its file still
// has the hash expected ("" : no file), under a lock shared by every
// process of Glose (invariant 3: no overwriting).
func SaveIfUnchanged(c Config, expected string) (Config, error) {
	c, err := c.Check()
	if err != nil {
		return c, err
	}
	dir, err := ConfigDir()
	if err != nil {
		return c, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := store.WaitLockNamed(ctx, dir, "agents.lock")
	if err != nil {
		return c, err
	}
	defer lock.Unlock()
	path := filepath.Join(dir, configFile)
	if expected != anyHash {
		cur := ""
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			cur = store.Hash(b)
		case !errors.Is(err, os.ErrNotExist):
			return c, err
		}
		if cur != expected {
			return c, ErrConfigChanged
		}
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := store.WriteAtomic(path, data, 0o600); err != nil && !errors.Is(err, store.ErrNotSynced) {
		return c, err
	}
	return c, nil
}

// LoadDetection returns the last detection kept, or false.
func LoadDetection() (Detection, bool) {
	dir, err := ConfigDir()
	if err != nil {
		return Detection{}, false
	}
	b, err := os.ReadFile(filepath.Join(dir, detectionFile))
	if err != nil {
		return Detection{}, false
	}
	var d Detection
	if json.Unmarshal(b, &d) != nil || len(d.Agents) == 0 {
		return Detection{}, false
	}
	return d, true
}

// SaveDetection keeps a detection; a failure only costs a new detection.
func SaveDetection(d Detection) {
	dir, err := ConfigDir()
	if err != nil {
		return
	}
	data, _ := json.MarshalIndent(d, "", "  ")
	store.WriteAtomic(filepath.Join(dir, detectionFile), data, 0o600)
}

// CachedDetection returns the detection kept, or detects now and keeps it.
func CachedDetection(ctx context.Context) Detection {
	if d, ok := LoadDetection(); ok {
		return d
	}
	d := Detect(ctx)
	SaveDetection(d)
	return d
}

// Names returns the short names of c, sorted.
func (c Config) Names() []string {
	var out []string
	for _, a := range c.Aliases {
		out = append(out, a.Name)
	}
	sort.Strings(out)
	return out
}
