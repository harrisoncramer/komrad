package doctor

import (
	"context"
	"errors"
	"testing"

	"weave/internal/doctor"
	"weave/proto/gen/doctor/v1/doctorv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestRunDoctorCarriesEveryFinding covers the translation from the doctor's report to the
// wire: each finding keeps its status, code, error and fix, a skipped one says what it
// needed, and the summary counts them.
func TestRunDoctorCarriesEveryFinding(t *testing.T) {
	t.Parallel()

	problem := &doctor.ProblemError{Code: "gone", Message: "it is not there"}
	checks := []doctor.Check{
		{
			Name:        "base",
			Description: "the thing others need",
			Run: func(context.Context, doctor.Env) doctor.Result {
				return doctor.Result{
					Status: doctor.StatusFail,
					Err:    errors.Join(problem),
					Fix:    "install it",
				}
			},
		},
		{
			Name:        "dependent",
			Description: "needs base",
			Needs:       []string{"base"},
			Run: func(context.Context, doctor.Env) doctor.Result {
				return doctor.Result{Status: doctor.StatusOK, Detail: "never reached"}
			},
		},
		{
			Name:        "fine",
			Description: "stands alone",
			Run: func(context.Context, doctor.Env) doctor.Result {
				return doctor.Result{Status: doctor.StatusOK, Detail: "3.5a"}
			},
		},
	}
	service := doctorv1weave.DoctorServiceAdapter{Impl: NewHandler(NewHandlerParams{Checks: checks})}

	resp, err := service.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetFindings(), 3)

	base := resp.Msg.GetFindings()[0]
	assert.Equal(t, "base", base.GetName())
	assert.Equal(t, "fail", base.GetStatus())
	assert.Equal(t, "gone", base.GetCode())
	assert.Equal(t, "it is not there", base.GetError())
	assert.Equal(t, "install it", base.GetFix())

	dependent := resp.Msg.GetFindings()[1]
	assert.Equal(t, "skip", dependent.GetStatus())
	assert.Contains(t, dependent.GetDetail(), "base has to pass first")

	fine := resp.Msg.GetFindings()[2]
	assert.Equal(t, "ok", fine.GetStatus())
	assert.Equal(t, "3.5a", fine.GetDetail())

	summary := resp.Msg.GetSummary()
	assert.Equal(t, int32(3), summary.GetTotal())
	assert.Equal(t, int32(1), summary.GetOk())
	assert.Equal(t, int32(1), summary.GetFail())
	assert.Equal(t, int32(1), summary.GetSkip())
	assert.False(t, summary.GetHealthy())
}
