package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked means that another process (or another Lock of this process)
// holds the lock of the project.
var ErrLocked = errors.New("le projet est déjà verrouillé par un autre processus")

// Lock is the exclusive lock of a project: only its holder writes the files
// and the journal. It is an advisory lock on a file of the .glose directory,
// released by Unlock or, if the process dies, by the operating system.
type Lock struct {
	f *os.File
}

// LockName is the name of the lock file inside the state directory.
const LockName = "lock"

// TryLock takes the lock of the state directory dir (created if needed)
// without waiting. It returns ErrLocked if the lock is held.
func TryLock(dir string) (*Lock, error) { return TryLockNamed(dir, LockName) }

// TryLockNamed takes the lock held in the file name of dir, without waiting.
// Glose uses one lock for the writes ("lock") and one for the server, which
// guarantees that a single server runs per project ("server.lock").
func TryLockNamed(dir, name string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f}, nil
}

// WaitLock takes the lock, waiting until it is free or ctx is done.
func WaitLock(ctx context.Context, dir string) (*Lock, error) {
	return WaitLockNamed(ctx, dir, LockName)
}

// WaitLockNamed takes the lock held in the file name of dir, waiting until
// it is free or ctx is done.
func WaitLockNamed(ctx context.Context, dir, name string) (*Lock, error) {
	for {
		l, err := TryLockNamed(dir, name)
		if !errors.Is(err, ErrLocked) {
			return l, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Unlock releases the lock.
func (l *Lock) Unlock() error {
	if err := unlockFile(l.f); err != nil {
		l.f.Close()
		return err
	}
	return l.f.Close()
}

// Held reports whether the lock file is still in place at its path: the
// file this lock holds has not been removed (a state directory removed
// while the lock is held; on Windows, an open file is not removed).
func (l *Lock) Held() bool {
	cur, err := os.Stat(l.f.Name())
	if err != nil {
		return false
	}
	mine, err := l.f.Stat()
	return err == nil && os.SameFile(cur, mine)
}
