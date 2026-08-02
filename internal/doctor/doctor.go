// Package doctor asserts the dependencies weave has outside itself and reports what it
// found, one result per assertion. It repairs nothing and writes nothing, so it stays
// safe to run against a machine that is already broken.
package doctor

import (
	"context"
	"errors"
	"fmt"
)

// Status is what a check concluded.
type Status string

const (
	// StatusOK means the dependency is present and usable.
	StatusOK Status = "ok"
	// StatusWarn is a fact worth knowing that does not stop the panel from working.
	StatusWarn Status = "warn"
	// StatusFail means something will not work until it is fixed.
	StatusFail Status = "fail"
	// StatusSkip means the check could not be made, because something it needed
	// failed first. It is not a pass and not a second failure.
	StatusSkip Status = "skip"
)

// Problem is a named way a check can fail. The code is stable and is what a script
// switches on, so it is part of the interface and does not change with the wording.
type Problem struct {
	Code    string
	Message string
}

func (p *Problem) Error() string { return p.Message }

// CodeOf returns the stable code of the problem err carries, or empty when it carries
// none. A check wraps its sentinel to add detail, so the code is found through the chain
// rather than off the error itself.
func CodeOf(err error) string {
	var problem *Problem
	if errors.As(err, &problem) {
		return problem.Code
	}

	return ""
}

// Check is one assertion about the machine.
type Check struct {
	// Name is what the check is called, short and stable enough to grep a report for.
	Name string
	// Description says what the dependency is for, in terms of what stops working
	// without it, since that is what tells a reader whether to care.
	Description string
	// Needs names the checks that have to pass before this one can be made at all.
	// One of them failing skips this rather than running it, so a machine with no
	// Postgres reports that once instead of once per thing that needed it.
	Needs []string
	// Run makes the assertion. It is given the env so a test can answer for a
	// dependency the machine running the test does not have.
	Run func(context.Context, Env) Result
}

// Result is what a check found.
type Result struct {
	Status Status
	// Detail is what was actually observed: the version, the resolved path, the
	// address. It is filled in on success too, since half of what doctor is for is
	// saying which tmux and which database it found.
	Detail string
	// Err is the problem the check failed with, nil when it did not.
	Err error
	// Fix is the command that would repair it, empty when there is nothing to run.
	Fix string
}

// ok is a passing result carrying what was observed.
func ok(detail string) Result {
	return Result{Status: StatusOK, Detail: detail}
}

// warn is a result worth reporting that stops nothing.
func warn(err error, detail string) Result {
	return Result{Status: StatusWarn, Detail: detail, Err: err}
}

// fail is a result that means something is broken, with the command that repairs it.
func fail(err error, detail, fix string) Result {
	return Result{Status: StatusFail, Detail: detail, Err: err, Fix: fix}
}

// skip is a check that was not made because blocker did not pass.
func skip(blocker string) Result {
	return Result{Status: StatusSkip, Detail: "not checked, " + blocker + " has to pass first"}
}

// Finding is one check and what it found, in the shape the JSON output takes.
type Finding struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	Detail      string `json:"detail,omitempty"`
	// Code is the stable identifier of the problem, which a script keys on. Error is
	// the same problem written for a person.
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
	Fix   string `json:"fix,omitempty"`
}

// Report is every finding and the counts over them.
type Report struct {
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`
}

// Summary is how many checks landed in each status, so a consumer can decide without
// walking the findings.
type Summary struct {
	OK      int  `json:"ok"`
	Warn    int  `json:"warn"`
	Fail    int  `json:"fail"`
	Skip    int  `json:"skip"`
	Total   int  `json:"total"`
	Healthy bool `json:"healthy"`
}

// Run makes every check in order and collects the findings. Checks do not depend on each
// other, so one that fails does not stop the rest: a machine missing three things should
// say so once rather than over three runs.
func Run(ctx context.Context, env Env, checks []Check) Report {
	report := Report{Findings: make([]Finding, 0, len(checks))}

	// What each check concluded, so the ones after it can be skipped rather than run
	// against a dependency already known to be missing.
	concluded := make(map[string]Status, len(checks))

	for _, check := range checks {
		result := runCheck(ctx, env, check, concluded)
		concluded[check.Name] = result.Status

		finding := Finding{
			Name:        check.Name,
			Description: check.Description,
			Status:      result.Status,
			Detail:      result.Detail,
			Fix:         result.Fix,
		}
		if result.Err != nil {
			finding.Code = CodeOf(result.Err)
			finding.Error = result.Err.Error()
		}

		report.Findings = append(report.Findings, finding)
	}

	report.Summary = summarize(report.Findings)

	return report
}

// ErrCheckPanicked reports a check that did not survive being run, which is a bug in the
// check rather than anything about the machine.
var ErrCheckPanicked = &Problem{
	Code:    "check_panicked",
	Message: "this check could not be made because it failed on its own",
}

// runCheck makes one assertion, unless something it needs did not pass. A dependency that
// was itself skipped skips this too, so a chain of them collapses to the one real failure
// at its head.
//
// A check that panics is reported as one bad row rather than taking the process with it.
// The panel runs these to explain why it could not start, so the one moment doctor is
// needed most is the moment it must not be the reason nothing renders.
func runCheck(ctx context.Context, env Env, check Check, concluded map[string]Status) (result Result) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = fail(fmt.Errorf("%w: %v", ErrCheckPanicked, recovered), "", "")
		}
	}()

	for _, needed := range check.Needs {
		switch concluded[needed] {
		case StatusFail, StatusSkip:
			return skip(needed)
		case StatusOK, StatusWarn:
			// A warning is a fact rather than a blocker, so what needed it still runs.
		}
	}

	return check.Run(ctx, env)
}

// summarize counts the findings by status. Healthy means nothing failed; a warning is a
// fact rather than a problem, so it does not make a machine unhealthy.
func summarize(findings []Finding) Summary {
	summary := Summary{Total: len(findings)}

	for _, finding := range findings {
		switch finding.Status {
		case StatusOK:
			summary.OK++
		case StatusWarn:
			summary.Warn++
		case StatusFail:
			summary.Fail++
		case StatusSkip:
			summary.Skip++
		}
	}

	summary.Healthy = summary.Fail == 0

	return summary
}
