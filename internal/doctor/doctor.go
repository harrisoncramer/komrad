package doctor

import (
	"context"
	"errors"
	"fmt"
)

var ErrCheckPanicked = &Problem{
	Code:    "check_panicked",
	Message: "this check could not be made because it failed on its own",
}

type Status string

const (
	StatusOK Status = "ok"

	StatusWarn Status = "warn"

	StatusFail Status = "fail"

	StatusSkip Status = "skip"
)

type Problem struct {
	Code    string
	Message string
}

func (p *Problem) Error() string { return p.Message }

// CodeOf returns the stable code of the problem err carries, or empty when it carries none;
// a check wraps its sentinel to add detail, so the code is found through the chain rather
// than off the error itself.
func CodeOf(err error) string {
	var problem *Problem
	if errors.As(err, &problem) {
		return problem.Code
	}

	return ""
}

type Check struct {
	Name string

	Description string

	Needs []string

	Run func(context.Context, Env) Result
}

type Result struct {
	Status Status

	Detail string

	Err error

	Fix string
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

func skip(blocker string) Result {
	return Result{Status: StatusSkip, Detail: "not checked, " + blocker + " has to pass first"}
}

type Finding struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	Detail      string `json:"detail,omitempty"`

	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
	Fix   string `json:"fix,omitempty"`
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

// Run makes every check in order and collects the findings; one failing does not stop the
// rest, so a machine missing three things says so once rather than over three runs.
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
			finding.Code = CodeOf(result.Err)
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
			result = fail(fmt.Errorf("%w: %v", ErrCheckPanicked, recovered), "", "")
		}
	}()

	for _, needed := range check.Needs {
		switch c[needed] {
		case StatusFail, StatusSkip:
			return skip(needed)
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
