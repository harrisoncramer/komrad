package agents_test

import (
	"slices"
	"strings"
	"testing"

	"komrad/internal/ui/paneltest"
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"
	hookv1 "komrad/proto/gen/hook/v1"
	"komrad/proto/gen/hook/v1/hookv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

// TestFormsWorkflowProvisioner covers the workflow form asking only what its provisioner needs.
func TestFormsWorkflowProvisioner(t *testing.T) {
	harness := openForms(t, newFormWorkflow(), newFormAuthor())

	harness.Press(slices.Concat([]string{"space", "w"}, slices.Repeat([]string{"tab"}, 3))...)
	frame := harness.Frame()
	assert.Contains(t, frame, "None: agents keep the directory they are given", "neither option is hidden by the other being picked")
	assert.Less(t, strings.Index(frame, "None: agents keep"), strings.Index(frame, "Worktree: a git worktree per agent"), "and the options keep their order")

	harness.Press(slices.Concat([]string{"tab", "down", "enter", "tab", "tab"}, strings.Split(".env", ""), []string{"esc", "shift+tab", "up", "tab", "enter"})...)
	assert.NotContains(t, harness.Frame(), "Worktree base branch", "none provisions nothing, so its worktree subform does not open")

	harness.Press("shift+tab", "down", "enter")

	sent := harness.Sent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
	require.Len(t, sent, 1)
	create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
	require.True(t, ok)
	assert.Equal(t, []string{".env"}, create.GetWorktree().GetCopyPaths(), "switching back hands back what was typed before")
}

// TestFormsWorkflowSubforms covers drilling into a subform of the workflow form and coming back.
func TestFormsWorkflowSubforms(t *testing.T) {
	t.Run("back keeps what the subform was given and lands on the list of subforms", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"space", "w"}, strings.Split("web", ""), openWorkflowSubformKeys(0), strings.Split("codex", ""), []string{"esc"})...)
		assert.NotContains(t, harness.Frame(), "Default Agent", "the subform closed")

		harness.Press("enter")
		assert.Contains(t, harness.Frame(), "Default Agent", "enter opens the subform the cursor came back to")

		harness.Press("enter")

		sent := harness.Sent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
		require.True(t, ok)
		assert.Equal(t, "codex", create.GetHarness())
	})

	t.Run("back on the top form throws the edit away", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"esc"})...)

		assert.NotContains(t, harness.Frame(), "New workflow name")
		assert.Empty(t, harness.Sent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure))
	})
}

// TestFormsWorkflowHooks covers the hook pickers offering what the hooks directory holds.
func TestFormsWorkflowHooks(t *testing.T) {
	every := []*workflowv1.WorkflowHook{
		{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "install-deps"},
		{Event: eventv1.Moment_MOMENT_AGENT_POST_LAUNCH, Script: "announce"},
		{Event: eventv1.Moment_MOMENT_AGENT_PRE_RESUME, Script: "warm-up"},
		{Event: eventv1.Moment_MOMENT_AGENT_POST_RESUME, Script: "announce-back"},
		{Event: eventv1.Moment_MOMENT_AGENT_PRE_ARCHIVE, Script: "check-in"},
		{Event: eventv1.Moment_MOMENT_AGENT_POST_ARCHIVE, Script: "give-back-the-slot"},
		{Event: eventv1.Moment_MOMENT_AGENT_PRE_RESTORE, Script: "lock"},
		{Event: eventv1.Moment_MOMENT_AGENT_POST_RESTORE, Script: "unlock"},
		{Event: eventv1.Moment_MOMENT_AGENT_PRE_DELETE, Script: "take-the-stack-down"},
		{Event: eventv1.Moment_MOMENT_AGENT_POST_DELETE, Script: "say-goodbye"},
	}

	t.Run("a picker offers every hook and opens on the one the workflow names", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "copy-env"}}, "install-deps", "copy-env")

		frame := harness.Frame()
		assert.Contains(t, frame, "install-deps")
		assert.Contains(t, frame, "› copy-env")
	})

	t.Run("a hook still waiting on a chmod is offered", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: []*hookv1.Hook{{Name: "unrunnable"}}}), nil
		})

		harness.Press(append([]string{"space", "E"}, openWorkflowSubformKeys(2)...)...)

		assert.Contains(t, harness.Frame(), "unrunnable")
	})

	t.Run("a hook the directory no longer holds is still offered", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "copy-env"}}, "install-deps")

		assert.Contains(t, harness.Frame(), "missing")
	})

	t.Run("an empty hooks directory says where hooks go", func(t *testing.T) {
		harness := openHookForm(t, nil)

		assert.Contains(t, harness.Frame(), "none found in your hooks directory")
	})

	t.Run("every moment comes back as it went in", func(t *testing.T) {
		harness := openHookForm(t, every, "install-deps", "announce", "warm-up", "announce-back", "check-in",
			"give-back-the-slot", "lock", "unlock", "take-the-stack-down", "say-goodbye")

		harness.Press("enter")

		assertSentHooks(t, harness, every)
	})

	t.Run("a moment holding two keeps the one that ran first", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{
			{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "install-deps"},
			{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "copy-env"},
		}, "install-deps", "copy-env")

		harness.Press("enter")

		assertSentHooks(t, harness, []*workflowv1.WorkflowHook{{Event: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, Script: "install-deps"}})
	})
}

