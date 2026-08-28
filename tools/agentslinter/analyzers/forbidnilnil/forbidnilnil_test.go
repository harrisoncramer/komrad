package forbidnilnil_test

import (
	"testing"

	"agentslinter/analyzers/forbidnilnil"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

func TestInvalidNilNilReturns(t *testing.T) {
	analyzer := forbidnilnil.NewAnalyzer(config.ForbidNilNilConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

func TestValidNilNilReturns(t *testing.T) {
	analyzer := forbidnilnil.NewAnalyzer(config.ForbidNilNilConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}
