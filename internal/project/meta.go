package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Meta is the key « project » of glose.json (D-51): what the author says of
// the project. What Glose computes (the hand, the annotations) is not there.
// No path and no secret: the repository of the text may be public.
type Meta struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	Deadline   string `json:"deadline"`
	NextAction string `json:"nextAction"`
}

// Editorial statuses. « done » closes nothing: it sorts (D-51).
const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusReview   = "review"
	StatusDone     = "done"
	StatusArchived = "archived"
)

// Keys of Meta, in the order of the profile.
const (
	MetaType       = "type"
	MetaStatus     = "status"
	MetaDeadline   = "deadline"
	MetaNextAction = "nextAction"
)

var metaKeys = []string{MetaType, MetaStatus, MetaDeadline, MetaNextAction}

const (
	maxTypeLen       = 40
	maxNextActionLen = 200
)

// checkLine refuses what would break a line of the overview, or show one
// text for another (control characters, line separators, bidirectional
// overrides).
func checkLine(key, v string, max int) error {
	if !utf8.ValidString(v) {
		return fmt.Errorf("%s : UTF-8 invalide", key)
	}
	if n := utf8.RuneCountInString(v); n > max {
		return fmt.Errorf("%s : %d caractères au plus (%d ici)", key, max, n)
	}
	for _, r := range v {
		if unicode.IsControl(r) || r == ' ' || r == ' ' || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩') {
			return fmt.Errorf("%s : une seule ligne de texte, sans caractère de contrôle", key)
		}
	}
	return nil
}

// CheckMeta validates the value of a key of Meta. An empty value means that
// the key is not filled in.
func CheckMeta(key, v string) error {
	if v == "" {
		return nil
	}
	switch key {
	case MetaType:
		return checkLine("project.type", v, maxTypeLen)
	case MetaNextAction:
		return checkLine("project.nextAction", v, maxNextActionLen)
	case MetaStatus:
		switch v {
		case StatusDraft, StatusActive, StatusReview, StatusDone, StatusArchived:
			return nil
		}
		return fmt.Errorf("project.status %q : draft, active, review, done ou archived attendu", v)
	case MetaDeadline:
		if d, err := time.Parse("2006-01-02", v); err != nil || d.Format("2006-01-02") != v {
			return fmt.Errorf("project.deadline %q : une date AAAA-MM-JJ existante est attendue", v)
		}
		return nil
	}
	return fmt.Errorf("project.%s : clé inconnue", key)
}

func (m *Meta) field(key string) *string {
	switch key {
	case MetaType:
		return &m.Type
	case MetaStatus:
		return &m.Status
	case MetaDeadline:
		return &m.Deadline
	case MetaNextAction:
		return &m.NextAction
	}
	return nil
}

// parseMeta reads the key « project » leniently: a value that is invalid is
// left out with a warning, and never prevents opening the text (D-51). Only
// the four known keys are admitted: neither a path nor a secret gets in.
func parseMeta(raw json.RawMessage) (Meta, []string) {
	var m Meta
	var warnings []string
	if len(raw) == 0 || string(raw) == "null" {
		return m, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return m, []string{"la clé project doit être un objet : elle est ignorée"}
	}
	var keys []string
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dst := m.field(k)
		if dst == nil {
			warnings = append(warnings, fmt.Sprintf("project.%s : clé inconnue, ignorée (type, status, deadline et nextAction seulement ; ni chemin ni clé d'accès)", k))
			continue
		}
		var v string
		if err := json.Unmarshal(fields[k], &v); err != nil {
			warnings = append(warnings, fmt.Sprintf("project.%s : du texte est attendu ; valeur ignorée", k))
			continue
		}
		v = strings.TrimSpace(v)
		if err := CheckMeta(k, v); err != nil {
			warnings = append(warnings, err.Error()+" ; valeur ignorée")
			continue
		}
		*dst = v
	}
	return m, warnings
}

// ---------- Changing the metadata without touching the rest ----------

// ErrNotObject means that the content of the profile cannot hold the change.
var ErrNotObject = errors.New("glose.json doit être un objet JSON valide")

// member is one « key: value » of a JSON object, by byte offsets.
type member struct {
	key                string
	before             int // end of what precedes the key: after « { » or « , »
	keyStart, keyEnd   int
	valueStart, valEnd int
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// skipString returns the offset after the string that starts at i.
func skipString(b []byte, i int) int {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return i
}

// skipValue returns the offset after the value that starts at i.
func skipValue(b []byte, i int) int {
	if i >= len(b) {
		return i
	}
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				i = skipString(b, i)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return i
	}
	for i < len(b) && !strings.ContainsRune(",}] \t\r\n", rune(b[i])) {
		i++
	}
	return i
}