// TestFormsWorkflowEventPrompts pins the workflow form carrying a prompt for each event.
func TestFormsWorkflowEventPrompts(t *testing.T) {
	sentEventPrompts := func(t *testing.T, harness *paneltest.Panel) []*workflowv1.WorkflowEventPrompt {
		t.Helper()

		sent := harness.Sent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
		require.Len(t, sent, 1)
		update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
		require.True(t, ok)

		return update.GetEventPrompts()
	}

	t.Run("the prompts come back in the order the events are listed", func(t *testing.T) {
		workflow := newFormWorkflow()
		workflow.EventPrompts = []*workflowv1.WorkflowEventPrompt{
			{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "open the pull request"},
			{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Prompt: "fix the checks"},
		}
		harness := openForms(t, workflow, newFormAuthor())

		harness.Press("space", "E", "enter")

		assert.Empty(t, cmp.Diff([]*workflowv1.WorkflowEventPrompt{
			{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Prompt: "fix the checks"},
			{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "open the pull request"},
		}, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("a prompt typed under its event is sent with it", func(t *testing.T) {
		harness := openWideForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(append([]string{"space", "E"}, openWorkflowSubformKeys(3)...)...)
		frame := harness.Frame()
		assert.Contains(t, frame, "when some checks on the head failed while others still run")

		harness.Press(slices.Concat(strings.Split("fix the checks", ""), []string{"enter"})...)

		assert.Empty(t, cmp.Diff([]*workflowv1.WorkflowEventPrompt{
			{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING_RUNNING, Prompt: "fix the checks"},
		}, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("a muted event with no prompt comes back muted", func(t *testing.T) {
		workflow := newFormWorkflow()
		workflow.EventPrompts = []*workflowv1.WorkflowEventPrompt{
			{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Prompt: "fix the checks"},
			{Event: eventv1.Event_EVENT_PULL_REQUEST_BEHIND, Muted: true},
		}
		harness := openForms(t, workflow, newFormAuthor())

		harness.Press("space", "E", "enter")

		assert.Empty(t, cmp.Diff(workflow.EventPrompts, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("muting an event under its prompt sends it muted", func(t *testing.T) {
		harness := openWideForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(append([]string{"space", "E"}, openWorkflowSubformKeys(3)...)...)
		harness.Press(slices.Concat(strings.Split("fix the checks", ""), []string{"tab"})...)
		frame := harness.Frame()
		assert.Contains(t, frame, "komrad sends no mail of its own")

		harness.Press("down", "enter")

		assert.Empty(t, cmp.Diff([]*workflowv1.WorkflowEventPrompt{
			{
				Event:  eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING_RUNNING,
				Prompt: "fix the checks",
				Muted:  true,
			},
		}, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("a new workflow starts with each event's default", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"enter"})...)

		sent := harness.Sent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
		require.True(t, ok)
		require.Len(t, create.GetEventPrompts().GetPrompts(), len(protoenum.Values[eventv1.Event]()))
		assert.Contains(t, create.GetEventPrompts().GetPrompts()[0].GetPrompt(), "Read the failed jobs' logs")
	})
}

func openWideForms(t *testing.T, workflow *workflowv1.Workflow, author *agentv1.Agent) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{Width: 220})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(workflow, author))
	harness.Press("g", "g")

	return harness
}

func openHookForm(t *testing.T, hooks []*workflowv1.WorkflowHook, available ...string) *paneltest.Panel {
	t.Helper()

	workflow := newFormWorkflow()
	workflow.Hooks = hooks
	harness := openWideForms(t, workflow, newFormAuthor())
	harness.Answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: xslice.Map(available, func(name string) *hookv1.Hook {
			return &hookv1.Hook{Name: name, Executable: true}
		})}), nil
	})

	harness.Press(append([]string{"space", "E"}, openWorkflowSubformKeys(2)...)...)

	return harness
}

func assertSentHooks(t *testing.T, harness *paneltest.Panel, want []*workflowv1.WorkflowHook) {
	t.Helper()

	sent := harness.Sent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
	require.Len(t, sent, 1)
	update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
	require.True(t, ok)
	assert.Empty(t, cmp.Diff(want, update.GetHooks(), protocmp.Transform()))
}
