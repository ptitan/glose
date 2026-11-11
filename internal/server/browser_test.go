package server

import (
	"reflect"
	"testing"
)

// Audit A9: the address keeps its « &f= » through cmd.exe.
func TestCmdStartArgsEscapesTheAddress(t *testing.T) {
	got := cmdStartArgs("http://127.0.0.1:1/#t=abc&f=livre%20(1)")
	want := []string{"/c", "start", "", "http://127.0.0.1:1/#t=abc^&f=livre%20^(1^)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}
