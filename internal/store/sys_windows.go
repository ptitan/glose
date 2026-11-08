//go:build windows

package store

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// LockFileEx and UnlockFileEx are not in package syscall; they are loaded from
// kernel32.dll, which keeps Glose free of dependencies.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x1
	lockfileExclusiveLock   = 0x2
	errorLockViolation      = syscall.Errno(33)
)

// lockFile locks the first byte of f, exclusively and without waiting.
// Windows releases the lock when the handle is closed or the process dies.
func lockFile(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately,
		0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r != 0 {
		return nil
	}
	if err == errorLockViolation {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r != 0 {
		return nil
	}
	return err
}

// renameFile replaces newpath by oldpath. On Windows, a file open elsewhere
// cannot be replaced for an instant (another process, or this one, reading it):
// the attempt is made again for a while, as the replacement itself is safe.
func renameFile(oldpath, newpath string) error {
	const (
		accessDenied     = syscall.Errno(5)
		sharingViolation = syscall.Errno(32)
	)
	var err error
	for i := 0; i < 50; i++ {
		if err = os.Rename(oldpath, newpath); err == nil {
			return nil
		}
		var code syscall.Errno
		if !errors.As(err, &code) || (code != accessDenied && code != sharingViolation) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}

// syncDir is not needed on Windows: there is no directory handle to flush.
func syncDir(string) error { return nil }
