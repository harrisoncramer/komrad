//go:build integration

package e2e

import (
	"testing"

	"komrad/internal/komradtest"
	doctorv1 "komrad/proto/gen/doctor/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestIntegrationRunDoctorChecksTheServersOwnMachine covers what a doctor pointed at an engine reads back: the
// server makes the checks about its machine with what it holds.
func TestIntegrationRunDoctorChecksTheServersOwnMachine(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})

	resp, err := instance.Doctor.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	byName := make(map[string]doctorv1.Status, len(resp.Msg.GetFindings()))
	for _, finding := range resp.Msg.GetFindings() {
		assert.NotEqual(t, doctorv1.Status_STATUS_UNSPECIFIED, finding.GetStatus(), finding.GetName())
		byName[finding.GetName()] = finding.GetStatus()
	}

	assert.Equal(t, doctorv1.Status_STATUS_OK, byName["database"], "the server reaches the database it opened")
	assert.Equal(t, doctorv1.Status_STATUS_OK, byName["schema"], "the schema is the one this build migrated")
	assert.Equal(t, doctorv1.Status_STATUS_OK, byName["tmux"], "the server found the tmux it runs")
	assert.Contains(t, byName, "tmux shell")
	assert.NotContains(t, byName, "server", "the server does not probe itself")
	assert.NotContains(t, byName, "terminal glyphs", "the server has no terminal to measure")
	assert.Equal(t, int32(len(resp.Msg.GetFindings())), resp.Msg.GetSummary().GetTotal())
}
