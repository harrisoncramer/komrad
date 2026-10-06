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

// checker runs the doctor's checks with these names and reports what they found.
type checker func(ctx context.Context, names ...string) doctor.Report

// verdict is what a step makes of what the doctor found.
type verdict int

const (
	verdictDone verdict = iota
	verdictAsk
	verdictRepair
	verdictStop
	verdictSkip
)

// observation is one look at the machine by a step.
type observation struct {
	Verdict verdict
	Detail  string
	Finding *doctor.Finding
}

// Step is one thing setup makes true.
type Step interface {
	name() string
	observe(ctx context.Context, check checker) observation
	question() *huh.Form
	answerUnattended() error
	repair(ctx context.Context) error
}

// errSkipped is what a repair or an unattended answer returns when the step is left as it is.
var errSkipped = fail.Precondition("skipped")

// errNeedsAnswer is what an unattended step returns when a flag has to answer for the person.
var errNeedsAnswer = fail.InvalidArgument("needs an answer")

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

func stopOn(finding *doctor.Finding) observation {
	return observation{
		Verdict: verdictStop,
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
