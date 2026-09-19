// Package doctor serves the checks the doctor makes, made on the machine the service runs on.
package doctor

import (
	"context"

	"weave/internal/doctor"
	"weave/internal/fail"
	doctorv1 "weave/proto/gen/doctor/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

// NewDoctorServiceParams is the machine the checks are made against and the checks to make.
type NewDoctorServiceParams struct {
	Env    doctor.Env
	Checks []doctor.Check
}

type DoctorService struct {
	env    doctor.Env
	checks []doctor.Check
}

// NewDoctorService constructs the handler over the checks the service makes on its own machine.
func NewDoctorService(params NewDoctorServiceParams) *DoctorService {
	return &DoctorService{env: params.Env, checks: params.Checks}
}

// RunDoctor makes every check and answers with the findings.
func (s *DoctorService) RunDoctor(ctx context.Context, _ *emptypb.Empty) (*doctorv1.RunDoctorResponse, *fail.Error) {
	report := doctor.Run(ctx, s.env, s.checks)

	findings := make([]*doctorv1.Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		findings = append(findings, &doctorv1.Finding{
			Name:        finding.Name,
			Description: finding.Description,
			Status:      string(finding.Status),
			Detail:      finding.Detail,
			Code:        finding.Code,
			Error:       finding.Error,
			Fix:         finding.Fix,
		})
	}

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
