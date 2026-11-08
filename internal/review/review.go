// Package review gives the annotations of a text their identity (D-19): each
// annotation, and each message of its thread, is followed by a technical
// comment that carries its identifier and its author:
//
//	{~~ancien~>nouveau~~}{>>glose:v1 id=k3fz9a by=agent<<}
//	{==passage==}{>>remarque<<}{>>glose:v1 id=p2m8qe by=author<<}{>>réponse<<}{>>glose:v1 id=w7xh2c re=p2m8qe by=agent<<}
//
// The head of an annotation is its mark, plus, for a highlighted passage, the
// remark that follows it; the technical comment of the annotation comes right
// after the head. Every later comment of the thread is a message, followed by
// its own technical comment. These comments are invisible in every view: they
// are comments.
//
// Package review reads that structure, reports what is wrong with it
// (duplicated, misplaced or malformed identifiers, a message moved to another
// thread), and writes missing identifiers without changing anything else.
package review

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/edit"
	"github.com/ptitan/glose/internal/notes"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/projection"
)

// Actors.
const (
	Author  = "author"
	Agent   = "agent"
	Unknown = "unknown"
)

// MetaPrefix starts the text of every technical comment.
const MetaPrefix = "glose:v1"

// Meta is the content of a technical comment.
type Meta struct {
	ID string // identifier of the annotation or of the message
	Re string // for a message: identifier of its annotation
	By string // author, agent or unknown
	// Fix marks a message that carries a correction of the proposal it
	// answers: its text is what the proposal should propose instead. The
	// author may apply it (it does not apply itself, D-14).
	Fix bool
	// To is the short name of the agent that must answer an annotation of
	// the author (D-49); Via the short name of the agent that wrote a
	// message or a proposal of the agent. Unknown to the versions before
	// P10.6, which ignore them.
	To, Via string
	// Model is the model of the agent that wrote it (P10.11), when known.
	Model string
}

// modelRe is the form of the value of model= (as agents.CheckModel).
var modelRe = regexp.MustCompile(`^[A-Za-z0-9._:/#@+-]{1,200}$`)

// nameRe is the form of the values of to= and via= (short names, D-49).
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// ValidName reports whether s may be the value of to= or via=.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// Bytes returns the technical comment, delimiters included.
func (m Meta) Bytes() []byte {
	var b strings.Builder
	b.WriteString(critic.Opener(critic.Comment))
	b.WriteString(MetaPrefix)
	b.WriteString(" id=" + m.ID)
	if m.Re != "" {
		b.WriteString(" re=" + m.Re)
	}
	if m.By != "" {
		b.WriteString(" by=" + m.By)
	}
	if m.Fix {
		b.WriteString(" fix=1")
	}
	if m.To != "" {
		b.WriteString(" to=" + m.To)
	}
	if m.Via != "" {
		b.WriteString(" via=" + m.Via)
	}
	if m.Model != "" {
		b.WriteString(" model=" + m.Model)
	}
	b.WriteString(critic.Closer(critic.Comment))
	return []byte(b.String())
}

// IsMeta reports whether the text of a comment is a technical comment, well
// formed or not.
func IsMeta(text []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(text), []byte("glose:"))
}

// ParseMeta reads the text of a technical comment.
func ParseMeta(text []byte) (Meta, error) {
	f := strings.Fields(string(text))
	if len(f) == 0 || f[0] != MetaPrefix {
		return Meta{}, errors.New("commentaire technique d'une version inconnue")
	}
	var m Meta
	seen := map[string]bool{}
	for _, kv := range f[1:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || seen[k] {
			return Meta{}, fmt.Errorf("commentaire technique illisible : %q", kv)
		}
		seen[k] = true
		switch k {
		case "id":
			if !ValidID(v) {
				return Meta{}, fmt.Errorf("identifiant invalide : %q", v)
			}
			m.ID = v
		case "re":
			if !ValidID(v) {
				return Meta{}, fmt.Errorf("identifiant invalide : %q", v)
			}
			m.Re = v
		case "by":
			if v != Author && v != Agent && v != Unknown {
				return Meta{}, fmt.Errorf("auteur inconnu : %q", v)
			}
			m.By = v
		case "fix":
			m.Fix = v == "1"
		case "model":
			if !modelRe.MatchString(v) {
				return Meta{}, fmt.Errorf("modèle invalide : %q", v)
			}
			m.Model = v
		case "to", "via":
			if !ValidName(v) {
				return Meta{}, fmt.Errorf("nom court invalide : %q", v)
			}
			if k == "to" {
				m.To = v
			} else {
				m.Via = v
			}
		default:
			// Unknown keys are kept for later versions; they are ignored.
		}
	}
	if m.ID == "" {
		return Meta{}, errors.New("commentaire technique sans identifiant")
	}
	return m, nil
}

