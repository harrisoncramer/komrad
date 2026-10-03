package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCtxSleep covers sleeps on time.After and on a timer, in either order and inside a loop,
// beside selects that do work when the delay passes, wait on another channel, have a third arm,
// tick, or keep the value they received.
func TestCtxSleep(t *testing.T) {
	analyzer, err := newCtxSleep(config.CtxSleepConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "sleep")
}

// TestCtxSleepExclude covers a file the settings leave alone.
func TestCtxSleepExclude(t *testing.T) {
	analyzer, err := newCtxSleep(config.CtxSleepConfig{Exclude: []string{`sleepskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "sleepskip")
}

// TestCtxSleepSkip covers the rule turned off, which reports nothing.
func TestCtxSleepSkip(t *testing.T) {
	analyzer, err := newCtxSleep(config.CtxSleepConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "sleepskip")
}
