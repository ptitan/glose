package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Durability (P5.4). Every file that Glose writes goes through a temporary
// file in the same directory, flushed to disk (fsync) before it is renamed;
// the directory is flushed after the rename. After a crash of the process,
// a file holds its old or its new content, never a mix. After a power cut,
// once the call has returned without error, the new content survives as far
// as the system and the disk honour fsync (not verified on every system).
// ErrNotSynced means that the new content is in place but that the directory
// could not be flushed: the rename may be lost by a power cut.

// syncHook, if set by a test, is called before each flush with the path of
// the file or directory; an error stands for a failed flush.
var syncHook func(path string) error

// SetSyncFailure makes the flushes fail when f returns an error, for the
// tests of other packages; it returns the function that restores the normal
// behaviour.
func SetSyncFailure(f func(path string) error) (restore func()) {
	prev := syncHook
	syncHook = f
	return func() { syncHook = prev }
}

func flushFile(f *os.File) error {
	if syncHook != nil {
		if err := syncHook(f.Name()); err != nil {
			return err
		}
	}
	return f.Sync()
}

// SyncDir flushes a directory to disk, so that the files renamed or created
// in it survive a power cut.
func SyncDir(dir string) error {
	if syncHook != nil {
		if err := syncHook(dir); err != nil {
			return err
		}
	}
	return syncDir(dir)
}

// mkdirDurable creates dir and its missing parents, then flushes the parent
// of each directory it created, from the top down, so that the new
// directories survive a power cut with what they will contain. The error is
// ErrNotSynced when the directories exist but a flush failed.
func mkdirDurable(dir string) error {
	var created []string
	for d := dir; ; {
		if _, err := os.Lstat(d); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		created = append(created, d)
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for i := len(created) - 1; i >= 0; i-- {
		if err := SyncDir(filepath.Dir(created[i])); err != nil {
			return fmt.Errorf("%w : %v", ErrNotSynced, err)
		}
	}
	return nil
}

// EnsureDir creates the directory dir, with permissions perm, and flushes its
// parent. Its parent must exist. It is for the directories of a text (a
// template copied in a project); those of Glose's state come with their files.
func EnsureDir(dir string, perm os.FileMode) error {
	if err := os.Mkdir(dir, perm); err != nil {
		return err
	}
	if err := SyncDir(filepath.Dir(dir)); err != nil {
		return fmt.Errorf("%w : %v", ErrNotSynced, err)
	}
	return nil
}

// writeTemp writes data to a new temporary file of dir, flushed to disk and
// closed, with permissions perm. It returns its name; on error, nothing is
// left behind.
func writeTemp(dir string, data []byte, perm os.FileMode) (string, error) {
	tmp, err := os.CreateTemp(dir, ".glose-*.tmp")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = flushFile(tmp)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, perm)
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// WriteAtomic replaces (or creates) a state file of Glose, such as a copy of
// the files handed over or .glose/server.json, durably (see above). It is not
// used for the reviewed texts, which go through WriteIfUnchanged.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	mkErr := mkdirDurable(dir)
	if mkErr != nil && !errors.Is(mkErr, ErrNotSynced) {
		return mkErr
	}
	name, err := writeTemp(dir, data, perm)
	if err != nil {
		return err
	}
	if err := renameFile(name, path); err != nil {
		os.Remove(name)
		return err
	}
	if err := SyncDir(dir); err != nil {
		return fmt.Errorf("%w : %v", ErrNotSynced, err)
	}
	return mkErr
}

// CreateAtomic creates a state file of Glose that did not exist when it was
// read, durably (see above): it is refused (ErrConflict) if the file has
// appeared meanwhile. As for WriteIfUnchanged, a file created between the
// last check and the rename is not detected.
func CreateAtomic(path string, data []byte, perm os.FileMode) error {
	if _, err := os.Lstat(path); err == nil {
		return ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	mkErr := mkdirDurable(dir)
	if mkErr != nil && !errors.Is(mkErr, ErrNotSynced) {
		return mkErr
	}
	name, err := writeTemp(dir, data, perm)
	if err != nil {
		return err
	}
	if beforeFinalCheck != nil {
		err = beforeFinalCheck()
	}
	if err == nil {
		if _, serr := os.Lstat(path); serr == nil {
			err = ErrConflict
		}
	}
	if err == nil {
		err = renameFile(name, path)
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	if err := SyncDir(dir); err != nil {
		return fmt.Errorf("%w : %v", ErrNotSynced, err)
	}
	return mkErr
}

// Writable tells whether Glose could create its state files in dir (glose
// doctor). It probes by creating then removing a temporary file; if dir does
// not exist yet, the probe is made in its nearest existing parent, where dir
// would be created. Nothing is left behind, and dir is not created.
func Writable(dir string) error {
	for {
		fi, err := os.Stat(dir)
		if err == nil {
			if !fi.IsDir() {
				return &os.PathError{Op: "stat", Path: dir, Err: errors.New("n'est pas un dossier")}
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return err
		}
		dir = parent
	}
	tmp, err := os.CreateTemp(dir, ".glose-doctor-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	tmp.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("fichier d'essai %s laissé (effacez-le) : %w", name, err)
	}
	return nil
}

// Remove deletes a state file; a missing file is not an error.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// OpenLog opens a log file of Glose (the server's output) for appending.
// Nothing durable: see OpenJournal for the journal.
func OpenLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

// OpenJournal opens the journal for appending. While the file is empty (just
// created, or created by an attempt whose flush failed), its directory and
// that directory's parent are flushed before it is returned, so that the lines that the caller flushes
// are not lost with the directory entry.
func OpenJournal(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := mkdirDurable(dir); err != nil {
		return nil, err
	}
	f, err := OpenLog(path)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err != nil || fi.Size() == 0 {
		// The journal's directory, and its own entry in the project (an
		// earlier attempt may have created it without flushing).
		if err == nil {
			err = SyncDir(dir)
		}
		if err == nil {
			err = SyncDir(filepath.Dir(dir))
		}
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%w : %v", ErrNotSynced, err)
		}
	}
	return f, nil
}

// WriteCache writes a cache file of Glose: through a temporary file and a
// rename (a reader never sees half a file), without flushing to disk. A
// cache is never normative (invariant 4): lost or damaged, it is rebuilt.
func WriteCache(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".glose-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = renameFile(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// KeepsPermissions tells whether the file system of dir (or of its nearest
// existing parent) keeps the permissions of a file: a probe file created
// with 0600 must read back as 0600. On /mnt/c from WSL without the
// « metadata » option, every file is 0777: the private state of Glose
// (journal, drafts) is then readable by every user of the machine. Nothing
// is left behind. On Windows, permissions are ACLs: always true.
func KeepsPermissions(dir string) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
		dir = parent
	}
	tmp, err := os.CreateTemp(dir, ".glose-doctor-*.tmp")
	if err != nil {
		return true // Writable reports it
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	if os.Chmod(name, 0o600) != nil {
		return false
	}
	fi, err := os.Stat(name)
	return err == nil && fi.Mode().Perm() == 0o600
}