// scanObject lists the members of the object that opens at start, in a
// document already known to be valid JSON, and returns the offset of its
// closing brace.
func scanObject(b []byte, start int) ([]member, int) {
	var out []member
	i := start + 1
	for {
		before := i
		i = skipSpace(b, i)
		if i >= len(b) || b[i] == '}' {
			return out, i
		}
		m := member{before: before, keyStart: i}
		m.keyEnd = skipString(b, i)
		json.Unmarshal(b[m.keyStart:m.keyEnd], &m.key)
		i = skipSpace(b, m.keyEnd)
		i = skipSpace(b, i+1) // the colon
		m.valueStart = i
		m.valEnd = skipValue(b, i)
		out = append(out, m)
		i = skipSpace(b, m.valEnd)
		if i < len(b) && b[i] == ',' {
			i++
		}
	}
}

// lastNamed returns the last member called key: that of a repeated key which
// the JSON decoder keeps.
func lastNamed(ms []member, key string) (member, bool) {
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].key == key {
			return ms[i], true
		}
	}
	return member{}, false
}

func splice(b []byte, start, end int, s string) []byte {
	out := make([]byte, 0, len(b)-(end-start)+len(s))
	out = append(out, b[:start]...)
	out = append(out, s...)
	return append(out, b[end:]...)
}

func encodeString(v string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false) // « & » reste « & » dans un fichier écrit à la main
	enc.Encode(v)
	return strings.TrimSuffix(sb.String(), "\n")
}

// addMember inserts a member after the last one of the object at start, with
// the white space and the separator of that last one (fine layout and line
// endings are kept), or just after the opening brace of an empty object.
func addMember(b []byte, start int, ms []member, key, value string) []byte {
	k := encodeString(key)
	if len(ms) == 0 {
		return splice(b, start+1, start+1, k+": "+value)
	}
	last := ms[len(ms)-1]
	space := string(b[last.before:last.keyStart])
	sep := string(b[last.keyEnd:last.valueStart])
	return splice(b, last.valEnd, last.valEnd, ","+space+k+sep+value)
}

// newProjectObject writes the object of the key « project », in the layout of
// the object that receives it (ms, its members; space is the white space that
// precedes its last member).
func newProjectObject(b []byte, ms []member, key, value string) string {
	k := encodeString(key)
	if len(ms) == 0 {
		return "{" + k + ": " + value + "}"
	}
	last := ms[len(ms)-1]
	space := string(b[last.before:last.keyStart])
	sep := string(b[last.keyEnd:last.valueStart])
	i := strings.LastIndexByte(space, '\n')
	if i < 0 {
		return "{" + k + sep + value + "}"
	}
	nl, indent := "\n", space[i+1:]
	if i > 0 && space[i-1] == '\r' {
		nl = "\r\n"
	}
	return "{" + nl + indent + indent + k + sep + value + nl + indent + "}"
}

// SetMetaValue returns the content of a profile in which the key of the object
// « project » has the given value: only that value changes, every other byte
// is kept (invariant 1). An absent key is added, with the layout around it;
// an empty value on an absent key changes nothing. The value is validated.
func SetMetaValue(raw []byte, key, value string) ([]byte, error) {
	if err := CheckMeta(key, value); err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, ErrNotObject
	}
	top := skipSpace(raw, 0)
	if top >= len(raw) || raw[top] != '{' {
		return nil, ErrNotObject
	}
	encoded := encodeString(value)
	ms, _ := scanObject(raw, top)
	// The decoder of the profile ignores the case of a key.
	var pm member
	ok := false
	for _, m := range ms {
		if strings.EqualFold(m.key, "project") {
			pm, ok = m, true
		}
	}
	if !ok {
		if value == "" {
			return append([]byte(nil), raw...), nil
		}
		return addMember(raw, top, ms, "project", newProjectObject(raw, ms, key, encoded)), nil
	}
	if raw[pm.valueStart] != '{' {
		return nil, errors.New("la clé project de glose.json n'est pas un objet : corrigez-la d'abord")
	}
	inner, _ := scanObject(raw, pm.valueStart)
	if m, ok := lastNamed(inner, key); ok {
		return splice(raw, m.valueStart, m.valEnd, encoded), nil
	}
	if value == "" {
		return append([]byte(nil), raw...), nil
	}
	return addMember(raw, pm.valueStart, inner, key, encoded), nil
}
