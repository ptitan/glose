package home

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

// MetaChange is one value to set in the key « project » of a profile.
type MetaChange struct {
	Key   string
	Value string
}

// WriteMeta changes values of the key « project » of the profile at root: only
// these values change, every other byte of glose.json is kept (D-51). The write
// is conditional: expected is the hash of the profile read by the caller (""
// reads it now), and a profile changed since is refused with store.ErrConflict.
// It returns the hash of the profile, and whether it was written.
func WriteMeta(root, expected string, changes []MetaChange) (string, bool, error) {
	path := filepath.Join(root, project.ProfileName)
	d, err := store.Read(path)
	if err != nil {
		return "", false, err
	}
	if expected == "" {
		expected = d.Hash
	}
	if d.Hash != expected {
		return "", false, store.ErrConflict
	}
	if err := project.CheckProfile(d.Bytes); err != nil {
		return "", false, fmt.Errorf("%s illisible : %w", project.ProfileName, err)
	}
	out := d.Bytes
	for _, c := range changes {
		// Same reading as the profile: without spaces around.
		if out, err = project.SetMetaValue(out, c.Key, strings.TrimSpace(c.Value)); err != nil {
			return "", false, err
		}
	}
	if string(out) == string(d.Bytes) {
		return d.Hash, false, nil
	}
	h, err := store.WriteIfUnchanged(path, expected, out)
	if err != nil && !errors.Is(err, store.ErrNotSynced) {
		return "", false, err
	}
	return h, true, nil
}
