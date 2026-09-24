package doctor

import (
	"context"
	"testing"

	"weave/internal/doctor"
	doctorv1 "weave/proto/gen/doctor/v1"
	"weave/proto/gen/doctor/v1/doctorv1weave"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestRunDoctorRendersTheReport pins how each finding and the summary reach the wire.
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
	service := doctorv1weave.DoctorServiceAdapter{Impl: NewHandler(NewHandlerParams{Checks: checks})}

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
		Summary: &doctorv1.Summary{
			Ok:    1,
			Fail:  1,
			Total: 2,
		},
	}
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform()))
}
