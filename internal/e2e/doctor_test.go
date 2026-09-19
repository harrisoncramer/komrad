//go:build integration

package e2e

import (
	"testing"

	"weave/internal/weavetest"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestRunDoctorChecksTheServersOwnMachine covers what a doctor pointed at an engine reads
// back: the service makes the checks about its machine with what it holds, which is the
// database it opened and the tmux server it runs, and answers every one with a status.
func TestRunDoctorChecksTheServersOwnMachine(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})

	resp, err := instance.Doctor.RunDoctor(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	byName := make(map[string]string, len(resp.Msg.GetFindings()))
	for _, finding := range resp.Msg.GetFindings() {
		assert.Contains(t, []string{"ok", "warn", "fail", "skip"}, finding.GetStatus(), finding.GetName())
		byName[finding.GetName()] = finding.GetStatus()
	}

	assert.Equal(t, "ok", byName["database"], "the service reaches the database it opened")
	assert.Equal(t, "ok", byName["schema"], "the schema is the one this build migrated")
	assert.Equal(t, "ok", byName["tmux"], "the service found the tmux it runs")
	assert.Contains(t, byName, "tmux shell")
	assert.NotContains(t, byName, "server", "the service does not probe itself")
	assert.NotContains(t, byName, "terminal glyphs", "the service has no terminal to measure")
	assert.Equal(t, int32(len(resp.Msg.GetFindings())), resp.Msg.GetSummary().GetTotal())
}
