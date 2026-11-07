// Package store is the only package of Glose that writes files.
// It reads files as bytes, identifies their content by a SHA-256 hash, and
// writes only if the file still has the expected content, through a temporary
// file and a rename, so that a file is never left half written.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotSynced means that the new content is in place, but that the
// directory could not be synced: the file is written, its durability after a
// crash of the machine is not certain.
var ErrNotSynced = errors.New("contenu remplacé, mais le dossier n'a pas pu être synchronisé")

// ErrConflict means that the file changed since it was read.
var ErrConflict = errors.New("le fichier a changé depuis sa lecture")

// Doc is the content of a file and its hash.
type Doc struct {
	Bytes []byte
	Hash  string
}

// Hash returns the hexadecimal SHA-256 hash of b.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Read reads a whole file, without any transformation.
func Read(path string) (Doc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, err
	}
	return Doc{b, Hash(b)}, nil
}

// beforeFinalCheck is called after the temporary file is written, just
// before the last hash check. Tests set it to simulate a change made by
// another program, or a failure, in the middle of a write.
var beforeFinalCheck func() error

// afterRename is called once the new content is in place, before the
// directory is synced; tests set it to simulate a failure of that sync.
var afterRename func() error

// SetAfterRename sets the test hook afterRename (for the tests of other
// packages) and returns a function that removes it.
func SetAfterRename(f func() error) (restore func()) {
	afterRename = f
	return func() { afterRename = nil }
}

// WriteIfUnchanged replaces the content of path with data, provided the file
// still has the hash expected. It returns the hash of the new content.
//
// The data goes to a temporary file in the same directory, which is flushed
// to disk and then renamed over the original: at every moment the file holds
// either its old or its new content. The permissions of the original are kept.
// A symbolic link is followed: the file it points to is replaced, not the link.
//
// The hash is checked twice, before writing and just before renaming; a
// program that writes the file between that last check and the rename is not
// detected.
func WriteIfUnchanged(path, expected string, data []byte) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if err := checkHash(target, expected); err != nil {
		return "", err
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(target)
	tmpName, err := writeTemp(dir, data, info.Mode().Perm())
	if err != nil {
		return "", err
	}
	done := false
	defer func() {
		if !done {
			os.Remove(tmpName)
		}
	}()
	if beforeFinalCheck != nil {
		if err := beforeFinalCheck(); err != nil {
			return "", err
		}
	}
	// The link may have been pointed elsewhere meanwhile: resolve it again,
	// and check the content through the path the caller gave.
	if again, err := filepath.EvalSymlinks(path); err != nil {
		return "", err
	} else if again != target {
		return "", ErrConflict
	}
	if err := checkHash(path, expected); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", fmt.Errorf("remplacement de %s : %w", target, err)
	}
	done = true
	if afterRename != nil {
		if err := afterRename(); err != nil {
			return Hash(data), fmt.Errorf("%w : %v", ErrNotSynced, err)
		}
	}
	if err := SyncDir(dir); err != nil {
		return Hash(data), fmt.Errorf("%w : %v", ErrNotSynced, err)
	}
	return Hash(data), nil
}

func checkHash(path, expected string) error {
	cur, err := Read(path)
	if err != nil {
		return err
	}
	if cur.Hash != expected {
		return ErrConflict
	}
	return nil
}
