package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/doctor"
	"komrad/internal/fail"

	"charm.land/huh/v2"
)

// Checker runs the doctor's checks with these names and reports what they found.
type Checker func(ctx context.Context, names ...string) doctor.Report

// Verdict is what a step makes of what the doctor found.
type Verdict int

const (
	VerdictDone Verdict = iota
	VerdictAsk
	VerdictRepair
	VerdictStop
)

// Observation is one look at the machine by a step.
type Observation struct {
	Verdict Verdict
	Detail  string
	Finding *doctor.Finding
}

// Step is one thing setup makes true.
type Step interface {
	name() string
	observe(ctx context.Context, check Checker) Observation
	question() *huh.Form
	answerUnattended() error
	repair(ctx context.Context) error
}

// ErrSkipped is what a repair returns when the person chose to leave the step as it is.
var ErrSkipped = fail.Precondition("skipped")

// ErrNeedsAnswer is what an unattended step returns when a flag has to answer for the person.
var ErrNeedsAnswer = fail.InvalidArgument("needs an answer")

func firstFailing(report doctor.Report) *doctor.Finding {
	for i := range report.Findings {
		if report.Findings[i].Status == doctor.StatusFail {
			return &report.Findings[i]
		}
	}

	return nil
}

func findingNamed(report doctor.Report, name string) doctor.Finding {
	for _, finding := range report.Findings {
		if finding.Name == name {
			return finding
		}
	}

	return doctor.Finding{}
}

func stopOn(finding *doctor.Finding) Observation {
	return Observation{
		Verdict: VerdictStop,
		Detail:  finding.Error,
		Finding: finding,
	}
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}

	return path
}
