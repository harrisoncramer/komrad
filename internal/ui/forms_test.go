package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"weave/config"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// TestFormsSubmit covers every form sending what was typed into it and closing once the write lands.
func TestFormsSubmit(t *testing.T) {
	tests := []struct {
		name      string
		workflow  *workflowv1.Workflow
		press     []string
		procedure string
		want      proto.Message
	}{
		{
			name:      "the new-agent form sends the workflow's directory and an overridden harness",
			press:     slices.Concat([]string{"c"}, typed("check CI"), []string{"enter"}, typed("ci"), []string{"enter", "ctrl+u"}, typed("codex"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			want: &agentv1.CreateAgentRequest{
				Prompt:     "check CI",
				Title:      "ci",
				Harness:    "codex",
				Dir:        "/work/api",
				WorkflowId: "w1",
			},
		},
		{
			name:      "the child form sends its parent and the parent's harness but not its flags",
			press:     slices.Concat([]string{"j", "C"}, typed("run the tests"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			want: &agentv1.CreateAgentRequest{
				Prompt:        "run the tests",
				Harness:       "claude",
				WorkflowId:    "w1",
				ParentAgentId: proto.String("a1"),
			},
		},
		{
			name:      "the rename form sends the new name",
			press:     slices.Concat([]string{"j", "r"}, typed("api-v2"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      &agentv1.UpdateAgentRequest{Id: "a1", Name: "api-v2"},
		},
		{
			name: "the adopt form sends the ref and prompt into the workflow",
			press: slices.Concat([]string{"a"}, typed("https://github.com/acmecorp/admin/pull/1069"), []string{"enter"},
				typed("pick it up"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceAdoptProcedure,
			want: &agentv1.AdoptRequest{
				Ref:        "https://github.com/acmecorp/admin/pull/1069",
				Prompt:     "pick it up",
				Harness:    "claude",
				WorkflowId: "w1",
			},
		},
		{
			name: "the new-workflow form with no provisioner makes no worktrees",
			press: slices.Concat([]string{"w"}, typed("web"), []string{"enter"}, typed("the frontend"), []string{"enter", "enter"},
				typed("/work/web"), []string{"enter"}, typed("--model sonnet"), []string{"enter", "up", "ctrl+s"}),
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Context:  "the frontend",
				Cwd:      "/work/web",
				Flags:    "--model sonnet",
				Worktree: &workflowv1.WorktreeConfig{},
			},
		},
		{
			name: "the new-workflow form with worktrees sends each path list without its blank lines",
			press: slices.Concat([]string{"w"}, typed("web"), []string{"enter", "enter", "enter", "enter", "enter", "enter"},
				typed("develop"), []string{"enter", "enter"}, typed(".env"), []string{"ctrl+j", "ctrl+j"}, typed("config/local.json"),
				[]string{"enter"}, typed("node_modules"), []string{"ctrl+s"}),
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name: "web",
				Worktree: &workflowv1.WorktreeConfig{
					Enabled:      true,
					BaseBranch:   "develop",
					CopyPaths:    []string{".env", "config/local.json"},
					SymlinkPaths: []string{"node_modules"},
				},
			},
		},
		{
			name:      "the workflow edit form sends the field edited and the ones that were not",
			workflow:  newConfiguredWorkflow(),
			press:     slices.Concat([]string{"e", "enter", "enter", "enter", "ctrl+u"}, typed("/work/api-v2"), []string{"ctrl+s"}),
			procedure: workflowv1connect.WorkflowServiceUpdateWorkflowProcedure,
			want: &workflowv1.UpdateWorkflowRequest{
				Id:       "w1",
				Name:     "api",
				Context:  "shared notes",
				Cwd:      "/work/api-v2",
				Flags:    "--model opus",
				Harness:  "claude",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
		},
		{
			name:      "the workflow rename form carries the config it is not changing",
			press:     slices.Concat([]string{"r"}, typed("backend"), []string{"ctrl+s"}),
			procedure: workflowv1connect.WorkflowServiceUpdateWorkflowProcedure,
			want: &workflowv1.UpdateWorkflowRequest{
				Id:       "w1",
				Name:     "backend",
				Cwd:      "/work/api",
				Flags:    "--model opus",
				Harness:  "claude",
				Worktree: &workflowv1.WorktreeConfig{},
			},
		},
		{
			name:      "the workflow archive prompt archives the workflow",
			press:     []string{"z", "ctrl+s"},
			procedure: workflowv1connect.WorkflowServiceArchiveWorkflowProcedure,
			want:      &workflowv1.ArchiveWorkflowRequest{Id: "w1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := tt.workflow
			if workflow == nil {
				workflow = newFormWorkflow()
			}
			harness := openForms(t, workflow)

			harness.press(tt.press...)

			harness.assertSentTo(tt.procedure, tt.want)
			assert.NotContains(t, harness.readFrame(), "Could not", "the service accepted the write")
			assert.NotContains(t, harness.readFrame(), "C-s submit", "and the form it closed is gone")
		})
	}
}

// TestFormsNewAgentWorktree covers the one thing an agent can say for itself about its worktree.
func TestFormsNewAgentWorktree(t *testing.T) {
	workflow := newFormWorkflow()
	workflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}

	t.Run("the field opens on what the workflow does", func(t *testing.T) {
		harness := openForms(t, workflow)

		harness.press("c")

		assert.Contains(t, harness.readFrame(), "> Same as workflow")
	})

	tests := []struct {
		name  string
		press []string
		want  *bool
	}{
		{
			name: "left on the default nothing is sent",
		},
		{
			name:  "choosing none is sent as an override rather than as nothing",
			press: []string{"enter", "enter", "enter", "enter", "enter", "down"},
			want:  proto.Bool(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, workflow)

			harness.press(slices.Concat([]string{"c"}, typed("go"), tt.press, []string{"ctrl+s"})...)

			sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
			require.Len(t, sent, 1)
			create, ok := sent[0].(*agentv1.CreateAgentRequest)
			require.True(t, ok)
			assert.Equal(t, tt.want, create.ProvisionEnabled)
		})
	}
}

// TestFormsRefused covers a refused write leaving the form up, holding what was typed.
func TestFormsRefused(t *testing.T) {
	tests := []struct {
		name      string
		procedure string
		press     []string
		typed     string
	}{
		{
			name:      "the new-agent form",
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			press:     slices.Concat([]string{"c"}, typed("check CI"), []string{"ctrl+s"}),
			typed:     "check CI",
		},
		{
			name:      "the rename form",
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			press:     slices.Concat([]string{"j", "r"}, typed("api-v2"), []string{"ctrl+s"}),
			typed:     "api-v2",
		},
		{
			name:      "the new-workflow form",
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			press:     slices.Concat([]string{"w"}, typed("web"), []string{"ctrl+s"}),
			typed:     "web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, newFormWorkflow())
			harness.refuse(tt.procedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Contains(t, frame, "not allowed", "a refused write is reported")
			assert.Contains(t, frame, tt.typed, "the form is still up holding what was filled in")

			harness.press("ctrl+g")
			assert.Contains(t, harness.readFrame(), "fix and submit again", "esc backs out of the form rather than clearing the message")

			harness.press("ctrl+g", "!")
			assert.Contains(t, harness.readFrame(), tt.typed+"!", "and the form takes keys again")
		})
	}
}

// TestFormsArchivePrompt covers the three ways the agent archive prompt can be answered.
func TestFormsArchivePrompt(t *testing.T) {
	t.Run("the prompt opens on the reversible answer", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow())

		harness.press("j", "z")

		frame := harness.readFrame()
		assert.Contains(t, frame, "> Archive")
		assert.Contains(t, frame, "Archive and delete")
		assert.Contains(t, frame, "Cancel")
	})

	tests := []struct {
		name     string
		press    []string
		wantSent []request
	}{
		{
			name:  "submitting without moving archives and keeps the record",
			press: []string{"ctrl+s"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
				msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "one row down archives and then destroys",
			press: []string{"down", "ctrl+s"},
			wantSent: []request{
				{
					procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
					msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}},
				},
				{
					procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
					msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a1"}},
				},
			},
		},
		{
			name:  "the last row leaves the agent where it is",
			press: []string{"down", "down", "ctrl+s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, newFormWorkflow())
			harness.press("j", "z")
			before := harness.readSent(worktreeReadProcedure)

			harness.press(tt.press...)

			harness.assertSent(append(asRequests(worktreeReadProcedure, before), tt.wantSent...)...)
			assert.Contains(t, splitLines(harness.readFrame())[0], "Agents & Workflows", "every answer lands back on the list")
		})
	}

	t.Run("a refused archive destroys nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow())
		harness.refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived")))

		harness.press(slices.Concat([]string{"j", "z", "down", "ctrl+s"}, widenListPresses)...)

		assert.Contains(t, harness.readFrame(), `Could not archive and delete agent "author"`)
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceDeleteAgentsProcedure))
	})
}