// idAlphabet avoids the characters that are easily confused (0, o, 1, l).
const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// ValidID reports whether s may be an identifier.
func ValidID(s string) bool {
	if len(s) < 4 || len(s) > 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789", rune(s[i])) {
			return false
		}
	}
	return true
}

// Gen draws new identifiers that are not taken yet.
type Gen struct {
	taken map[string]bool // every identifier that may not be drawn again
	live  map[string]bool // those carried by an annotation of the files now
	// rand fills b with random bytes; tests replace it.
	rand func(b []byte) error
}

// NewGen returns a generator that avoids the identifiers of taken, all of
// them deemed carried by the files now.
func NewGen(taken map[string]bool) *Gen { return NewGenLive(taken, nil) }

// NewGenLive returns a generator that avoids the identifiers carried by the
// files (live) and those of the history (reserved). An identifier of the
// history may still be restored to the annotation that lost it (Assign).
func NewGenLive(live, reserved map[string]bool) *Gen {
	g := &Gen{taken: map[string]bool{}, live: map[string]bool{},
		rand: func(b []byte) error { _, err := rand.Read(b); return err }}
	for k := range live {
		g.taken[k], g.live[k] = true, true
	}
	for k := range reserved {
		g.taken[k] = true
	}
	return g
}

// Take marks id as used, and carried by an annotation.
func (g *Gen) Take(id string) { g.taken[id], g.live[id] = true, true }

// New returns a fresh identifier of six characters.
func (g *Gen) New() (string, error) {
	for range 100 {
		b := make([]byte, 6)
		if err := g.rand(b); err != nil {
			return "", err
		}
		for i := range b {
			b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
		}
		if id := string(b); !g.taken[id] {
			g.Take(id)
			return id, nil
		}
	}
	return "", errors.New("impossible de tirer un identifiant libre")
}

// Message is one message of a thread.
type Message struct {
	ID, By  string
	To, Via string       // Meta.To, Meta.Via
	Model   string       // Meta.Model
	Fix     bool         // a correction of the proposal (Meta.Fix)
	Mark    critic.Mark  // the comment
	Meta    *critic.Span // its technical comment, if any
	Span    critic.Span  // the comment and its technical comment
}

// Annotation is an annotation with its identity and its thread.
type Annotation struct {
	ID, By  string
	To, Via string // Meta.To, Meta.Via of the annotation
	Model   string // Meta.Model of the annotation
	Kind    critic.Kind
	Mark    critic.Mark  // the main mark
	Span    critic.Span  // the whole annotation, thread included
	Head    critic.Span  // the mark, and the remark of a highlighted passage
	Meta    *critic.Span // the technical comment of the annotation, if any
	// Messages is the thread: for a comment, the comment itself first; for a
	// highlighted passage, its remark first (both carry the identity of the
	// annotation); then the replies.
	Messages []Message
}

// Issue codes of the identity layer.
const (
	IssueMalformed  = "commentaire technique illisible"
	IssueMisplaced  = "commentaire technique mal placé"
	IssueDuplicated = "identifiant en double"
	IssueMoved      = "message rattaché à une autre annotation"
)

// Issue is a problem of identity at a byte offset.
type Issue struct {
	Offset  int
	Message string
}

// Doc is a text read with the identity of its annotations.
type Doc struct {
	Src         []byte
	Marks       []critic.Mark
	MarkIssues  []critic.Issue // grammar problems (package critic)
	Annotations []Annotation
	Issues      []Issue // identity problems
}

