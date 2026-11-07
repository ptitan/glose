package sourcepos

// Status tells how far a rendered fragment can be traced back to the source.
type Status int

const (
	// Certified: the byte range is exact and holds exactly the rendered text
	// (for a word) or only blanks (for a space). It may be edited.
	Certified Status = iota
	// Transformed: the byte range is exact but its content differs from the
	// rendering (entity, escape, removed '\r'…). It may be commented, not edited
	// character by character.
	Transformed
	// Approximate: the range went through a character pandoc may have composed,
	// or falls inside a tab. Neither editing nor precise commenting.
	Approximate
	// Unknown: no position, several segments, or a position outside the text.
	Unknown
)

func (s Status) String() string {
	return [...]string{"certifié", "transformé", "approximatif", "inconnu"}[s]
}

// Located is a node with its byte range in the source and its status.
type Located struct {
	Node
	Start, End int // byte range [Start, End) when Status < Unknown
	Status     Status
}

// Locate gives every node its byte range and its status.
func Locate(x *Index, nodes []Node) []Located {
	out := make([]Located, len(nodes))
	for i, n := range nodes {
		out[i] = LocateOne(x, n)
	}
	return out
}

// LocateOne gives one node its byte range and its status.
func LocateOne(x *Index, n Node) Located {
	l := Located{Node: n, Status: Unknown}
	if len(n.Ranges) != 1 {
		return l
	}
	start, end, exact, err := x.Span(n.Ranges[0])
	if err != nil {
		return l
	}
	l.Start, l.End = start, end
	switch {
	case !exact:
		l.Status = Approximate
	case n.Kind == KindStr && string(x.src[start:end]) == n.Text:
		l.Status = Certified
	case n.Kind == KindSpace && end > start && onlyBlanks(x.src[start:end]):
		l.Status = Certified
	default:
		l.Status = Transformed
	}
	return l
}

func onlyBlanks(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}
