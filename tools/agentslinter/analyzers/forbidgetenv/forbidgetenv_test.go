package forbidgetenv_test

import (
	"testing"

	"agentslinter/analyzers/forbidgetenv"
	"agentslinter/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

var testdata = analysistest.TestData()

// TestInvalidEnvAccess pins that a bare read of the process environment is found.
func TestInvalidEnvAccess(t *testing.T) {
	analyzer := forbidgetenv.NewAnalyzer(config.ForbidGetenvConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "invalid")
}

// TestValidEnvAccess pins that reading configuration through the config package is left
// alone.
func TestValidEnvAccess(t *testing.T) {
	analyzer := forbidgetenv.NewAnalyzer(config.ForbidGetenvConfig{Skip: false})
	analysistest.Run(t, testdata, analyzer, "valid")
}