// TestFormsKeys covers the keys a form answers to.
func TestFormsKeys(t *testing.T) {
	t.Run("enter on the last field of a form leaves it up", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow())

		harness.press(slices.Concat([]string{"c"}, typed("go"), slices.Repeat([]string{"enter"}, 8))...)

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure))
		assert.Contains(t, harness.readFrame(), "Initial prompt")
	})

	t.Run("enter leaves a one-field form up too", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow())

		harness.press(slices.Concat([]string{"j", "r"}, typed("api-v2"), []string{"enter"})...)

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceUpdateAgentProcedure))
	})

	t.Run("the adopt key on an agent row opens nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow())

		harness.press("j", "a")

		assert.NotContains(t, harness.readFrame(), "Adopt")
		assert.Equal(t, "  author", readHoveredRow(harness.readFrame()))
	})
}

// TestFormsWorkflowProvisioner covers the workflow form asking only what its provisioner needs.
func TestFormsWorkflowProvisioner(t *testing.T) {
	harness := openForms(t, newFormWorkflow())

	harness.press(slices.Concat([]string{"w"}, slices.Repeat([]string{"enter"}, 5))...)
	frame := harness.readFrame()
	assert.Contains(t, frame, "None: agents keep the directory they are given", "neither option is hidden by the other being picked")
	assert.Less(t, strings.Index(frame, "None: agents keep"), strings.Index(frame, "Worktree: a git worktree per agent"), "and the options keep their order")
	assert.Contains(t, frame, "Worktree base branch", "worktree is the default, so its fields are already on screen")

	harness.press(slices.Concat([]string{"enter", "enter", "enter"}, typed(".env"), []string{"shift+tab", "shift+tab", "shift+tab", "up"})...)
	frame = harness.readFrame()
	assert.NotContains(t, frame, "Worktree base branch", "none provisions nothing, so it asks nothing about a worktree")
	assert.Contains(t, frame, "Pre-launch hook", "the hooks are asked for whichever provisioner is chosen")

	harness.press("down", "ctrl+s")

	sent := harness.readSent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
	require.Len(t, sent, 1)
	create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
	require.True(t, ok)
	assert.Equal(t, []string{".env"}, create.GetWorktree().GetCopyPaths(), "switching back hands back what was typed before")
}

