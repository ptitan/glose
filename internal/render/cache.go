package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"github.com/ptitan/glose/internal/projection"
	"github.com/ptitan/glose/internal/sourcemap"
	"github.com/ptitan/glose/internal/store"
)

// The disk cache (P6.4) keeps, in .glose/cache/, the first stage of each
// file (rewritten tree and source map) and its HTML, so that a project
// reopened unchanged is not read by pandoc again. It is never normative
// (invariant 4): an entry lost, damaged or from another version is ignored
// and computed again. A key covers everything a result depends on: the
// content of the file, its rank, the view, the pandoc program and its
// version, the executable of Glose itself, and the filter for the HTML.

// CacheDir is the directory of the cache inside the state directory.
const CacheDir = "cache"

// cacheMax bounds the size of the cache; the oldest entries go first.
var cacheMax int64 = 512 << 20

var (
	selfOnce sync.Once
	selfHash string
)

// executableHash identifies the build of Glose: any rebuild invalidates the
// cache, whatever its version string.
func executableHash() string {
	selfOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.Open(exe)
		if err != nil {
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err == nil {
			selfHash = hex.EncodeToString(h.Sum(nil))
		}
	})
	return selfHash
}

// diskCache is the cache of one renderer; nil when disabled.
type diskCache struct {
	dir     string
	base    string // executable, pandoc and its version
	mu      sync.Mutex
	written int
}

// EnableDiskCache turns the disk cache on for r (the server). Without it, a
// renderer keeps its results in memory only and writes nothing.
func (r *Renderer) EnableDiskCache(ctx context.Context) {
	exe := executableHash()
	if exe == "" {
		return
	}
	version, err := CheckPandoc(ctx, r.P.Pandoc)
	if err != nil {
		return
	}
	r.disk = &diskCache{dir: filepath.Join(r.P.State(), CacheDir), base: exe + "\x00" + r.P.Pandoc + "\x00" + version}
}

func (c *diskCache) key(parts ...string) string {
	h := sha256.New()
	h.Write([]byte(c.base))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type stage1Entry struct {
	Key    string           `json:"key"`
	Sum    string           `json:"sum"`
	API    json.RawMessage  `json:"api"`
	Blocks json.RawMessage  `json:"blocks"`
	Items  []sourcemap.Item `json:"items"`
	Parts  []partEntry      `json:"parts,omitempty"`
}

// partEntry is a section of a big file (P6.12).
type partEntry struct {
	Rel  json.RawMessage `json:"rel"`
	Base int             `json:"base"`
}

type htmlEntry struct {
	Key  string `json:"key"`
	Sum  string `json:"sum"`
	HTML []byte `json:"html"`
}

func stage1Sum(e *stage1Entry) string {
	items, _ := json.Marshal(e.Items)
	parts, _ := json.Marshal(e.Parts)
	return store.Hash(bytes.Join([][]byte{e.API, e.Blocks, items, parts}, []byte{0}))
}

func (c *diskCache) path(key string) string { return filepath.Join(c.dir, key[:2], key+".json") }

func (c *diskCache) stage1Key(path string, index int, v projection.View, hash string) string {
	return c.key("stage1", path, strconv.Itoa(index), strconv.Itoa(int(v)), hash)
}

func (c *diskCache) htmlKey(path string, index int, v projection.View, hash, filter string) string {
	return c.key("html", path, strconv.Itoa(index), strconv.Itoa(int(v)), hash, filter)
}

// loadStage1 returns the first stage kept for this file, or nil.
func (c *diskCache) loadStage1(key string) *fileResult {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil
	}
	var e stage1Entry
	if json.Unmarshal(b, &e) != nil || e.Key != key || e.Sum != stage1Sum(&e) {
		return nil
	}
	var api any
	if json.Unmarshal(e.API, &api) != nil {
		return nil
	}
	res := &fileResult{api: api, blocks: e.Blocks, m: &sourcemap.Map{Items: e.Items}}
	for _, p := range e.Parts {
		res.parts = append(res.parts, chunkPart{rel: p.Rel, base: p.Base})
	}
	return res
}

func (c *diskCache) saveStage1(key string, res *fileResult) {
	api, err := json.Marshal(res.api)
	if err != nil {
		return
	}
	e := stage1Entry{Key: key, API: api, Blocks: res.blocks, Items: res.m.Items}
	for _, p := range res.parts {
		e.Parts = append(e.Parts, partEntry{Rel: p.rel, Base: p.base})
	}
	e.Sum = stage1Sum(&e)
	c.save(key, e)
}

func (c *diskCache) loadHTML(key string) []byte {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil
	}
	var e htmlEntry
	if json.Unmarshal(b, &e) != nil || e.Key != key || e.Sum != store.Hash(e.HTML) {
		return nil
	}
	return e.HTML
}

func (c *diskCache) saveHTML(key string, html []byte) {
	c.save(key, htmlEntry{Key: key, Sum: store.Hash(html), HTML: html})
}

func (c *diskCache) save(key string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	if store.WriteCache(c.path(key), b) != nil {
		return
	}
	c.mu.Lock()
	c.written++
	trim := c.written%20 == 0
	c.mu.Unlock()
	if trim {
		c.trim()
	}
}

// trim removes the oldest entries beyond cacheMax.
func (c *diskCache) trim() {
	type entry struct {
		path string
		size int64
		mod  int64
	}
	var all []entry
	var total int64
	filepath.WalkDir(c.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			all = append(all, entry{p, fi.Size(), fi.ModTime().UnixNano()})
			total += fi.Size()
		}
		return nil
	})
	if total <= cacheMax {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod < all[j].mod })
	for _, e := range all {
		if total <= cacheMax {
			break
		}
		if store.Remove(e.path) == nil {
			total -= e.size
		}
	}
}