// Read analyses src.
func Read(src []byte) *Doc {
	marks, mi := critic.Parse(src)
	d := &Doc{Src: src, Marks: marks, MarkIssues: mi}
	isMeta := func(i int) bool {
		return marks[i].Kind == critic.Comment && IsMeta(src[marks[i].Text.Start:marks[i].Text.End])
	}
	meta := func(i int) (Meta, bool) {
		m, err := ParseMeta(src[marks[i].Text.Start:marks[i].Text.End])
		if err != nil {
			d.Issues = append(d.Issues, Issue{marks[i].Span.Start, IssueMalformed + " : " + err.Error()})
			return Meta{}, false
		}
		return m, true
	}
	seen := map[string]int{}
	use := func(id string, at int) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			d.Issues = append(d.Issues, Issue{at, IssueDuplicated + " : " + id})
			return
		}
		seen[id] = at
	}
	for _, g := range critic.Group(marks) {
		elems := append([]int{g.Main}, g.Comments...)
		if isMeta(g.Main) {
			// A technical comment that follows nothing: a leftover.
			for _, e := range elems {
				if isMeta(e) {
					d.Issues = append(d.Issues, Issue{marks[e].Span.Start, IssueMisplaced})
				}
			}
			continue
		}
		main := marks[g.Main]
		a := Annotation{Kind: main.Kind, Mark: main, Span: g.Span, Head: main.Span}
		i := 1
		if main.Kind == critic.Highlight && i < len(elems) && !isMeta(elems[i]) {
			a.Head.End = marks[elems[i]].Span.End
			a.Messages = append(a.Messages, Message{Mark: marks[elems[i]], Span: marks[elems[i]].Span})
			i++
		}
		if main.Kind == critic.Comment {
			a.Messages = append(a.Messages, Message{Mark: main, Span: main.Span})
		}
		if i < len(elems) && isMeta(elems[i]) {
			s := marks[elems[i]].Span
			a.Meta = &s
			if m, ok := meta(elems[i]); ok {
				a.ID, a.By, a.To, a.Via, a.Model = m.ID, m.By, m.To, m.Via, m.Model
				if m.Re != "" {
					d.Issues = append(d.Issues, Issue{s.Start, IssueMisplaced + " (identifiant de message en tête d'annotation)"})
				}
				use(m.ID, s.Start)
			}
			i++
		}
		for k := range a.Messages { // the head messages share the identity of the annotation
			a.Messages[k].ID, a.Messages[k].By, a.Messages[k].To, a.Messages[k].Via, a.Messages[k].Model = a.ID, a.By, a.To, a.Via, a.Model
			if a.Meta != nil {
				a.Messages[k].Span.End = a.Meta.End
			}
		}
		for ; i < len(elems); i++ {
			e := elems[i]
			if isMeta(e) {
				d.Issues = append(d.Issues, Issue{marks[e].Span.Start, IssueMisplaced})
				continue
			}
			msg := Message{Mark: marks[e], Span: marks[e].Span}
			if i+1 < len(elems) && isMeta(elems[i+1]) {
				s := marks[elems[i+1]].Span
				msg.Meta = &s
				msg.Span.End = s.End
				if m, ok := meta(elems[i+1]); ok {
					msg.ID, msg.By, msg.Fix, msg.To, msg.Via, msg.Model = m.ID, m.By, m.Fix, m.To, m.Via, m.Model
					use(m.ID, s.Start)
					if m.Re != "" && a.ID != "" && m.Re != a.ID {
						d.Issues = append(d.Issues, Issue{s.Start, IssueMoved + " : " + m.ID})
					}
				}
				i++
			}
			a.Messages = append(a.Messages, msg)
		}
		d.Annotations = append(d.Annotations, a)
	}
	return d
}

// IDs returns every identifier of the text (annotations and messages).
func (d *Doc) IDs() []string {
	var out []string
	for _, a := range d.Annotations {
		if a.ID != "" {
			out = append(out, a.ID)
		}
		for _, m := range a.Messages[min(len(a.Messages), headMessages(a)):] {
			if m.ID != "" {
				out = append(out, m.ID)
			}
		}
	}
	return out
}

// headMessages is the number of messages that belong to the head.
func headMessages(a Annotation) int {
	switch {
	case a.Kind == critic.Comment:
		return 1
	case a.Kind == critic.Highlight && a.Head.End > a.Mark.Span.End:
		return 1
	}
	return 0
}

// Replies returns the messages of a that are replies (not its head).
func (a Annotation) Replies() []Message { return a.Messages[headMessages(a):] }

// Find returns the annotation with identifier id.
func (d *Doc) Find(id string) (Annotation, bool) {
	for _, a := range d.Annotations {
		if a.ID == id {
			return a, true
		}
	}
	return Annotation{}, false
}

// At returns the annotation that starts at offset start.
func (d *Doc) At(start int) (Annotation, bool) {
	for _, a := range d.Annotations {
		if a.Span.Start == start {
			return a, true
		}
	}
	return Annotation{}, false
}

// ErrIdentity is returned when an edit would change something else than
// technical comments.
var ErrIdentity = errors.New("l'ajout des identifiants modifierait le texte ou les annotations")