// TestFormsWorkflowHooks covers the hook pickers offering what the hooks directory holds.
func TestFormsWorkflowHooks(t *testing.T) {
	every := []*workflowv1.WorkflowHook{
		{Event: config.HookPreLaunch, Script: "install-deps"},
		{Event: config.HookPostLaunch, Script: "announce"},
		{Event: config.HookPostResume, Script: "announce-back"},
		{Event: config.HookPostRestore, Script: "unlock"},
		{Event: config.HookPreTeardown, Script: "take-the-stack-down"},
	}

	t.Run("a picker offers every hook and opens on the one the workflow names", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: config.HookPreLaunch, Script: "copy-env"}}, "install-deps", "copy-env")

		frame := harness.readFrame()
		assert.Contains(t, frame, "install-deps")
		assert.Contains(t, frame, "> copy-env")
	})

	t.Run("a hook the directory no longer holds is still offered", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: config.HookPreLaunch, Script: "copy-env"}}, "install-deps")

		assert.Contains(t, harness.readFrame(), "missing")
	})

	t.Run("an empty hooks directory says where hooks go", func(t *testing.T) {
		harness := openHookForm(t, nil)

		assert.Contains(t, harness.readFrame(), "none found in your hooks directory")
	})

	t.Run("a directory too long to draw whole says the list carries on", func(t *testing.T) {
		harness := openHookForm(t, nil, "copy-env", "install-deps", "seed-db", "warm-cache", "link-modules")

		assert.Contains(t, harness.readFrame(), "↑/↓ for more")
	})

	t.Run("every moment comes back as it went in", func(t *testing.T) {
		harness := openHookForm(t, every, "install-deps", "announce", "announce-back", "unlock", "take-the-stack-down")

		harness.press(slices.Concat(slices.Repeat([]string{"enter"}, 20), []string{"ctrl+s"})...)

		assertSentHooks(t, harness, every)
	})

	t.Run("a moment holding two keeps the one that ran first", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{
			{Event: config.HookPreLaunch, Script: "install-deps"},
			{Event: config.HookPreLaunch, Script: "copy-env"},
		}, "install-deps", "copy-env")

		harness.press("ctrl+s")

		assertSentHooks(t, harness, []*workflowv1.WorkflowHook{{Event: config.HookPreLaunch, Script: "install-deps"}})
	})
}

