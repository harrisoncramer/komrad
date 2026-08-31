package forbidnilnil_test

import (
	"testing"

	"agentslinter/analyzers/forbidnilnil"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

// TestInvalidNilNilReturns pins that a nil pointer returned alongside a nil error is found.
func TestInvalidNilNilReturns(t *testing.T) {
	analyzer := forbidnilnil.NewAnalyzer(config.ForbidNilNilConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

// TestValidNilNilReturns pins that a nil map or slice returned with a nil error is left
// alone, since it already reads as empty.
func TestValidNilNilReturns(t *testing.T) {
	analyzer := forbidnilnil.NewAnalyzer(config.ForbidNilNilConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}
