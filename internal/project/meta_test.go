package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadProfile(t *testing.T, profile string) *Project {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("Texte.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProfileName), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("%s : %v", profile, err)
	}
	return p
}

func TestMetaRead(t *testing.T) {
	p := loadProfile(t, `{"project":{"type":"article","status":"review","deadline":"2026-12-31","nextAction":"Relire  la partie 2"}}`)
	want := Meta{Type: "article", Status: "review", Deadline: "2026-12-31", NextAction: "Relire  la partie 2"}
	if p.Meta != want || len(p.MetaWarnings) != 0 {
		t.Fatalf("%+v %q", p.Meta, p.MetaWarnings)
	}
	if p := loadProfile(t, `{"title":"x"}`); p.Meta != (Meta{}) || p.MetaWarnings != nil {
		t.Fatalf("sans clé : %+v %q", p.Meta, p.MetaWarnings)
	}
	if p := loadProfile(t, `{"project":null}`); p.Meta != (Meta{}) || p.MetaWarnings != nil {
		t.Fatalf("null : %+v %q", p.Meta, p.MetaWarnings)
	}
}

// An invalid value gives a warning and is left out, the other values stay,
// and the text always opens (D-51).
func TestMetaInvalidIsAWarning(t *testing.T) {
	for _, tc := range []struct{ profile, warning string }{
		{`{"project":"article"}`, "objet"},
		{`{"project":[1,2]}`, "objet"},
		{`{"project":3}`, "objet"},
		{`{"project":{"status":"fini"}}`, "project.status"},
		{`{"project":{"status":3}}`, "project.status"},
		{`{"project":{"deadline":"31/12/2026"}}`, "project.deadline"},
		{`{"project":{"deadline":"2026-02-30"}}`, "project.deadline"},
		{`{"project":{"deadline":"2026-2-3"}}`, "project.deadline"},
		{`{"project":{"type":"` + strings.Repeat("x", 41) + `"}}`, "project.type"},
		{`{"project":{"nextAction":"` + strings.Repeat("é", 201) + `"}}`, "project.nextAction"},
		{`{"project":{"nextAction":"deux\nlignes"}}`, "project.nextAction"},
		{`{"project":{"nextAction":"bidi \u202e"}}`, "project.nextAction"},
	} {
		p := loadProfile(t, tc.profile)
		if len(p.MetaWarnings) != 1 || !strings.Contains(p.MetaWarnings[0], tc.warning) {
			t.Errorf("%s : %q", tc.profile, p.MetaWarnings)
		}
		if p.Meta != (Meta{}) {
			t.Errorf("%s : valeur gardée %+v", tc.profile, p.Meta)
		}
	}
	p := loadProfile(t, `{"project":{"status":"fini","type":"cours"}}`)
	if p.Meta.Type != "cours" || p.Meta.Status != "" || len(p.MetaWarnings) != 1 {
		t.Fatalf("%+v %q", p.Meta, p.MetaWarnings)
	}
}

