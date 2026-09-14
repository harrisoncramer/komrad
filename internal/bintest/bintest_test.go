package bintest

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDecideReadsThePathAndTheVariable covers the decision the suite's green result now rests on.
func TestDecideReadsThePathAndTheVariable(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		binary       string
		variable     string
		wantMissing  bool
		wantRequired bool
	}{
		{name: "a binary that is there", binary: os.Args[0]},
		{name: "a binary that is there under the variable", binary: os.Args[0], variable: "1", wantRequired: true},
		{name: "a binary that is not", binary: "weave-no-such-binary", wantMissing: true},
		{name: "a binary that is not under the variable", binary: "weave-no-such-binary", variable: "1", wantMissing: true, wantRequired: true},
		{name: "a binary that is not under a false-looking variable", binary: "weave-no-such-binary", variable: "0", wantMissing: true, wantRequired: true},
		{name: "a binary that is not under an empty variable", binary: "weave-no-such-binary", variable: "", wantMissing: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(RequireVariable, testCase.variable)

			missing, required := decide(testCase.binary)
			assert.Equal(t, testCase.wantMissing, missing)
			assert.Equal(t, testCase.wantRequired, required)
		})
	}
}