// Assign writes the missing identifiers of the annotations and messages that
// intersect [from, to) (that is, share at least one byte with it),
// attributed to by. Identifiers come from gen. It
// returns the new text, or src itself if nothing was missing.
func Assign(src []byte, from, to int, by string, gen *Gen) ([]byte, error) {
	d := Read(src)
	for _, id := range d.IDs() {
		gen.Take(id)
	}
	type ins struct {
		at   int
		meta []byte
	}
	var todo []ins
	for _, a := range d.Annotations {
		if a.Span.End <= from || a.Span.Start >= to {
			continue
		}
		id := a.ID
		if a.Meta == nil {
			// An identity erased while its replies still name it is restored.
			if re := repliedTo(src, a); re != "" && !gen.live[re] {
				id = re
				gen.Take(re)
			} else {
				nid, err := gen.New()
				if err != nil {
					return nil, err
				}
				id = nid
			}
			todo = append(todo, ins{a.Head.End, Meta{ID: id, By: by}.Bytes()})
		}
		for _, m := range a.Replies() {
			if m.Meta != nil {
				continue
			}
			nid, err := gen.New()
			if err != nil {
				return nil, err
			}
			todo = append(todo, ins{m.Mark.Span.End, Meta{ID: nid, Re: id, By: by}.Bytes()})
		}
	}
	if len(todo) == 0 {
		return src, nil
	}
	out := make([]byte, 0, len(src)+len(todo)*40)
	pos := 0
	for _, t := range todo {
		out = append(out, src[pos:t.at]...)
		out = append(out, t.meta...)
		pos = t.at
	}
	out = append(out, src[pos:]...)
	if err := SameButMeta(src, out); err != nil {
		return nil, err
	}
	return out, nil
}

// repliedTo returns the annotation identifier named by the replies of a, if
// they all name the same one.
func repliedTo(src []byte, a Annotation) string {
	re := ""
	for _, m := range a.Replies() {
		if m.Meta == nil {
			continue
		}
		mm, err := ParseMeta(src[m.Meta.Start+3 : m.Meta.End-3])
		if err != nil || mm.Re == "" || (re != "" && mm.Re != re) {
			return ""
		}
		re = mm.Re
	}
	return re
}

// shape is what a text says once its technical comments are set aside: the
// views, and the annotations element by element.
type shape struct {
	current, proposed []byte
	annotations       [][]string
	issues            []critic.IssueCode
}

func shapeOf(src []byte) shape {
	s := shape{
		current:  projection.Project(src, projection.Current).Text,
		proposed: projection.Project(src, projection.Proposed).Text,
	}
	marks, issues := critic.Parse(src)
	for _, is := range issues {
		s.issues = append(s.issues, is.Code)
	}
	for _, g := range critic.Group(marks) {
		var row []string
		for _, i := range append([]int{g.Main}, g.Comments...) {
			m := marks[i]
			if m.Kind == critic.Comment && IsMeta(src[m.Text.Start:m.Text.End]) {
				continue
			}
			row = append(row, fmt.Sprintf("%d\x00%s\x00%s", m.Kind, src[m.Text.Start:m.Text.End], src[m.New.Start:m.New.End]))
		}
		if len(row) > 0 {
			s.annotations = append(s.annotations, row)
		}
	}
	return s
}

// SameButMeta checks that after differs from before only by technical
// comments: same views, same annotations and threads element by element, same
// grammar problems.
func SameButMeta(before, after []byte) error {
	a, b := shapeOf(before), shapeOf(after)
	if !bytes.Equal(a.current, b.current) || !bytes.Equal(a.proposed, b.proposed) ||
		fmt.Sprint(a.annotations) != fmt.Sprint(b.annotations) || fmt.Sprint(a.issues) != fmt.Sprint(b.issues) {
		return ErrIdentity
	}
	return nil
}

// Diff returns the single edit that turns src into out: the range between
// their common prefix and their common suffix.
func Diff(src, out []byte) patch.Edit {
	p := 0
	for p < len(src) && p < len(out) && src[p] == out[p] {
		p++
	}
	s := 0
	for s < len(src)-p && s < len(out)-p && src[len(src)-1-s] == out[len(out)-1-s] {
		s++
	}
	return patch.Edit{Start: p, End: len(src) - s, Insert: append([]byte(nil), out[p:len(out)-s]...)}
}

