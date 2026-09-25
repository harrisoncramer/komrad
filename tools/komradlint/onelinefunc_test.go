package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestOnelineFunc covers a declaration whose body shares a line with its braces beside an
// empty one, a multi-line one, and a literal, which are left alone.
func TestOnelineFunc(t *testing.T) {
	analyzer := newOnelineFunc(config.OnelineFuncConfig{})

	analysistest.Run(t, analysistest.TestData(), analyzer, "oneline")
}

// TestOnelineFuncSkip covers the rule turned off, which reports nothing.
func TestOnelineFuncSkip(t *testing.T) {
	analyzer := newOnelineFunc(config.OnelineFuncConfig{Skip: true})

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrad/quiet")
}
