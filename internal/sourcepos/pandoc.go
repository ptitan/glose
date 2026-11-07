package sourcepos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Reader is the pandoc input format that attaches source positions to every
// block and inline element.
const Reader = "commonmark_x+sourcepos"

// Kind classifies a positioned element of the pandoc tree.
type Kind int

const (
	KindStr    Kind = iota // a word: one Str inline
	KindSpace              // one Space inline
	KindBreak              // SoftBreak or LineBreak
	KindInline             // any other inline (Strong, Emph, Math, Note, Link…)
	KindBlock              // a block (Div around a paragraph, Header, CodeBlock…)
)

// Node is an element of the pandoc tree that carries a data-pos attribute.
type Node struct {
	Kind   Kind
	Type   string  // pandoc element type of the content ("Str", "Strong", "Para"…)
	Text   string  // for KindStr: the rendered text
	Ranges []Range // source ranges; empty when pandoc gave no position
	Parent int     // index of the nearest positioned ancestor, or -1
}

// Run calls pandoc (without a shell) on src and returns its JSON tree.
// pandocPath may be "pandoc" to use the PATH.
func Run(ctx context.Context, pandocPath string, src []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, pandocPath, "-f", Reader, "-t", "json")
	cmd.Stdin = bytes.NewReader(src)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pandoc : %v : %s", err, bytes.TrimSpace(errOut.Bytes()))
	}
	return out.Bytes(), nil
}

// Nodes walks a pandoc JSON tree and returns, in document order, every
// element that carries a data-pos attribute.
func Nodes(tree []byte) ([]Node, error) {
	var doc any
	if err := json.Unmarshal(tree, &doc); err != nil {
		return nil, fmt.Errorf("arbre pandoc illisible : %w", err)
	}
	var out []Node
	var walkErr error
	var walk func(v any, parent int)
	walk = func(v any, parent int) {
		if walkErr != nil {
			return
		}
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				walk(e, parent)
			}
		case map[string]any:
			if n, ok, err := NodeOf(x); err != nil {
				walkErr = err
				return
			} else if ok {
				n.Parent = parent
				out = append(out, n)
				parent = len(out) - 1
			}
			walk(x["c"], parent)
		}
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("arbre pandoc sans racine")
	}
	walk(root["blocks"], -1) // only the body: metadata has no source positions
	return out, walkErr
}

// inlineWithAttr are the inline elements that carry their own attributes,
// where pandoc puts the position directly instead of wrapping them in a Span.
var inlineWithAttr = map[string]bool{"Link": true, "Image": true, "Code": true}

// NodeOf recognises the elements that carry a data-pos attribute: inline Span
// wrappers added by sourcepos, and blocks whose Attr holds the position.
func NodeOf(el map[string]any) (Node, bool, error) {
	t, _ := el["t"].(string)
	c, _ := el["c"].([]any)
	if t == "" || len(c) == 0 {
		return Node{}, false, nil
	}
	at := 0
	if t == "Header" { // Header: [level, attr, inlines]
		at = 1
	}
	if len(c) <= at {
		return Node{}, false, nil
	}
	attr, ok := c[at].([]any)
	if !ok || len(attr) != 3 {
		return Node{}, false, nil
	}
	pos, found := dataPos(attr)
	if !found {
		return Node{}, false, nil
	}
	ranges, err := ParseDataPos(pos)
	if err != nil {
		return Node{}, false, err
	}
	if t != "Span" {
		kind := KindBlock
		if inlineWithAttr[t] {
			kind = KindInline
		}
		return Node{Kind: kind, Type: t, Ranges: ranges}, true, nil
	}
	var inl []any
	if len(c) > 1 {
		inl, _ = c[1].([]any)
	}
	n := Node{Kind: KindInline, Type: "Span", Ranges: ranges}
	if len(inl) == 1 {
		if e, ok := inl[0].(map[string]any); ok {
			n.Type, _ = e["t"].(string)
			switch n.Type {
			case "Str":
				n.Kind = KindStr
				n.Text, _ = e["c"].(string)
			case "Space":
				n.Kind = KindSpace
			case "SoftBreak", "LineBreak":
				n.Kind = KindBreak
			}
		}
	}
	return n, true, nil
}

func dataPos(attr []any) (string, bool) {
	kvs, _ := attr[2].([]any)
	for _, kv := range kvs {
		pair, ok := kv.([]any)
		if ok && len(pair) == 2 && pair[0] == "data-pos" {
			s, ok := pair[1].(string)
			return s, ok
		}
	}
	return "", false
}
