package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// isolate gives the test a configuration directory of its own, and moves the
// temporary directory of the system elsewhere: the projects made by the test
// under t.TempDir() then count as ordinary directories.
func isolate(t *testing.T) (config, base string) {
	t.Helper()
	base = t.TempDir()
	if real, err := filepath.EvalSymlinks(base); err == nil {
		base = real
	}
	config = filepath.Join(base, "config")
	other := filepath.Join(base, "systeme-tmp")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLOSE_CONFIG_DIR", config)
	t.Setenv("TMPDIR", other)
	t.Setenv("TMP", other)
	t.Setenv("TEMP", other)
	return config, base
}

func makeProject(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "glose.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("Texte.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func roots(t *testing.T) []string {
	t.Helper()
	es, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range es {
		out = append(out, e.Root)
	}
	return out
}

func TestRegisterKeepsOneEntryPerRoot(t *testing.T) {
	_, base := isolate(t)
	a := makeProject(t, filepath.Join(base, "a"))
	b := makeProject(t, filepath.Join(base, "b"))
	for _, r := range []string{a, b, a} {
		if err := Register(r); err != nil {
			t.Fatal(err)
		}
	}
	got := roots(t)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("attendu a (dernier ouvert) puis b : %q", got)
	}
}

// A directory without a profile is not a project of the registry, and what
// lives under the temporary directory of the system stays out.
func TestRegisterSkipsWhatIsNotAProject(t *testing.T) {
	config, base := isolate(t)
	plain := filepath.Join(base, "sans-profil")
	os.MkdirAll(plain, 0o755)
	if err := Register(plain); err != nil {
		t.Fatal(err)
	}
	tmp := makeProject(t, filepath.Join(base, "systeme-tmp", "copie"))
	if err := Register(tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(config, registryFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registre écrit pour rien : %v", err)
	}
}

// A link to a project records the resolved root.
func TestRegisterResolvesLinks(t *testing.T) {
	_, base := isolate(t)
	real := makeProject(t, filepath.Join(base, "vrai"))
	link := filepath.Join(base, "lien")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("liens symboliques indisponibles")
	}
	if err := Register(link); err != nil {
		t.Fatal(err)
	}
	if got := roots(t); len(got) != 1 || got[0] != real {
		t.Fatalf("racine résolue attendue : %q", got)
	}
}

// Simultaneous openings lose no entry.
func TestConcurrentRegisterLosesNothing(t *testing.T) {
	_, base := isolate(t)
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		dir := makeProject(t, filepath.Join(base, "p", fmt.Sprint(i)))
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Register(dir)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := roots(t); len(got) != n {
		t.Fatalf("%d inscriptions sur %d", len(got), n)
	}
}

// An unreadable registry is an error, and is never overwritten.
func TestUnreadableRegistryIsNeverOverwritten(t *testing.T) {
	config, base := isolate(t)
	a := makeProject(t, filepath.Join(base, "a"))
	os.MkdirAll(config, 0o700)
	path := filepath.Join(config, registryFile)
	for _, bad := range []string{
		"{ pas du json",
		`{"version":2,"projects":[]}`,
		`{"version":1,"projects":[],"inconnu":1}`,
		`{"version":1,"projects":[{"root":"relatif","opened":"2026-01-01T00:00:00Z"}]}`,
		`{"version":1,"projects":[{"root":"` + filepath.ToSlash(a) + `","opened":"hier"}]}`,
		`{"version":1,"projects":[]} {}`,
	} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Register(a); err == nil || !strings.Contains(err.Error(), "illisible") {
			t.Errorf("%q : Register rend %v", bad, err)
		}
		if _, err := Forget(a); err == nil {
			t.Errorf("%q : Forget réussit", bad)
		}
		if _, err := Load(); err == nil {
			t.Errorf("%q : Load réussit", bad)
		}
		if b, _ := os.ReadFile(path); string(b) != bad {
			t.Errorf("%q écrasé par %q", bad, b)
		}
	}
}

// A project is forgotten by its identifier, its root or a path inside it,
// even once its directory has disappeared; the project itself is not touched.
func TestForget(t *testing.T) {
	_, base := isolate(t)
	a := makeProject(t, filepath.Join(base, "a"))
	b := makeProject(t, filepath.Join(base, "b"))
	c := makeProject(t, filepath.Join(base, "c"))
	sub := filepath.Join(c, "chapitres")
	os.MkdirAll(sub, 0o755)
	for _, r := range []string{a, b, c} {
		if err := Register(r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Forget(ID(a)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a, "glose.json")); err != nil {
		t.Fatal("projet touché")
	}
	os.RemoveAll(b)
	if _, err := Forget(b); err != nil {
		t.Fatal(err)
	}
	if e, err := Forget(sub); err != nil || e.Root != c {
		t.Fatal(e, err)
	}
	if got := roots(t); len(got) != 0 {
		t.Fatalf("restent %q", got)
	}
	if _, err := Forget(a); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("projet inconnu : %v", err)
	}
}

// A path inside a project whose nearest profile is gone, or that lies in a
// nested project: the parent is never removed in its place.
func TestForgetNeverRemovesAParentInstead(t *testing.T) {
	_, base := isolate(t)
	parent := makeProject(t, filepath.Join(base, "livre"))
	nested := makeProject(t, filepath.Join(parent, "annexe"))
	sub := filepath.Join(nested, "chapitres")
	os.MkdirAll(sub, 0o755)
	for _, r := range []string{parent, nested} {
		if err := Register(r); err != nil {
			t.Fatal(err)
		}
	}
	// The nested project has lost its profile: its registered root still names it.
	os.Remove(filepath.Join(nested, "glose.json"))
	if e, err := Forget(sub); err != nil || e.Root != nested {
		t.Fatalf("%+v %v", e, err)
	}
	if got := roots(t); len(got) != 1 || got[0] != parent {
		t.Fatalf("restent %q", got)
	}
	// A nested project that is not registered: not the parent.
	other := makeProject(t, filepath.Join(parent, "autre"))
	if _, err := Forget(other); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("%v", err)
	}
	if got := roots(t); len(got) != 1 {
		t.Fatalf("restent %q", got)
	}
}
