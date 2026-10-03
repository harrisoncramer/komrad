package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestErrorText covers Contains and Equal from assert and require reading an error's text, beside
// checks that read it as the expected value, NotContains, an Error that is not error's, and a
// method on Assertions.
func TestErrorText(t *testing.T) {
	analyzer, err := newErrorText(config.ErrorTextConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "errortext")
}

// TestErrorTextExclude covers a file the settings leave alone.
func TestErrorTextExclude(t *testing.T) {
	analyzer, err := newErrorText(config.ErrorTextConfig{Exclude: []string{`errortextskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "errortextskip")
}

// TestErrorTextSkip covers the rule turned off, which reports nothing.
func TestErrorTextSkip(t *testing.T) {
	analyzer, err := newErrorText(config.ErrorTextConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "errortextskip")
}
