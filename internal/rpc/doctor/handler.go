// Package doctor serves the checks the doctor makes, made on the machine the service runs on.
package doctor

import (
	"context"

	"komrad/internal/doctor"
	"komrad/internal/fail"
	"komrad/lib/xslice"
	doctorv1 "komrad/proto/gen/doctor/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

// NewHandlerParams is the machine the checks are made against and the checks to make.
type NewHandlerParams struct {
	Env    doctor.Env
	Checks []doctor.Check
}

type Handler struct {
	env    doctor.Env
	checks []doctor.Check
}

// NewHandler constructs the handler over the checks the service makes on its own machine.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{env: params.Env, checks: params.Checks}
}

// RunDoctor makes every check and answers with the findings.
func (s *Handler) RunDoctor(ctx context.Context, _ *emptypb.Empty) (*doctorv1.RunDoctorResponse, *fail.Error) {
	report := doctor.Run(ctx, s.env, s.checks)

	findings := xslice.Map(report.Findings, func(finding doctor.Finding) *doctorv1.Finding {
		return &doctorv1.Finding{
			Name:        finding.Name,
			Description: finding.Description,
			Status:      string(finding.Status),
			Detail:      finding.Detail,
			Code:        finding.Code,
			Error:       finding.Error,
			Fix:         finding.Fix,
		}
	})

	return &doctorv1.RunDoctorResponse{
		Findings: findings,
		Summary: &doctorv1.Summary{
			Ok:      int32(report.Summary.OK),
			Warn:    int32(report.Summary.Warn),
			Fail:    int32(report.Summary.Fail),
			Skip:    int32(report.Summary.Skip),
			Total:   int32(report.Summary.Total),
			Healthy: report.Summary.Healthy,
		},
	}, nil
}