// WithIDs takes an edit already verified by package patch or edit, and adds
// the missing identifiers of the annotations it touches, attributed to by.
// It returns a single edit of src, which covers at least the range of e.
func WithIDs(src []byte, e patch.Edit, by string, gen *Gen) (patch.Edit, error) {
	out := e.Apply(src)
	full, err := Assign(out, e.Start, e.Start+len(e.Insert), by, gen)
	if err != nil {
		return patch.Edit{}, err
	}
	d := Diff(src, full)
	start, end := min(d.Start, e.Start), max(d.End, e.End)
	return patch.Edit{Start: start, End: end, Insert: append([]byte(nil), full[start:len(full)-(len(src)-end)]...)}, nil
}

// Comment comments a passage (or a point if span is empty), with identity.
func Comment(src []byte, span critic.Span, note, by string, gen *Gen) (patch.Edit, error) {
	e, err := patch.Comment(src, span, note)
	if err != nil {
		return patch.Edit{}, err
	}
	return WithIDs(src, e, by, gen)
}

// Reply adds a message at the end of the thread of the annotation that
// starts at offset start (or whose identifier is id, if id is not empty). It
// returns the edit and the offset of the new message in the edited text. An
// annotation without identity receives one, attributed to by.
func Reply(src []byte, start int, id, note, by string, gen *Gen) (patch.Edit, int, error) {
	d := Read(src)
	a, ok := d.At(start)
	if id != "" {
		a, ok = d.Find(id)
	}
	if !ok {
		return patch.Edit{}, 0, ErrNotFound
	}
	// First the identity of the annotation and of its thread, then the reply.
	src1, err := Assign(src, a.Span.Start, a.Span.End, by, gen)
	if err != nil {
		return patch.Edit{}, 0, err
	}
	d1 := Read(src1)
	a1, ok := d1.At(a.Span.Start)
	if !ok {
		return patch.Edit{}, 0, ErrNotFound
	}
	e, err := patch.Reply(src1, d1.Marks, d1.Group(a1), note)
	if err != nil {
		return patch.Edit{}, 0, err
	}
	at := e.Start
	e, err = WithIDs(src1, e, by, gen)
	if err != nil {
		return patch.Edit{}, 0, err
	}
	full := e.Apply(src1)
	return Diff(src, full).Cover(src, full, a.Span.Start, a.Span.End), at, nil
}

// Propose turns src[span] into proposals towards text (package edit), with a
// message attached if note is not empty, and gives them their identity.
//
// Footnotes (package notes): « ^[text] » in text becomes a reference to a new
// note, whose definition is proposed at the end of the file; a reference
// deleted, if it was the last one to its note, takes the definition with it.
// These proposals are linked to the rewriting (notes.Links).
//
// It returns the edit of src, and the range of the rewriting itself in the
// edited text (to undo it: its linked proposals go with it).
func Propose(src []byte, span critic.Span, text, note, by string, gen *Gen) (patch.Edit, critic.Span, error) {
	return propose(src, span, text, note, by, gen, false)
}

// ProposeWhole is Propose, with the rewriting as one proposal, accepted or
// rejected as a whole (D-32, edit.ProposeWhole). Footnotes keep their linked
// proposals.
func ProposeWhole(src []byte, span critic.Span, text, note, by string, gen *Gen) (patch.Edit, critic.Span, error) {
	return propose(src, span, text, note, by, gen, true)
}

func propose(src []byte, span critic.Span, text, note, by string, gen *Gen, whole bool) (patch.Edit, critic.Span, error) {
	old := src[span.Start:span.End]
	var defs []notes.Def
	if !bytes.Contains(old, []byte("^[")) {
		taken := notes.Labels(src)
		for l := range notes.Labels([]byte(text)) {
			taken[l] = true // labels written in the new text itself
		}
		text, defs = notes.Expand(text, taken)
	}
	e, err := edit.ProposeWithNoteWhole(src, span, text, note, whole)
	if err != nil {
		return patch.Edit{}, critic.Span{}, err
	}
	if whole {
		// A definition of a note inside the proposal would escape the linked
		// treatment of notes (its reference deleted, it would stay alone).
		ins := e.Insert
		marks, _ := critic.Parse(ins)
		for _, m := range marks {
			if (m.Kind == critic.Deletion || m.Kind == critic.Substitution) && footnoteDef.Match(ins[m.Text.Start:m.Text.End]) {
				return patch.Edit{}, critic.Span{}, ErrWholeNote
			}
		}
	}
	if e, err = WithIDs(src, e, by, gen); err != nil {
		return patch.Edit{}, critic.Span{}, err
	}
	out := e.Apply(src)
	main := critic.Span{Start: e.Start, End: e.Start + len(e.Insert)}
	// extra applies a further proposal to out, keeping main in place.
	extra := func(e2 patch.Edit, err error) error {
		if err != nil {
			return err
		}
		if e2, err = WithIDs(out, e2, by, gen); err != nil {
			return err
		}
		if e2.End <= main.Start {
			d := len(e2.Insert) - (e2.End - e2.Start)
			main.Start, main.End = main.Start+d, main.End+d
		}
		out = e2.Apply(out)
		return nil
	}
	// Notes whose last reference is deleted: their definition goes too.
	proposed := projection.Project(out, projection.Proposed).Text
	gone := notes.Refs(proposed)
	for label := range notes.Refs(old) {
		if gone[label] || notes.Refs([]byte(text))[label] {
			continue
		}
		if s, ok := notes.Definition(out, label); ok {
			// Best effort: a definition that cannot be proposed for deletion
			// (annotated, malformed neighbour) stays; the reference is gone.
			_ = extra(patch.ProposeDeletion(out, s))
		}
	}
	// One proposal per definition: each note is decided on its own.
	for _, d := range defs {
		if err := extra(patch.ProposeAddition(out, len(out), string(notes.Append(out, []notes.Def{d})))); err != nil {
			return patch.Edit{}, critic.Span{}, err
		}
	}
	return Diff(src, out).Cover(src, out, e.Start, e.End), main, nil
}

