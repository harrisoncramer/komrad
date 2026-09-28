//go:build integration

package e2e

import (
	"encoding/json"
	"testing"

	"komrad/internal/interceptors"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	logv1 "komrad/proto/gen/log/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationLogReadsTheServersOwnLog covers the path a person takes after a call fails: the error
// carries a trace id, the listing names that trace, and reading it back through the service
// gives the failure line with the method on it.
func TestIntegrationLogReadsTheServersOwnLog(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{KeepLog: true})
	ctx := t.Context()

	_, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: "00000000-0000-0000-0000-000000000000"}))
	require.Error(t, err)

	var failed *connect.Error
	require.ErrorAs(t, err, &failed)
	trace := failed.Meta().Get(interceptors.TraceHeader)
	require.NotEmpty(t, trace, "a failed call answers with its trace id")

	recent, err := instance.Logs.ListRecentTraces(ctx, connect.NewRequest(&logv1.ListRecentTracesRequest{}))
	require.NoError(t, err)

	var listed *logv1.TraceSummary
	for _, summary := range recent.Msg.GetTraces() {
		if summary.GetTraceId() == trace {
			listed = summary
		}
	}
	require.NotNil(t, listed, "the failed call is among the recent traces")
	assert.Equal(t, "GetAgent", listed.GetWhat())

	lines, err := instance.Logs.GetLogLines(ctx, connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: trace}))
	require.NoError(t, err)
	require.NotEmpty(t, lines.Msg.GetLines())

	last := lines.Msg.GetLines()[len(lines.Msg.GetLines())-1]
	assert.Equal(t, "rpc failed", last.GetMessage())
	assert.Equal(t, "GetAgent", last.GetFields().AsMap()["rpc.method"])

	_, err = instance.Logs.GetLogLines(ctx, connect.NewRequest(&logv1.GetLogLinesRequest{TraceId: "no-such-trace"}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestIntegrationLogReadsWhatWasLoggedAboutAgents covers an orchestrator reading its children's
// lines at once, then reading back only what was written since.
func TestIntegrationLogReadsWhatWasLoggedAboutAgents(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{KeepLog: true})
	ctx := t.Context()

	workflowID := createWorkflow(t, instance, "checkout")
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")
	bystander := createAgent(t, instance, workflowID, "bystander")

	logAbout := func(agentID, message string) {
		t.Helper()
		_, err := instance.Agents.Log(ctx, connect.NewRequest(&agentv1.LogRequest{Id: &agentID, Message: message}))
		require.NoError(t, err)
	}
	messages := func(lines []string) []string {
		t.Helper()
		var bodies []string
		for _, line := range lines {
			var fields map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &fields))
			bodies = append(bodies, fields["body"].(string))
		}

		return bodies
	}

	logAbout(api.GetId(), "api started the refund column")
	logAbout(bystander.GetId(), "bystander did something else")
	logAbout(migrations.GetId(), "migrations wrote the migration")

	ids := []string{api.GetId(), migrations.GetId()}
	first, err := instance.Logs.ReadAgentLogs(ctx, connect.NewRequest(&logv1.ReadAgentLogsRequest{AgentIds: ids}))
	require.NoError(t, err)
	assert.Equal(t, []string{"api started the refund column", "migrations wrote the migration"}, messages(first.Msg.GetLines()))
	assert.False(t, first.Msg.GetReset_())

	logAbout(bystander.GetId(), "bystander again")
	logAbout(api.GetId(), "api finished")

	since, err := instance.Logs.ReadAgentLogs(ctx, connect.NewRequest(&logv1.ReadAgentLogsRequest{AgentIds: ids, Cursor: first.Msg.GetCursor()}))
	require.NoError(t, err)
	assert.Equal(t, []string{"api finished"}, messages(since.Msg.GetLines()))
}
