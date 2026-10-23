package panel

import (
	"testing"
	"time"

	"komrad/internal/assemble"
	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestTaskFromCarriesTheCheckout pins the branch, the diff stat and the removal of a task's
// checkout surviving the conversion.
func TestTaskFromCarriesTheCheckout(t *testing.T) {
	branch := "fix/thing"
	converted := taskFrom(assemble.TaskRow{Task: &taskv1.Task{
		Id:                    "t1",
		Branch:                &branch,
		DiffStat:              &taskv1.DiffStat{Added: 3, Removed: 1},
		WorktreeRemovalReason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
		Snapshots:             2,
	}})

	require.NotNil(t, converted.Branch)
	assert.Equal(t, branch, *converted.Branch)
	assert.Equal(t, int32(3), converted.DiffStat.GetAdded())
	assert.Equal(t, int32(1), converted.DiffStat.GetRemoved())
	assert.Equal(t, taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE, converted.WorktreeRemovalReason)
	assert.Equal(t, int32(2), converted.Snapshots)
}

// TestProtoToAgentCarriesAFailedTurn pins the text a turn an API error ended is rendered with.
func TestProtoToAgentCarriesAFailedTurn(t *testing.T) {
	assert.Equal(t, "Overloaded", agentFrom(&agentv1.Agent{Id: "a1", TurnError: "Overloaded"}).TurnError)
}

// TestProtoToAgentCarriesTheStateChange pins the timestamp the row's state is dated by.
func TestProtoToAgentCarriesTheStateChange(t *testing.T) {
	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)
	converted := agentFrom(&agentv1.Agent{
		Id:             "a1",
		StateChangedAt: timestamppb.New(changed),
	})

	assert.True(t, converted.StateChangedAt.Equal(changed))
}

// TestProtoToAgentWithoutAStateChange covers a server that sent no timestamp, which leaves the
// zero time rather than an instant nothing dated it from.
func TestProtoToAgentWithoutAStateChange(t *testing.T) {
	assert.True(t, agentFrom(&agentv1.Agent{Id: "a1"}).StateChangedAt.IsZero())
}

// TestWorkflowsFromCarriesTheWholeWorkflow covers the listing conversion field by field.
func TestWorkflowsFromCarriesTheWholeWorkflow(t *testing.T) {
	got := workflowsFrom([]*workflowv1.Workflow{{
		Id:              "w1",
		Name:            "api",
		Context:         "shared notes",
		Harness:         "claude",
		Cwd:             "/work/api",
		Flags:           "--model opus",
		ReviewerHarness: "codex",
		ReviewerFlags:   "--model gpt-5",
		Worktree: &workflowv1.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			SetupCommand: "mise install",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
		Hooks: []*workflowv1.WorkflowHook{
			{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "install-deps"},
			{Event: eventv1.Moment_MOMENT_AGENT_POST_LAUNCH, Script: "announce"},
		},
		EventPrompts: []*workflowv1.WorkflowEventPrompt{{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "open the pull request"}, {Event: eventv1.Event_EVENT_PULL_REQUEST_BEHIND, Muted: true}},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, Workflow{
		ID:              "w1",
		Name:            "api",
		Context:         "shared notes",
		Harness:         "claude",
		Cwd:             "/work/api",
		Flags:           "--model opus",
		ReviewerHarness: "codex",
		ReviewerFlags:   "--model gpt-5",
		Hooks: []Hook{
			{Moment: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "install-deps"},
			{Moment: eventv1.Moment_MOMENT_AGENT_POST_LAUNCH, Script: "announce"},
		},
		EventPrompts: []EventPrompt{{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "open the pull request"}, {Event: eventv1.Event_EVENT_PULL_REQUEST_BEHIND, Muted: true}},
		Worktree: WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			SetupCommand: "mise install",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
	}, got[0])
}

// TestWorktreeFateIsReadOffTheWire covers each fate the server names landing on the item, and
// one it does not name reading as unowned.
func TestWorktreeFateIsReadOffTheWire(t *testing.T) {
	for wire, want := range map[worktreev1.WorktreeFate]WorktreeFate{
		worktreev1.WorktreeFate_WORKTREE_FATE_LIVE:        FateLive,
		worktreev1.WorktreeFate_WORKTREE_FATE_DELETING:    FateDeleting,
		worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED:    FateArchived,
		worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED:     FateUnowned,
		worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED: FateUnowned,
	} {
		item := WorktreeFrom(&worktreev1.Worktree{Dir: "/trees/a", Fate: wire})
		assert.Equal(t, want, item.Fate, wire.String())
	}
}
