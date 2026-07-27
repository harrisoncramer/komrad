package forbidgetenv_test

import (
	"testing"

	"agentslinter/analyzers/forbidgetenv"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

func TestInvalidEnvAccess(t *testing.T) {
	analyzer := forbidgetenv.NewAnalyzer(config.ForbidGetenvConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

func TestValidEnvAccess(t *testing.T) {
	analyzer := forbidgetenv.NewAnalyzer(config.ForbidGetenvConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}
