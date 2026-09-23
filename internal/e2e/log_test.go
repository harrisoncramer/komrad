//go:build integration

package e2e

import (
	"testing"

	"weave/internal/interceptors"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	logv1 "weave/proto/gen/log/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationLogReadsTheServersOwnLog covers the path a person takes after a call fails: the error
// carries a trace id, the listing names that trace, and reading it back through the service
// gives the failure line with the method on it.
func TestIntegrationLogReadsTheServersOwnLog(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{KeepLog: true})
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