// ErrWholeNote: a rewriting proposed as a whole cannot take a footnote
// definition in.
var ErrWholeNote = errors.New("une réécriture d'un tenant ne peut pas englober la définition d'une note de bas de page : réécrivez sans elle, ou proposez mot à mot")

var footnoteDef = regexp.MustCompile(`(?m)^ {0,3}\[\^[^\]\s]+\]:`)

// Errors of Revise.
var (
	ErrNotYours = errors.New("seules vos propres propositions se corrigent ; répondez à celle-ci, ou rejetez-la et proposez la vôtre")
	ErrLinked   = errors.New("une note de bas de page ajoutée ou supprimée ne se corrige pas : annulez-la, puis refaites la réécriture")
)

// Revise corrects the proposal that starts at offset start (or whose
// identifier is id, if id is not empty) so that it proposes text instead
// (P3.2): the mark stays one mark, with its identity and its thread. Only the
// proposals of by (or those still without identity) can be corrected, and not
// the proposals linked to a footnote (package notes), which are decided
// together. It returns the edit and the range of the corrected mark in the
// edited text.
func Revise(src []byte, start int, id, text, by string, gen *Gen) (patch.Edit, critic.Span, error) {
	d := Read(src)
	a, ok := d.At(start)
	if id != "" {
		a, ok = d.Find(id)
	}
	if !ok {
		return patch.Edit{}, critic.Span{}, ErrNotFound
	}
	if a.By != by && !(a.ID == "" && a.By == "") {
		return patch.Edit{}, critic.Span{}, ErrNotYours
	}
	if _, linked := notes.Links(src)[a.Span.Start]; linked {
		return patch.Edit{}, critic.Span{}, ErrLinked
	}
	e, err := patch.Revise(src, d.Marks, d.Group(a), text)
	if err != nil {
		return patch.Edit{}, critic.Span{}, err
	}
	// A proposal written by hand receives its identity now.
	full, err := WithIDs(src, e, by, gen)
	if err != nil {
		return patch.Edit{}, critic.Span{}, err
	}
	return full, critic.Span{Start: e.Start, End: e.Start + len(e.Insert)}, nil
}

// Errors of Fix.
var (
	ErrFixKind = errors.New("une correction ne se joint qu'à une proposition (ajout, suppression, remplacement)")
	ErrFixMine = errors.New("cette proposition est la vôtre : corrigez-la directement")
	// ErrFixSymbol: « ∅ » alone is how a correction says « delete ».
	ErrFixSymbol = errors.New("une correction réduite au signe « ∅ » se lirait comme une suppression : écrivez le signe avec ses voisins, ou répondez dans le fil")
)

