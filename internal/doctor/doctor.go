// Package doctor checks the things komrad needs from the machine around it and reports what is
// wrong in terms a person can act on: the configuration file, the database and its schema, the
// service, tmux and its shell, the directories the hooks and worktree features read and write,
// and the CLI binary each harness runs.
//
// Every one of these fails at a distance from where it is noticed. A missing tmux shell shows
// up as an agent whose pane dies on launch, a schema behind the binary shows up as a query
// against a column that is not there, and a hooks directory that has been moved away shows up
// as a workflow form offering no hooks to hang. The doctor is where those are asked about
// directly.
package doctor

import (
	"context"
	"errors"
	"fmt"
)

var errCheckPanicked = &ProblemError{
	Code:    "check_panicked",
	Message: "this check could not be made because it failed on its own",
}

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

type ProblemError struct {
	Code    string
	Message string
}

// Error returns the message the check wrote.
func (p *ProblemError) Error() string {
	return p.Message
}

// codeOf returns the stable code of the problem err carries, or empty when it carries none,
// looking through the chain because a check wraps its sentinel to add detail.
func codeOf(err error) string {
	var problem *ProblemError
	if errors.As(err, &problem) {
		return problem.Code
	}

	return ""
}

// Check is one thing the doctor looks at, listed under HereChecks, EngineChecks or both.
type Check struct {
	Name        string
	Description string
	// Needs names the checks that have to pass first, each listed before this one wherever it
	// is listed.
	Needs []string
	Run   func(context.Context, Env) Result
}

type Result struct {
	Status Status
	Detail string
	Err    error
	Fix    string
}

func pass(detail string) Result {
	return Result{Status: StatusOK, Detail: detail}
}

func warn(err error, detail string) Result {
	return Result{
		Status: StatusWarn,
		Detail: detail,
		Err:    err,
	}
}

func fail(err error, detail, fix string) Result {
	return Result{
		Status: StatusFail,
		Detail: detail,
		Err:    err,
		Fix:    fix,
	}
}

func skip(reason string) Result {
	return Result{Status: StatusSkip, Detail: "not checked, " + reason}
}

type Finding struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Code        string `json:"code,omitempty"`
	Error       string `json:"error,omitempty"`
	Fix         string `json:"fix,omitempty"`
	// Where names the machine the finding is about when it is not the one the doctor ran on.
	Where string `json:"where,omitempty"`
}

// WhereEngine is what a finding the engine's service made carries in Where.
const WhereEngine = "engine"

var errEngineUnanswered = &ProblemError{
	Code:    "engine_unanswered",
	Message: "the engine did not answer with its checks",
}

const engineDescription = "the checks the engine's service makes on its own machine"

// EngineNotChecked stands in for the engine's checks when the server check did not pass.
func EngineNotChecked() Finding {
	return Finding{
		Name:        "checks",
		Description: engineDescription,
		Status:      StatusSkip,
		Detail:      "not checked, server has to pass first",
		Where:       WhereEngine,
	}
}

// EngineUnanswered stands in for the engine's checks when the service was asked for them and
// the call failed.
func EngineUnanswered(err error) Finding {
	wrapped := fmt.Errorf("%w: %w", errEngineUnanswered, err)

	return Finding{
		Name:        "checks",
		Description: engineDescription,
		Status:      StatusFail,
		Code:        codeOf(wrapped),
		Error:       wrapped.Error(),
		Fix:         "run komrad doctor on the engine, or read the service's log there",
		Where:       WhereEngine,
	}
}

// WithEngine is the report with the findings an engine's service made added after the ones
// made here, each marked as the engine's and counted into the summary.
func (r Report) WithEngine(findings []Finding) Report {
	merged := Report{Findings: make([]Finding, 0, len(r.Findings)+len(findings))}
	merged.Findings = append(merged.Findings, r.Findings...)
	for _, finding := range findings {
		finding.Where = WhereEngine
		merged.Findings = append(merged.Findings, finding)
	}

	merged.Summary = summarize(merged.Findings)

	return merged
}

type Report struct {
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`
}

type Summary struct {
	OK      int  `json:"ok"`
	Warn    int  `json:"warn"`
	Fail    int  `json:"fail"`
	Skip    int  `json:"skip"`
	Total   int  `json:"total"`
	Healthy bool `json:"healthy"`
}

// Run makes every check in order and collects the findings, one failing not stopping the rest.
func Run(ctx context.Context, env Env, checks []Check) Report {
	report := Report{Findings: make([]Finding, 0, len(checks))}

	concluded := make(conclusions, len(checks))

	for _, check := range checks {
		result := concluded.run(ctx, env, check)
		concluded[check.Name] = result.Status

		finding := Finding{
			Name:        check.Name,
			Description: check.Description,
			Status:      result.Status,
			Detail:      result.Detail,
			Fix:         result.Fix,
		}
		if result.Err != nil {
			finding.Code = codeOf(result.Err)
			finding.Error = result.Err.Error()
		}

		report.Findings = append(report.Findings, finding)
	}

	report.Summary = summarize(report.Findings)

	return report
}

type conclusions map[string]Status

func (c conclusions) run(ctx context.Context, env Env, check Check) (result Result) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = fail(fmt.Errorf("%w: %v", errCheckPanicked, recovered), "", "")
		}
	}()

	for _, needed := range check.Needs {
		switch c[needed] {
		case StatusFail, StatusSkip:
			return skip(needed + " has to pass first")
		case StatusOK, StatusWarn:
		}
	}

	return check.Run(ctx, env)
}

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
