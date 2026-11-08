package home

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/store"
)

var fixedTime = time.Unix(1000, 0)

// Only the values concerned change: a profile written by hand, in CRLF, keeps
// every other byte.
func TestWriteMetaKeepsEveryOtherByte(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glose.json")
	before := "{\r\n\t\"title\" : \"Livre\",   \"files\":[ \"a.md\" ],\r\n\t\"project\" : {\r\n\t\t\"status\" : \"draft\"\r\n\t}\r\n}\r\n"
	os.WriteFile(path, []byte(before), 0o640)
	h, wrote, err := WriteMeta(dir, "", []MetaChange{{project.MetaStatus, "review"}, {project.MetaDeadline, "2026-12-31"}, {project.MetaNextAction, "  Relire  "}})
	if err != nil || !wrote {
		t.Fatal(wrote, err)
	}
	want := "{\r\n\t\"title\" : \"Livre\",   \"files\":[ \"a.md\" ],\r\n\t\"project\" : {\r\n\t\t\"status\" : \"review\",\r\n\t\t\"deadline\" : \"2026-12-31\",\r\n\t\t\"nextAction\" : \"Relire\"\r\n\t}\r\n}\r\n"
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("\nattendu %q\nobtenu  %q", want, got)
	}
	if h != store.Hash(got) {
		t.Fatal("empreinte rendue")
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Fatalf("droits %v", fi.Mode().Perm())
	}
	// Nothing to change: nothing is written.
	os.Chtimes(path, fixedTime, fixedTime)
	if _, wrote, err := WriteMeta(dir, "", []MetaChange{{project.MetaStatus, "review"}}); err != nil || wrote {
		t.Fatal(wrote, err)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(fixedTime) {
		t.Fatal("réécrit sans changement")
	}
}

// A profile changed since it was read is refused and left as it is.
func TestWriteMetaRefusesAConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glose.json")
	os.WriteFile(path, []byte(`{"title":"Un"}`), 0o644)
	stale := store.Hash([]byte(`{"title":"Avant"}`))
	if _, _, err := WriteMeta(dir, stale, []MetaChange{{project.MetaStatus, "done"}}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflit attendu : %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"title":"Un"}` {
		t.Fatalf("écrit malgré le conflit : %s", b)
	}
}

// All the changes, or none: an invalid value and an unreadable profile leave
// the file as it is.
func TestWriteMetaWritesAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glose.json")
	for _, tc := range []struct {
		profile string
		changes []MetaChange
	}{
		{`{"title":"Un"}`, []MetaChange{{project.MetaStatus, "done"}, {project.MetaDeadline, "demain"}}},
		{`{"inconnu":1}`, []MetaChange{{project.MetaStatus, "done"}}},
		{`{ abîmé`, []MetaChange{{project.MetaStatus, "done"}}},
		{`{"project":"x"}`, []MetaChange{{project.MetaStatus, "done"}}},
	} {
		os.WriteFile(path, []byte(tc.profile), 0o644)
		if _, wrote, err := WriteMeta(dir, "", tc.changes); err == nil || wrote {
			t.Errorf("%s : accepté", tc.profile)
		}
		if b, _ := os.ReadFile(path); string(b) != tc.profile {
			t.Errorf("%s : écrit %s", tc.profile, b)
		}
	}
	if _, _, err := WriteMeta(filepath.Join(dir, "absent"), "", nil); err == nil {
		t.Error("profil absent accepté")
	}
}
