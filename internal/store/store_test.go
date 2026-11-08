package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var original = []byte("\ufeffL’été\r\nSeconde ligne\r\n")

func setup(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "texte.md")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func content(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// noTemporaryFiles checks that no ".glose-*.tmp" file was left behind.
func noTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".glose-") {
			t.Errorf("fichier temporaire laissé : %s", e.Name())
		}
	}
}

func TestReadKeepsBytesAsTheyAre(t *testing.T) {
	_, path := setup(t)
	d, err := Read(path)
	if err != nil || !bytes.Equal(d.Bytes, original) || d.Hash != Hash(original) {
		t.Fatalf("Read = %q, %s, %v", d.Bytes, d.Hash, err)
	}
}

func TestWriteIfUnchanged(t *testing.T) {
	dir, path := setup(t)
	next := []byte("\ufeffL’été\r\nTroisième version\r\n")
	h, err := WriteIfUnchanged(path, Hash(original), next)
	if err != nil {
		t.Fatal(err)
	}
	if got := content(t, path); !bytes.Equal(got, next) || h != Hash(next) {
		t.Fatalf("contenu %q, empreinte %s", got, h)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
			t.Errorf("droits %v, attendu 0640", info.Mode().Perm())
		}
	}
	noTemporaryFiles(t, dir)
}

func TestWriteIsRefusedIfTheFileChanged(t *testing.T) {
	dir, path := setup(t)
	external := []byte("modifié par un autre programme\n")
	if err := os.WriteFile(path, external, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteIfUnchanged(path, Hash(original), []byte("écrasement")); !errors.Is(err, ErrConflict) {
		t.Fatalf("erreur %v, attendu ErrConflict", err)
	}
	if got := content(t, path); !bytes.Equal(got, external) {
		t.Fatalf("le fichier a été écrasé : %q", got)
	}
	noTemporaryFiles(t, dir)
}

func TestChangeDuringTheWriteIsDetected(t *testing.T) {
	dir, path := setup(t)
	external := []byte("écrit par un autre programme pendant l'écriture\n")
	defer func() { beforeFinalCheck = nil }()
	beforeFinalCheck = func() error { return os.WriteFile(path, external, 0o640) }
	if _, err := WriteIfUnchanged(path, Hash(original), []byte("Glose")); !errors.Is(err, ErrConflict) {
		t.Fatalf("erreur %v, attendu ErrConflict", err)
	}
	if got := content(t, path); !bytes.Equal(got, external) {
		t.Fatalf("la modification extérieure a été écrasée : %q", got)
	}
	noTemporaryFiles(t, dir)
}

func TestFailureDuringTheWriteLeavesTheOriginal(t *testing.T) {
	dir, path := setup(t)
	defer func() { beforeFinalCheck = nil }()
	beforeFinalCheck = func() error { return errors.New("panne simulée") }
	if _, err := WriteIfUnchanged(path, Hash(original), []byte("nouveau")); err == nil {
		t.Fatal("erreur attendue")
	}
	if got := content(t, path); !bytes.Equal(got, original) {
		t.Fatalf("original abîmé : %q", got)
	}
	noTemporaryFiles(t, dir)
}

func TestWriteFollowsSymbolicLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques : droits particuliers sous Windows")
	}
	dir, path := setup(t)
	link := filepath.Join(dir, "lien.md")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteIfUnchanged(link, Hash(original), []byte("par le lien")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("le lien a été remplacé par un fichier")
	}
	if got := content(t, path); string(got) != "par le lien" {
		t.Fatalf("cible : %q", got)
	}
}

func TestLinkRetargetedDuringTheWriteIsDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liens symboliques : droits particuliers sous Windows")
	}
	dir := t.TempDir()
	a, b, link := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md"), filepath.Join(dir, "lien.md")
	os.WriteFile(a, []byte("A"), 0o644)
	os.WriteFile(b, []byte("B"), 0o644)
	if err := os.Symlink(a, link); err != nil {
		t.Fatal(err)
	}
	defer func() { beforeFinalCheck = nil }()
	beforeFinalCheck = func() error {
		os.Remove(link)
		return os.Symlink(b, link)
	}
	if _, err := WriteIfUnchanged(link, Hash([]byte("A")), []byte("nouveau")); !errors.Is(err, ErrConflict) {
		t.Fatalf("erreur %v, attendu ErrConflict", err)
	}
	if string(content(t, a)) != "A" || string(content(t, b)) != "B" {
		t.Fatalf("contenus modifiés : a = %q, b = %q", content(t, a), content(t, b))
	}
	noTemporaryFiles(t, dir)
}

func TestLockIsExclusive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".glose")
	first, err := TryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second verrou : %v, attendu ErrLocked", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if _, err := WaitLock(ctx, dir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("attente : %v, attendu l'expiration", err)
	}
	if err := first.Unlock(); err != nil {
		t.Fatal(err)
	}
	second, err := WaitLock(context.Background(), dir)
	if err != nil {
		t.Fatalf("verrou libéré, mais : %v", err)
	}
	second.Unlock()
}

// TestLockAcrossProcesses holds the lock in a child process (this test binary
// run again), checks that the parent is refused, kills the child brutally and
// checks that the operating system released the lock.
func TestLockAcrossProcesses(t *testing.T) {
	if dir := os.Getenv("GLOSE_LOCK_HOLDER"); dir != "" {
		if _, err := TryLock(dir); err != nil {
			fmt.Println("échec", err)
			os.Exit(1)
		}
		fmt.Println("verrouillé")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	dir := filepath.Join(t.TempDir(), ".glose")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "GLOSE_LOCK_HOLDER="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "verrouillé" {
		cmd.Process.Kill()
		t.Fatalf("le processus enfant n'a pas pris le verrou : %q", line)
	}
	if _, err := TryLock(dir); !errors.Is(err, ErrLocked) {
		cmd.Process.Kill()
		t.Fatalf("verrou détenu par un autre processus : %v, attendu ErrLocked", err)
	}
	cmd.Process.Kill()
	cmd.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l, err := WaitLock(ctx, dir)
	if err != nil {
		t.Fatalf("verrou non libéré après la mort du détenteur : %v", err)
	}
	l.Unlock()
}

func TestWritableLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, ".glose", "sous")
	if err := Writable(state); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("Writable a laissé %v", entries)
	}
	f := filepath.Join(dir, "fichier")
	os.WriteFile(f, nil, 0o644)
	if err := Writable(f); err == nil {
		t.Fatal("un fichier pris pour un dossier")
	}
}

func TestCreateAtomicRefusesAFileThatAppeared(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etat.json")
	beforeFinalCheck = func() error { return os.WriteFile(path, []byte("autre"), 0o600) }
	defer func() { beforeFinalCheck = nil }()
	if err := CreateAtomic(path, []byte("le mien"), 0o600); err != ErrConflict {
		t.Fatalf("erreur : %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "autre" {
		t.Fatalf("remplacé : %q", b)
	}
	noTemporaryFiles(t, dir)
	beforeFinalCheck = nil
	if err := CreateAtomic(path, []byte("x"), 0o600); err != ErrConflict {
		t.Fatalf("fichier existant : %v", err)
	}
	os.Remove(path)
	if err := CreateAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A state file read all the time (the presence of the agent, polled by the
// interface) is still replaced: on Windows, a reader once made the replacement
// fail, and the write was lost (P10.13).
func TestWriteAtomicWhileBeingRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etat.json")
	if err := WriteAtomic(path, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case <-time.After(time.Millisecond):
					os.ReadFile(path)
				}
			}
		}()
	}
	var err error
	for i := 0; i < 300 && err == nil; i++ {
		err = WriteAtomic(path, []byte(strconv.Itoa(i)), 0o600)
	}
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "299" {
		t.Fatalf("dernière écriture perdue : %q", b)
	}
}
