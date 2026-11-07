package sourcepos

import "testing"

func TestNodesRecognisesHeaders(t *testing.T) {
	tree := `{"blocks":[{"t":"Header","c":[1,["titre",[],[["data-pos","1:1-2:1"]]],
		[{"t":"Span","c":[["",[],[["data-pos","1:3-1:8"]]],[{"t":"Str","c":"titre"}]]}]]}]}`
	nodes, err := Nodes([]byte(tree))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Kind != KindBlock || nodes[0].Type != "Header" ||
		nodes[1].Kind != KindStr || nodes[1].Text != "titre" || nodes[0].Parent != -1 || nodes[1].Parent != 0 {
		t.Fatalf("nœuds : %+v", nodes)
	}
}

// Malformed trees found by the code review used to panic.
func TestNodesSurvivesMalformedTrees(t *testing.T) {
	for _, tree := range []string{
		`{"blocks":[{"t":"Span","c":[["",[],[["data-pos","1:1-1:2"]]]]}]}`,
		`{"blocks":[{"t":"Header","c":[1]}]}`,
		`{"blocks":[{"t":"Header","c":[]}]}`,
		`{"blocks":[{"t":"Span","c":[["",[],[["data-pos",3]]],[]]}]}`,
		`{"blocks":[{"t":"Span","c":[["",[],[["data-pos"]]],[]]}]}`,
		`{"blocks":[{"t":"Para","c":"pas une liste"}]}`,
		`{"blocks":"rien"}`,
		`[]`,
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panique sur %s : %v", tree, r)
				}
			}()
			Nodes([]byte(tree))
		}()
	}
	if _, err := Nodes([]byte(`{"blocks":[{"t":"Span","c":[["",[],[["data-pos","x"]]],[]]}]}`)); err == nil {
		t.Error("position illisible : erreur attendue")
	}
}
