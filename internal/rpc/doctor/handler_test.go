package doctor

import (
	"context"
	"fmt"
	"testing"

	"komrade/internal/doctor"
	doctorv1 "komrade/proto/gen/doctor/v1"
	"komrade/proto/gen/doctor/v1/doctorv1komrade"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestRunDoctorRendersTheReport pins how each finding reaches the wire.
func TestRunDoctorRendersTheReport(t *testing.T) {
	t.Parallel()

	checks := []doctor.Check{
		{
			Name:        "base",
			Description: "the thing others need",
			Run: func(context.Context, doctor.Env) doctor.Result {
				return doctor.Result{
					Status: doctor.StatusFail,
					Err:    &doctor.ProblemError{Code: "gone", Message: "it is not there"},
					Fix:    "install it",
				}
			},
		},
		{
			Name:        "fine",
			Description: "stands alone",
			Run: func(context.Context, doctor.Env) doctor.Result {
				return doctor.Result{
					Status: doctor.StatusOK,
					Detail: "3.5a",
				}
			},
		},
	}
	service := doctorv1komrade.DoctorServiceAdapter{Impl: NewHandler(NewHandlerParams{Checks: checks})}

	resp, err := service.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	want := &doctorv1.RunDoctorResponse{
		Findings: []*doctorv1.Finding{
			{
				Name:        "base",
				Description: "the thing others need",
				Status:      "fail",
				Code:        "gone",
				Error:       "it is not there",
				Fix:         "install it",
			},
			{
				Name:        "fine",
				Description: "stands alone",
				Status:      "ok",
				Detail:      "3.5a",
			},
		},
	}
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform(), protocmp.IgnoreFields(&doctorv1.RunDoctorResponse{}, "summary")))
}

// TestRunDoctorRendersTheSummary pins that each count of the summary reaches its own field.
func TestRunDoctorRendersTheSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		counts map[doctor.Status]int
		want   *doctorv1.Summary
	}{
		{
			name: "a failure makes the machine unhealthy",
			counts: map[doctor.Status]int{
				doctor.StatusFail: 1,
				doctor.StatusSkip: 2,
				doctor.StatusWarn: 3,
				doctor.StatusOK:   4,
			},
			want: &doctorv1.Summary{
				Fail:  1,
				Skip:  2,
				Warn:  3,
				Ok:    4,
				Total: 10,
			},
		},
		{
			name: "no failure leaves it healthy",
			counts: map[doctor.Status]int{
				doctor.StatusWarn: 1,
				doctor.StatusOK:   2,
			},
			want: &doctorv1.Summary{
				Warn:    1,
				Ok:      2,
				Total:   3,
				Healthy: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var checks []doctor.Check
			for status, count := range tt.counts {
				for range count {
					checks = append(checks, doctor.Check{
						Name: fmt.Sprintf("%s-%d", status, len(checks)),
						Run: func(context.Context, doctor.Env) doctor.Result {
							return doctor.Result{Status: status}
						},
					})
				}
			}
			service := doctorv1komrade.DoctorServiceAdapter{Impl: NewHandler(NewHandlerParams{Checks: checks})}

			resp, err := service.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)

			assert.Empty(t, cmp.Diff(tt.want, resp.Msg.GetSummary(), protocmp.Transform()))
		})
	}
}
