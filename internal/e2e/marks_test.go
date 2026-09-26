//go:build integration

package e2e

import (
	"testing"

	"komrad/internal/komradtest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestIntegrationMarks walks the letters a person types to jump to a row, from claiming one to
// losing it with the row.
func TestIntegrationMarks(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflowID := createWorkflow(t, instance, "checkout")

	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	t.Run("a letter goes on an agent", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(api.GetId()), Letter: xptr.Ptr("a")}))

		assert.Equal(t, map[string]string{"a": api.GetId()}, listMarks(t, instance))
	})

	t.Run("claiming a held letter takes it off the row that had it", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(migrations.GetId()), Letter: xptr.Ptr("a")}))

		assert.Equal(t, map[string]string{"a": migrations.GetId()}, listMarks(t, instance))
	})

	t.Run("a row answers to one letter at a time", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(migrations.GetId()), Letter: xptr.Ptr("m")}))

		assert.Equal(t, map[string]string{"m": migrations.GetId()}, listMarks(t, instance))
	})

	t.Run("a letter goes on a workflow", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newWorkflowTarget(workflowID), Letter: xptr.Ptr("w")}))

		assert.Equal(t, map[string]string{
			"m": migrations.GetId(),
			"w": workflowID,
		}, listMarks(t, instance))
	})

	t.Run("leaving the letter out clears the row", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(migrations.GetId())}))

		assert.Equal(t, map[string]string{"w": workflowID}, listMarks(t, instance))
	})

	t.Run("archiving an agent frees its letter", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(api.GetId()), Letter: xptr.Ptr("a")}))

		archiveAgent(t, instance, api.GetId())

		assert.Equal(t, map[string]string{"w": workflowID}, listMarks(t, instance))
	})

	t.Run("an archived agent cannot be marked", func(t *testing.T) {
		err := setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(api.GetId()), Letter: xptr.Ptr("a")})
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
		assert.Equal(t, map[string]string{"w": workflowID}, listMarks(t, instance))
	})

	t.Run("archiving a workflow frees its letter and its agents' letters", func(t *testing.T) {
		require.NoError(t, setMark(t, instance, &agentv1.SetMarkRequest{Target: newAgentTarget(migrations.GetId()), Letter: xptr.Ptr("m")}))

		_, err := instance.Workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{
			Id: workflowID,
		}))
		require.NoError(t, err)

		assert.Empty(t, listMarks(t, instance))
	})
}

func newAgentTarget(id string) *agentv1.SetMarkRequest_AgentId {
	return &agentv1.SetMarkRequest_AgentId{AgentId: id}
}

func newWorkflowTarget(id string) *agentv1.SetMarkRequest_WorkflowId {
	return &agentv1.SetMarkRequest_WorkflowId{WorkflowId: id}
}

func setMark(t *testing.T, instance *komradtest.Instance, req *agentv1.SetMarkRequest) error {
	t.Helper()

	_, err := instance.Agents.SetMark(t.Context(), connect.NewRequest(req))

	return err
}

// listMarks is every mark by letter, each naming the agent or the workflow it jumps to.
func listMarks(t *testing.T, instance *komradtest.Instance) map[string]string {
	t.Helper()

	listed, err := instance.Agents.ListMarks(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	marks := map[string]string{}
	for _, mark := range listed.Msg.GetMarks() {
		marks[mark.GetLetter()] = mark.GetAgentId() + mark.GetWorkflowId()
	}

	return marks
}
