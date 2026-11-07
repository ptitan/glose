//go:build unix

package store

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive flock on f without waiting. flock locks belong
// to the open file: a second open of the same file, even in this process, is
// refused, and the kernel releases the lock when the process dies.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// syncDir flushes the directory entry of a renamed file to disk.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
