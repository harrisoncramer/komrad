package ui_test

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

// TestFormsKeys covers the keys a form answers to.
func TestFormsKeys(t *testing.T) {
	t.Run("enter submits from a text field", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), []string{"enter"})...)

		sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "go", create.GetPrompt())
	})

	t.Run("ctrl+j and alt+enter break a line in a text field", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("check CI", ""), []string{"ctrl+j"}, strings.Split("then", ""),
			[]string{"alt+enter"}, strings.Split("fix it", ""), []string{"enter"})...)

		sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "check CI\nthen\nfix it", create.GetPrompt())
	})
}

// TestFormsFieldEditor covers the editor key opening a form's text field in the reader's editor.
func TestFormsFieldEditor(t *testing.T) {
	t.Run("the field under the cursor is written out for the editor", func(t *testing.T) {
		harness := openFieldEditor(t)

		harness.Press("space", "E", "tab", "ctrl+e")

		edited := harness.Edited()
		require.Len(t, edited, 1)
		draft, err := os.ReadFile(edited[0])
		require.NoError(t, err)
		assert.Equal(t, "shared notes", string(draft))
	})

	t.Run("a form with no text field of its own leaves the key to the form", func(t *testing.T) {
		harness := openFieldEditor(t)

		harness.Press("r", "ctrl+e")

		assert.Empty(t, harness.Edited())
	})

	tests := []struct {
		name  string
		press []string
		field string
		want  *workflowv1.CreateWorkflowRequest
	}{
		{
			name:  "the context of a workflow that provisions nothing",
			press: slices.Concat([]string{"space", "w"}, strings.Split("web", ""), slices.Repeat([]string{"tab"}, 3), []string{"up", "shift+tab", "shift+tab"}),
			field: "context",
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Context:  "what the editor wrote!",
				Worktree: &workflowv1.WorktreeConfig{},
			},
		},
		{
			name:  "a list field of a workflow that makes worktrees",
			press: slices.Concat([]string{"space", "w"}, strings.Split("web", ""), openWorkflowSubformKeys(1), []string{"tab", "tab"}),
			field: "worktree_copy_paths",
			want: &workflowv1.CreateWorkflowRequest{
				Name: "web",
				Worktree: &workflowv1.WorktreeConfig{
					Enabled:   true,
					CopyPaths: []string{"what the editor wrote!"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFieldEditor(t)
			harness.WriteInEditor("what the editor wrote\n")
			harness.Press(append(tt.press, "ctrl+e")...)
			harness.Press("!", "enter")

			harness.AssertSentToWith(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, ignoringEventPrompts, tt.want)
		})
	}
}

// TestFormsLayout covers the panel rendering a form in a column beside the list that opened it.
func TestFormsLayout(t *testing.T) {
	t.Parallel()

	t.Run("closing the form hands the window back", func(t *testing.T) {
		t.Parallel()

		harness := openForms(t, newFormWorkflow(), newLiveAuthor())
		harness.Press("j")
		harness.Advance(time.Second)

		harness.Press("r", "esc")

		assert.False(t, harness.Tmux().IsZoomed())
	})

	t.Run("a refusal is read in full from inside the form", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{Width: 160})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(newFormWorkflow()))
		harness.Refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))
		harness.Press(slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"enter"})...)

		harness.Press("ctrl+g")
		frame := harness.Frame()
		assert.Contains(t, frame, "Could not create the workflow")
		assert.NotContains(t, frame, "New workflow name", "the form gives up what is rendered in its column")

		harness.Press("ctrl+g")
		assert.Contains(t, harness.Frame(), "New workflow name", "and takes it back")
	})
}

func openForms(t *testing.T, workflow *workflowv1.Workflow, author *agentv1.Agent) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, workflow, author)
	harness.Press("g", "g")

	return harness
}

func openFieldEditor(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(newConfiguredWorkflow(), newFormAuthor()))
	harness.Press("g", "g")
	t.Cleanup(func() {
		removeEdited(harness)
	})

	return harness
}

const workflowTopFieldCount = 4

func openWorkflowSubformKeys(index int) []string {
	return slices.Concat(slices.Repeat([]string{"tab"}, workflowTopFieldCount), slices.Repeat([]string{"down"}, index), []string{"enter"})
}

func newFormWorkflow() *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:      "w1",
		Name:    "api",
		Harness: "claude",
		Cwd:     "/work/api",
		Flags:   "--model opus",
	}
}

func newConfiguredWorkflow() *workflowv1.Workflow {
	workflow := newFormWorkflow()
	workflow.Context = "shared notes"
	workflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}

	return workflow
}

func newFormAuthor() *agentv1.Agent {
	author := paneltest.Agent("a1", "author", "w1")
	author.Cwd = "/work/api"
	author.Harness = "claude"
	author.Flags = "--model opus"

	return author
}

func newLiveAuthor() *agentv1.Agent {
	author := paneltest.Agent("a1", "author", "w1")
	author.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	author.PaneId = "%9"
	author.Alive = xptr.Ptr(true)

	return author
}

var ignoringEventPrompts = []cmp.Option{protocmp.IgnoreFields(&workflowv1.CreateWorkflowRequest{}, "event_prompts")}

func removeEdited(harness *paneltest.Panel) {
	for _, path := range harness.Edited() {
		_ = os.Remove(path)
	}
}