// TestFormsLayout covers a form being drawn in a column beside the list it was opened over.
func TestFormsLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "a form takes the window beside the list",
			press: []string{"w"},
			want:  "New workflow name",
		},
		{
			name:  "a confirmation takes the window beside the list",
			press: []string{"z"},
			want:  `Archive agent "author"?`,
		},
		{
			name:  "the rename form takes the window beside the list",
			press: []string{"r"},
			want:  `Rename "author" to`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openForms(t, newFormWorkflow(), newLiveAuthor())
			harness.press("j")
			harness.fireTimers()
			require.False(t, harness.panel.IsZoomed(), "a live agent's pane shares the window")

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.True(t, harness.panel.IsZoomed())
			assert.Contains(t, readStage(frame)[0], tt.want)
			assert.Contains(t, readListRows(frame), "  author", "the list it was opened over is still beside it")
		})
	}

	t.Run("closing the form hands the window back", func(t *testing.T) {
		t.Parallel()

		harness := openForms(t, newFormWorkflow(), newLiveAuthor())
		harness.press("j")
		harness.fireTimers()

		harness.press("r", "esc")

		assert.False(t, harness.panel.IsZoomed())
	})

	t.Run("a window too narrow for both halves draws the form whole", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 60})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newAgent("a1", "author", "w1")))

		harness.press("w")

		frame := harness.readFrame()
		assert.Contains(t, splitLines(frame)[0], "New workflow name")
		assert.NotContains(t, frame, "author", "the list gives the form its room")
	})

	for _, height := range []int{16, 24, 32, 60} {
		t.Run(fmt.Sprintf("a form in a window %d rows tall opens at its top and stays inside", height), func(t *testing.T) {
			t.Parallel()

			harness := newPanelHarness(t, panelHarnessParams{width: 160, height: height})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(newFormWorkflow()))

			harness.press("w")

			frame := harness.readFrame()
			assert.LessOrEqual(t, len(splitLines(frame)), height)
			assert.Contains(t, frame, "New workflow name")
		})
	}

	for _, height := range []int{24, 32, 40} {
		t.Run(fmt.Sprintf("a refused form in a window %d rows tall stays inside", height), func(t *testing.T) {
			t.Parallel()

			harness := newPanelHarness(t, panelHarnessParams{width: 160, height: height})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(newFormWorkflow()))
			harness.refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))

			harness.press(slices.Concat([]string{"w"}, typed("web"), []string{"ctrl+s"})...)

			frame := harness.readFrame()
			assert.LessOrEqual(t, len(splitLines(frame)), height)
			assert.Contains(t, frame, "no such workflow")
		})
	}

	t.Run("a refusal is read in full from inside the form", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 160})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow()))
		harness.refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))
		harness.press(slices.Concat([]string{"w"}, typed("web"), []string{"ctrl+s"})...)

		harness.press("ctrl+g")
		frame := harness.readFrame()
		assert.Contains(t, frame, "Could not create the workflow")
		assert.NotContains(t, frame, "New workflow name", "the form gives up what is drawn in its column")

		harness.press("ctrl+g")
		assert.Contains(t, harness.readFrame(), "New workflow name", "and takes it back")
	})
}

const worktreeReadProcedure = "/worktree.v1.WorktreeService/GetWorktree"

func openForms(t *testing.T, workflow *workflowv1.Workflow, agents ...*agentv1.Agent) *panelHarness {
	t.Helper()

	author := newAgent("a1", "author", "w1")
	author.Cwd = "/work/api"
	author.Harness = "claude"
	author.Flags = "--model opus"
	if len(agents) > 0 {
		author = agents[0]
	}

	harness := openAgents(t, workflow, author)
	harness.press("g", "g")

	return harness
}

func openHookForm(t *testing.T, hooks []*workflowv1.WorkflowHook, available ...string) *panelHarness {
	t.Helper()

	workflow := newFormWorkflow()
	workflow.Hooks = hooks
	harness := openForms(t, workflow)
	harness.answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: xslice.Map(available, func(name string) *hookv1.Hook {
			return &hookv1.Hook{Name: name, Executable: true}
		})}), nil
	})

	harness.press("e")

	return harness
}

func assertSentHooks(t *testing.T, harness *panelHarness, want []*workflowv1.WorkflowHook) {
	t.Helper()

	sent := harness.readSent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
	require.Len(t, sent, 1)
	update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
	require.True(t, ok)
	assert.Empty(t, cmp.Diff(want, update.GetHooks(), protocmp.Transform()))
}

func asRequests(procedure string, msgs []proto.Message) []request {
	return xslice.Map(msgs, func(msg proto.Message) request {
		return request{procedure: procedure, msg: msg}
	})
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

func newLiveAuthor() *agentv1.Agent {
	author := newAgent("a1", "author", "w1")
	author.PaneId = "%9"
	author.Alive = proto.Bool(true)

	return author
}

func typed(text string) []string {
	return strings.Split(text, "")
}
