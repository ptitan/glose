package sourcepos

import (
	"reflect"
	"testing"
)

func TestParseDataPos(t *testing.T) {
	cases := []struct {
		in   string
		want []Range
	}{
		{"", nil},
		{"1:3-1:5", []Range{{Pos{1, 3}, Pos{1, 5}}}},
		{"chapitre.md@12:1-13:1", []Range{{Pos{12, 1}, Pos{13, 1}}}},
		{"a@b.md@2:4-2:9", []Range{{Pos{2, 4}, Pos{2, 9}}}},
		{"1:3-2:1;2:3-3:1", []Range{{Pos{1, 3}, Pos{2, 1}}, {Pos{2, 3}, Pos{3, 1}}}},
	}
	for _, c := range cases {
		got, err := ParseDataPos(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseDataPos(%q) = %v, %v ; attendu %v", c.in, got, err, c.want)
		}
	}
}

func TestParseDataPosRejectsMalformedValues(t *testing.T) {
	for _, in := range []string{"1:3", "1-2", "0:1-1:1", "1:0-1:1", "x:1-1:2", "2:1-1:1", "1:5-1:4"} {
		if got, err := ParseDataPos(in); err == nil {
			t.Errorf("ParseDataPos(%q) = %v, erreur attendue", in, got)
		}
	}
}
