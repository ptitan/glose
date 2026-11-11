package project

import "testing"

func TestTaskProfileValidation(t *testing.T) {
	for _, src := range []string{
		`{"tasks":[{"id":"rewrite","name":"Réécrire","icon":"suggerer","prompt":"Relis","place":["bar"],"workdir":"/secret"}]}`,
		`{"tasks":[{"id":"rewrite","name":"Réécrire","icon":"inconnue","prompt":"Relis","place":["bar"]}]}`,
	} {
		if CheckProfile([]byte(src)) == nil {
			t.Fatal("profil accepté", src)
		}
	}
	if err := CheckProfile([]byte(`{"tasks":[{"id":"rewrite","name":"Réécrire","icon":"suggerer","prompt":"Relis","place":["bar"],"workdir":"chapitres"}]}`)); err != nil {
		t.Fatal(err)
	}
}
