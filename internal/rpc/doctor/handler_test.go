package doctor

import (
	"context"
	"fmt"
	"testing"

	"komrad/internal/doctor"
	doctorv1 "komrad/proto/gen/doctor/v1"
	"komrad/proto/gen/doctor/v1/doctorv1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

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
			service := doctorv1komrad.DoctorServiceAdapter{Impl: NewHandler(NewHandlerParams{Checks: checks})}

			resp, err := service.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)

			assert.Empty(t, cmp.Diff(tt.want, resp.Msg.GetSummary(), protocmp.Transform()))
		})
	}
}

// TestRunDoctorRendersEachStatus pins that each status a check concludes with reaches the finding as its own value.
func TestRunDoctorRendersEachStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status doctor.Status
		want   doctorv1.Status
	}{
		{status: doctor.StatusOK, want: doctorv1.Status_STATUS_OK},
		{status: doctor.StatusWarn, want: doctorv1.Status_STATUS_WARN},
		{status: doctor.StatusFail, want: doctorv1.Status_STATUS_FAIL},
		{status: doctor.StatusSkip, want: doctorv1.Status_STATUS_SKIP},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			checks := []doctor.Check{{
				Name: "check",
				Run: func(context.Context, doctor.Env) doctor.Result {
					return doctor.Result{Status: tt.status}
				},
			}}
			service := doctorv1komrad.DoctorServiceAdapter{Impl: NewHandler(NewHandlerParams{Checks: checks})}

			resp, err := service.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)

			require.Len(t, resp.Msg.GetFindings(), 1)
			assert.Equal(t, tt.want, resp.Msg.GetFindings()[0].GetStatus())
		})
	}
}
