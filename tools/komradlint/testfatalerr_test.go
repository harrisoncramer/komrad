package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestTestFatalErr covers errors handed to Fatal, Fatalf, Error and Errorf on a T, a B, a TB and a
// T held on a struct, beside failures that carry no error and methods outside the testing package.
func TestTestFatalErr(t *testing.T) {
	analyzer, err := newTestFatalErr(config.FatalErrConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "fatal")
}

// TestTestFatalErrPackages covers a package the settings do not cover.
func TestTestFatalErrPackages(t *testing.T) {
	analyzer, err := newTestFatalErr(config.FatalErrConfig{Packages: []string{`^komrad/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "fatalskip")
}

// TestTestFatalErrExclude covers a file the settings leave alone.
func TestTestFatalErrExclude(t *testing.T) {
	analyzer, err := newTestFatalErr(config.FatalErrConfig{Exclude: []string{`fatalskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "fatalskip")
}

// TestTestFatalErrSkip covers the rule turned off, which reports nothing.
func TestTestFatalErrSkip(t *testing.T) {
	analyzer, err := newTestFatalErr(config.FatalErrConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "fatalskip")
}
