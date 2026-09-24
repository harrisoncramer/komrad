package blackbox

import "testing"

func TestSortRows(t *testing.T) {
	if len(sortRows([]string{"b", "a"})) != 2 {
		t.Fatal("a row went missing")
	}
}
