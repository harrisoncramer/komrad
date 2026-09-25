package blackbox

import "testing"

func TestModel(t *testing.T) {
	panel := newModel() // want `a test inside blackbox drives model; write it in package blackbox_test and drive it through the harness`
	panel.height = 2
	_ = model{}
}