// Fix adds to the thread of the proposal id a correction by by: a message
// whose text is what the proposal should propose instead (Meta.Fix). It
// changes neither the proposal nor its text: the other party applies it, or
// not. It returns the edit and the start of the new message.
func Fix(src []byte, id, text, by string, gen *Gen) (patch.Edit, int, error) {
	d := Read(src)
	a, ok := d.Find(id)
	if !ok {
		return patch.Edit{}, 0, ErrNotFound
	}
	if a.Kind != critic.Addition && a.Kind != critic.Deletion && a.Kind != critic.Substitution {
		return patch.Edit{}, 0, ErrFixKind
	}
	if a.By == by {
		return patch.Edit{}, 0, ErrFixMine
	}
	if a.ID == "" {
		// Its identity would be given to by, as if the proposal were his.
		return patch.Edit{}, 0, errors.New("cette proposition n'a pas encore d'identité : répondez dans son fil")
	}
	if a.Kind == critic.Addition && text == "" {
		return patch.Edit{}, 0, patch.ErrEmpty
	}
	if text == fixDelete {
		return patch.Edit{}, 0, ErrFixSymbol
	}
	if bytes.Equal([]byte(text), src[a.Mark.New.Start:a.Mark.New.End]) && a.Kind == critic.Substitution ||
		a.Kind == critic.Addition && text == string(src[a.Mark.Text.Start:a.Mark.Text.End]) {
		return patch.Edit{}, 0, patch.ErrSame
	}
	// A comment cannot be empty: a correction that deletes says so in words
	// (the text of the message is then the empty proposal, marked « ∅ »).
	body := text
	if body == "" {
		body = fixDelete
	}
	e, at, err := Reply(src, a.Span.Start, "", body, by, gen)
	if err != nil {
		return patch.Edit{}, 0, err
	}
	out := e.Apply(src)
	// The technical comment of the new message receives the mark of a fix.
	d2 := Read(out)
	for _, ann := range d2.Annotations {
		for _, m := range ann.Replies() {
			if m.Span.Start != at || m.Meta == nil {
				continue
			}
			meta := Meta{ID: m.ID, Re: ann.ID, By: by, Fix: true, To: m.To, Via: m.Via, Model: m.Model}.Bytes()
			fixed := patch.Edit{Start: m.Meta.Start, End: m.Meta.End, Insert: meta}.Apply(out)
			if err := SameButMeta(out, fixed); err != nil {
				return patch.Edit{}, 0, err
			}
			return Diff(src, fixed).Cover(src, fixed, a.Span.Start, a.Span.End), at, nil
		}
	}
	return patch.Edit{}, 0, ErrNotFound
}

// FixDelete is the text of a correction that proposes to delete the
// passage. A correction cannot propose this symbol alone (ErrFixSymbol).
const FixDelete = "∅"

const fixDelete = FixDelete

// FixText returns what a correction message proposes (the empty text for a
// deletion).
func FixText(msg []byte) string {
	if string(msg) == fixDelete {
		return ""
	}
	return string(msg)
}

// Errors of Amend.
var (
	ErrAmendNotYours = errors.New("seuls vos propres messages se modifient ; répondez plutôt dans le fil")
	ErrAmendFix      = errors.New("une correction proposée par l'agent ne se modifie pas : reprenez-la dans votre proposition, ou répondez")
	ErrAmendMeta     = errors.New("un message ne peut pas commencer par « glose: », réservé aux identifiants")
)

// Amend rewrites the message that starts at offset at, so that it says text
// instead: the remark of a comment or of a highlighted passage, or a reply.
// Only the messages of by (or those still without identity) can be amended,
// and not a correction (Meta.Fix). The identity and the thread stay; a
// message written by hand receives its identity now, attributed to by. It
// returns the edit and the message as it was.
//
// Whether the message was already handed over to the other party is not
// known here (package navette): the caller refuses those.
func Amend(src []byte, at int, text, by string, gen *Gen) (patch.Edit, Message, error) {
	if IsMeta([]byte(text)) {
		return patch.Edit{}, Message{}, ErrAmendMeta
	}
	d := Read(src)
	for _, a := range d.Annotations {
		for _, m := range a.Messages {
			if m.Mark.Span.Start != at {
				continue
			}
			if m.Fix {
				return patch.Edit{}, m, ErrAmendFix
			}
			if m.By != by && !(m.ID == "" && m.By == "") {
				return patch.Edit{}, m, ErrAmendNotYours
			}
			idx := -1
			for i, mk := range d.Marks {
				if mk.Span == m.Mark.Span {
					idx = i
				}
			}
			e, err := patch.Amend(src, d.Marks, d.Group(a), idx, text)
			if err != nil {
				return patch.Edit{}, m, err
			}
			full, err := WithIDs(src, e, by, gen)
			return full, m, err
		}
	}
	return patch.Edit{}, Message{}, ErrNotFound
}

// ErrNotFound is returned when no annotation or message has the identifier
// or the position given.
var ErrNotFound = errors.New("annotation introuvable")