// Neither a path nor a key gets in: only the four known keys are read, and an
// unknown one is said. Elsewhere in the profile, an unknown key stays refused.
func TestMetaAdmitsNoPathNorKey(t *testing.T) {
	p := loadProfile(t, `{"project":{"root":"/chemin","apiKey":"secret","type":"cours"}}`)
	if p.Meta != (Meta{Type: "cours"}) || len(p.MetaWarnings) != 2 {
		t.Fatalf("%+v %q", p.Meta, p.MetaWarnings)
	}
	for _, w := range p.MetaWarnings {
		if strings.Contains(w, "secret") || strings.Contains(w, "/chemin") {
			t.Errorf("valeur répétée dans l'avertissement : %q", w)
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("T\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ProfileName), []byte(`{"inconnu":1}`), 0o644)
	if _, err := Load(dir); err == nil {
		t.Fatal("clé inconnue du profil acceptée")
	}
}

// generic decodes a profile for a comparison of its content.
func generic(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%q : %v", b, err)
	}
	return v
}

// Only the value concerned changes: every other byte is kept, whatever the
// layout, the line endings or the order of the keys.
func TestSetMetaValueKeepsEveryOtherByte(t *testing.T) {
	for _, tc := range []struct{ name, before, key, value, after string }{
		{"valeur remplacée, à la main",
			"{\n\t\"title\" : \"Un\\u00e9\",   \"project\":{  \"status\" :\"draft\" , \"type\":\"x\"  }\n}\n",
			"status", "review",
			"{\n\t\"title\" : \"Un\\u00e9\",   \"project\":{  \"status\" :\"review\" , \"type\":\"x\"  }\n}\n"},
		{"CRLF, clé ajoutée dans project",
			"{\r\n  \"title\": \"T\",\r\n  \"project\": {\r\n    \"type\": \"cours\"\r\n  },\r\n  \"css\": \"\"\r\n}\r\n",
			"deadline", "2026-12-31",
			"{\r\n  \"title\": \"T\",\r\n  \"project\": {\r\n    \"type\": \"cours\",\r\n    \"deadline\": \"2026-12-31\"\r\n  },\r\n  \"css\": \"\"\r\n}\r\n"},
		{"CRLF, project ajouté",
			"{\r\n  \"title\": \"T\"\r\n}\r\n",
			"status", "active",
			"{\r\n  \"title\": \"T\",\r\n  \"project\": {\r\n    \"status\": \"active\"\r\n  }\r\n}\r\n"},
		{"sur une ligne",
			`{"title":"T","files":["a.md"]}`,
			"type", "note",
			`{"title":"T","files":["a.md"],"project":{"type":"note"}}`},
		{"objet vide",
			"{\n}\n",
			"status", "done",
			"{\"project\": {\"status\": \"done\"}\n}\n"},
		{"project vide",
			`{"project":{ }}`,
			"status", "done",
			`{"project":{"status": "done" }}`},
		{"valeur vidée sans retirer la clé",
			`{"project":{"deadline":"2026-01-01","type":"x"}}`,
			"deadline", "",
			`{"project":{"deadline":"","type":"x"}}`},
		{"valeur vide, clé absente : rien",
			"{ \"title\":\"T\" }\n",
			"deadline", "",
			"{ \"title\":\"T\" }\n"},
		{"clé répétée : la dernière, celle que lit le décodeur",
			`{"project":{"status":"draft","status":"active"}}`,
			"status", "done",
			`{"project":{"status":"draft","status":"done"}}`},
		{"project en capitales, que le décodeur prend aussi",
			`{"PROJECT":{"type":"x"}}`,
			"status", "done",
			`{"PROJECT":{"type":"x","status":"done"}}`},
		{"clé écrite avec un échappement",
			`{"pro\u006aect":{"st\u0061tus":"draft"}}`,
			"status", "done",
			`{"pro\u006aect":{"st\u0061tus":"done"}}`},
		{"accolades et guillemets dans les chaînes voisines",
			`{"title":"a}\"{","project":{"nextAction":"x,\"y\"}"},"css":"z.css"}`,
			"nextAction", "Relire & corriger « ça »",
			`{"title":"a}\"{","project":{"nextAction":"Relire & corriger « ça »"},"css":"z.css"}`},
		{"valeur remplacée quel que soit son type",
			`{"project":{"status":3,"type":{"a":[1,"}"]}}}`,
			"type", "cours",
			`{"project":{"status":3,"type":"cours"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SetMetaValue([]byte(tc.before), tc.key, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.after {
				t.Fatalf("\nattendu %q\nobtenu  %q", tc.after, got)
			}
			// Everything outside the key « project » decodes the same.
			a, b := generic(t, []byte(tc.before)), generic(t, got)
			for _, v := range []map[string]any{a, b} {
				delete(v, "project")
				delete(v, "PROJECT")
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("reste du profil changé : %v / %v", a, b)
			}
			if tc.value != "" {
				p, err := parseProfile(got)
				if err != nil {
					t.Fatal(err)
				}
				var m map[string]any
				json.Unmarshal(p.Project, &m)
				if v := m[tc.key]; v != tc.value {
					t.Fatalf("valeur relue %v", v)
				}
			}
		})
	}
}

func TestSetMetaValueRefuses(t *testing.T) {
	for _, tc := range []struct{ before, key, value string }{
		{`{"title":"T"}`, "status", "fini"},
		{`{"title":"T"}`, "deadline", "demain"},
		{`{"title":"T"}`, "root", "/chemin"},
		{`{"title":"T"}`, "nextAction", "deux\nlignes"},
		{`{ abîmé`, "status", "done"},
		{`[1,2]`, "status", "done"},
		{"\xef\xbb\xbf{}", "status", "done"},
		{`{"project":"x"}`, "status", "done"},
		{`{"project":null}`, "status", "done"},
	} {
		if got, err := SetMetaValue([]byte(tc.before), tc.key, tc.value); err == nil {
			t.Errorf("%s %s=%s accepté : %q", tc.before, tc.key, tc.value, got)
		}
	}
}

// Whatever the valid object: the new value is read back, and the rest of the
// document decodes the same.
func FuzzSetMetaValue(f *testing.F) {
	for _, s := range []string{
		`{}`, ` { } `, `{"a":1}`, "{\r\n\t\"a\" : [1,{\"b\":\"}\"}] ,\r\n\t\"project\" : { \"type\" : null }\r\n}\r\n",
		`{"project":{}}`, `{"project":{"status":"x","status":"y"},"project":{"deadline":"z"}}`, `{"\"":"\\","project":{"\u0074ype":"é"}}`,
	} {
		f.Add(s, "cours")
	}
	f.Fuzz(func(t *testing.T, s, value string) {
		value = strings.TrimSpace(value)
		if CheckMeta(MetaType, value) != nil || value == "" {
			return
		}
		got, err := SetMetaValue([]byte(s), MetaType, value)
		if err != nil {
			return
		}
		var before, after map[string]any
		if err := json.Unmarshal([]byte(s), &before); err != nil {
			t.Fatalf("accepté sans être un objet : %q", s)
		}
		if err := json.Unmarshal(got, &after); err != nil {
			t.Fatalf("%q donne un JSON invalide %q : %v", s, got, err)
		}
		m, _ := after["project"].(map[string]any)
		if m == nil || m[MetaType] != value {
			t.Fatalf("%q → %q : valeur non relue", s, got)
		}
		b4, _ := before["project"].(map[string]any)
		for k, v := range b4 {
			if k != MetaType && !reflect.DeepEqual(m[k], v) {
				t.Fatalf("%q → %q : project.%s changé", s, got, k)
			}
		}
		delete(before, "project")
		delete(after, "project")
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%q → %q : reste changé", s, got)
		}
	})
}
