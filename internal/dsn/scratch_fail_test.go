package dsn

import "testing"

func TestScratchDeliberateFailure(t *testing.T) {
	t.Fatal("deliberate failure proving the gate reports red")
}