// RemoveReply removes the reply that starts at offset at, with its technical
// comment (to undo a reply). The head of an annotation cannot be removed this
// way.
func RemoveReply(src []byte, at int) (patch.Edit, error) {
	d := Read(src)
	for _, a := range d.Annotations {
		for _, m := range a.Replies() {
			if m.Span.Start == at {
				return patch.RemoveRange(src, d.Marks, a.group(d.Marks), m.Span)
			}
		}
	}
	return patch.Edit{}, ErrNotFound
}

// group returns the critic annotation of a.
func (a Annotation) group(marks []critic.Mark) critic.Annotation {
	for _, g := range critic.Group(marks) {
		if g.Span == a.Span {
			return g
		}
	}
	return critic.Annotation{Main: -1}
}

// Group returns the critic annotation (indices of marks) of a, for the
// operations of package patch.
func (d *Doc) Group(a Annotation) critic.Annotation { return a.group(d.Marks) }

// Recipient is the agent that must answer the annotation (D-49): the To of
// its last message of the author that has one, else its own; "" for the
// default agent.
func (a Annotation) Recipient() string {
	to := a.To
	for _, m := range a.Replies() {
		if m.By == Author && m.To != "" {
			to = m.To
		}
	}
	return to
}

// setMeta rewrites the technical comments of the annotations and messages
// of out whose identifier is in ids, by f. It returns out unchanged if
// there is nothing to do.
func setMeta(out []byte, ids map[string]bool, f func(*Meta)) ([]byte, error) {
	if len(ids) == 0 {
		return out, nil
	}
	d := Read(out)
	type rw struct {
		at   critic.Span
		meta []byte
	}
	var todo []rw
	visit := func(span *critic.Span) error {
		m, err := ParseMeta(out[span.Start+3 : span.End-3])
		if err != nil {
			return err
		}
		f(&m)
		todo = append(todo, rw{*span, m.Bytes()})
		return nil
	}
	for _, a := range d.Annotations {
		if a.Meta != nil && ids[a.ID] {
			if err := visit(a.Meta); err != nil {
				return nil, err
			}
		}
		for _, m := range a.Replies() {
			if m.Meta != nil && ids[m.ID] {
				if err := visit(m.Meta); err != nil {
					return nil, err
				}
			}
		}
	}
	res := out
	for i := len(todo) - 1; i >= 0; i-- { // from the end: offsets stay valid
		res = patch.Edit{Start: todo[i].at.Start, End: todo[i].at.End, Insert: todo[i].meta}.Apply(res)
	}
	if err := SameButMeta(out, res); err != nil {
		return nil, err
	}
	return res, nil
}

// created returns the identifiers that are in after and not in before.
func created(before, after []byte) map[string]bool {
	old := map[string]bool{}
	for _, id := range Read(before).IDs() {
		old[id] = true
	}
	out := map[string]bool{}
	for _, id := range Read(after).IDs() {
		if !old[id] {
			out[id] = true
		}
	}
	return out
}

// Address writes to=name on the annotations and messages that the edit e of
// src creates (an annotation or a reply of the author addressed to an
// agent, D-49). An empty name changes nothing.
func Address(src []byte, e patch.Edit, name string) (patch.Edit, error) {
	return stamp(src, e, name, func(m *Meta) { m.To = name })
}

// Sign writes via=name on the annotations and messages that the edit e of
// src creates (the agent who wrote them, D-49).
func Sign(src []byte, e patch.Edit, name string) (patch.Edit, error) {
	return SignModel(src, e, name, "")
}

// SignModel is Sign, with model=model when the model is known (P10.11): the
// model is written even without a name (the default agent « agent »).
func SignModel(src []byte, e patch.Edit, name, model string) (patch.Edit, error) {
	if model != "" && !modelRe.MatchString(model) {
		model = ""
	}
	if name == "" && model == "" {
		return e, nil
	}
	if name != "" && !ValidName(name) {
		return patch.Edit{}, fmt.Errorf("nom court invalide : %q", name)
	}
	out := e.Apply(src)
	res, err := setMeta(out, created(src, out), func(m *Meta) { m.Via, m.Model = name, model })
	if err != nil {
		return patch.Edit{}, err
	}
	return Diff(src, res), nil
}

func stamp(src []byte, e patch.Edit, name string, f func(*Meta)) (patch.Edit, error) {
	if name == "" {
		return e, nil
	}
	if !ValidName(name) {
		return patch.Edit{}, fmt.Errorf("nom court invalide : %q", name)
	}
	out := e.Apply(src)
	res, err := setMeta(out, created(src, out), f)
	if err != nil {
		return patch.Edit{}, err
	}
	return Diff(src, res), nil
}
