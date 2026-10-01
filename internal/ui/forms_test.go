package ui_test

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	hookv1 "komrad/proto/gen/hook/v1"
	"komrad/proto/gen/hook/v1/hookv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

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
		author    *agentv1.Agent
		empty     bool
		hostPath  string
		press     []string
		procedure string
		want      proto.Message
	}{
		{
			name:      "the new-agent form in a workflow with no directory sends the one tmux was invoked from",
			workflow:  &workflowv1.Workflow{Id: "w1", Name: "api"},
			hostPath:  "/work/from-tmux",
			press:     slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			want: &agentv1.CreateAgentRequest{
				Prompt:     "check CI",
				Dir:        "/work/from-tmux",
				WorkflowId: "w1",
			},
		},
		{
			name:      "the new-agent form sends the workflow's directory and an overridden harness",
			press:     slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"tab"}, spellKeys("ci"), []string{"tab", "ctrl+u"}, spellKeys("codex"), []string{"ctrl+s"}),
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
			press:     slices.Concat([]string{"j", "space", "C"}, spellKeys("run the tests"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			want: &agentv1.CreateAgentRequest{
				Prompt:        "run the tests",
				Harness:       "claude",
				WorkflowId:    "w1",
				ParentAgentId: xptr.Ptr("a1"),
			},
		},
		{
			name: "the adopt form sends the ref and prompt into the workflow",
			press: slices.Concat([]string{"space", "A"}, spellKeys("https://github.com/acmecorp/admin/pull/1069"), []string{"tab"},
				spellKeys("pick it up"), []string{"ctrl+s"}),
			procedure: agentv1connect.AgentServiceAdoptProcedure,
			want: &agentv1.AdoptRequest{
				Ref:        "https://github.com/acmecorp/admin/pull/1069",
				Prompt:     "pick it up",
				Harness:    "claude",
				WorkflowId: "w1",
			},
		},
		{
			name:      "the first new-workflow form starts where tmux was invoked from",
			empty:     true,
			hostPath:  "/work/from-tmux",
			press:     slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"ctrl+s"}),
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Cwd:      "/work/from-tmux",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
		},
		{
			name: "the new-workflow form with no provisioner makes no worktrees",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"tab"}, spellKeys("the frontend"), []string{"tab", "tab"},
				spellKeys("/work/web"), []string{"tab"}, spellKeys("--model sonnet"), []string{"tab"}, spellKeys("codex"),
				[]string{"tab"}, spellKeys("--model gpt-5"), []string{"tab", "up", "ctrl+s"}),
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name:            "web",
				Context:         "the frontend",
				Cwd:             "/work/web",
				Flags:           "--model sonnet",
				ReviewerHarness: "codex",
				ReviewerFlags:   "--model gpt-5",
				Worktree:        &workflowv1.WorktreeConfig{},
			},
		},
		{
			name: "the new-workflow form with worktrees sends each path list without its blank lines",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), slices.Repeat([]string{"tab"}, 8),
				spellKeys("develop"), []string{"tab", "tab"}, spellKeys(".env"), []string{"ctrl+j", "ctrl+j"}, spellKeys("config/local.json"),
				[]string{"tab"}, spellKeys("node_modules"), []string{"ctrl+s"}),
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
			press:     slices.Concat([]string{"space", "E", "tab", "tab", "tab", "ctrl+u"}, spellKeys("/work/api-v2"), []string{"ctrl+s"}),
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
			name:      "the workflow archive prompt archives the workflow",
			press:     []string{"space", "a", "ctrl+s"},
			procedure: workflowv1connect.WorkflowServiceArchiveWorkflowProcedure,
			want:      &workflowv1.ArchiveWorkflowRequest{Id: "w1"},
		},
		{
			name:      "the workflow archive prompt archives the workflow from its orchestrator's menu",
			author:    newOrchestrator(),
			press:     []string{"space", "a", "ctrl+s"},
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
			author := tt.author
			if author == nil {
				author = newFormAuthor()
			}
			var harness *panelHarness
			if tt.empty {
				harness = openAgents(t)
			} else {
				harness = openForms(t, workflow, author)
			}
			harness.panel.SetHostPath(tt.hostPath)

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
		harness := openForms(t, workflow, newFormAuthor())

		harness.press("space", "c")

		assert.Contains(t, harness.readFrame(), "› Same as workflow")
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
			press: []string{"tab", "tab", "tab", "tab", "tab", "down"},
			want:  xptr.Ptr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, workflow, newFormAuthor())

			harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), tt.press, []string{"ctrl+s"})...)

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
			press:     slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"ctrl+s"}),
			typed:     "check CI",
		},
		{
			name:      "the new-workflow form",
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			press:     slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"ctrl+s"}),
			typed:     "web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, newFormWorkflow(), newFormAuthor())
			harness.refuse(tt.procedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))

			harness.press(tt.press...)

			assert.Contains(t, harness.readFrame(), tt.typed, "the form is still up holding what was filled in")

			harness.press("ctrl+g")
			frame := harness.readFrame()
			assert.Contains(t, frame, "not allowed", "a refused write is reported")
			assert.Contains(t, frame, "fix and submit again", "ctrl+g opens the whole refusal over the form")

			harness.press("ctrl+g", "!")
			assert.Contains(t, harness.readFrame(), tt.typed+"!", "and the form takes keys again")
		})
	}
}

// TestFormsAdoptExisting covers an adoption answered with the agent already on the branch.
func TestFormsAdoptExisting(t *testing.T) {
	tests := []struct {
		name        string
		existing    *agentv1.Agent
		wantScreen  string
		wantHovered string
	}{
		{
			name:        "a live agent lands the agents list on its row",
			existing:    newFormAuthor(),
			wantScreen:  agentsScreen,
			wantHovered: "author",
		},
		{
			name:        "an archived agent lands the archive on its row",
			existing:    newArchivedAgent("a3", "old-work", "w1"),
			wantScreen:  archiveScreen,
			wantHovered: "old-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, newFormWorkflow(), newFormAuthor(), newArchivedAgent("a3", "old-work", "w1"))
			harness.press("g", "g")
			harness.answer(agentv1connect.AgentServiceAdoptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.AdoptResponse{Agent: tt.existing, Existing: true}), nil
			})

			harness.press(slices.Concat([]string{"space", "A"}, spellKeys("someones/work"), []string{"ctrl+s"})...)

			frame := harness.readFrame()
			assert.Equal(t, tt.wantScreen, harness.readScreen())
			assert.Contains(t, readHoveredRow(frame), tt.wantHovered)
			assert.Contains(t, frame, "This agent already exists")
			assert.NotContains(t, frame, "Could not adopt")
		})
	}
}

// TestFormsInFlight covers the new-agent form while its create has not come back.
func TestFormsInFlight(t *testing.T) {
	create := agentv1connect.AgentServiceCreateAgentProcedure
	submit := slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"ctrl+s"})

	t.Run("the form stays up saying it is submitting until the create lands", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(create)

		harness.press(submit...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "starting the agent…")
		assert.Contains(t, frame, "check CI", "the form is still up")
		assert.Empty(t, harness.panel.ReadRespawns(), "no pane is opened for the provisioner")

		harness.release(create)

		frame = harness.readFrame()
		assert.NotContains(t, frame, "starting the agent…")
		assert.NotContains(t, frame, "check CI", "the form goes once the create has landed")
	})

	t.Run("the form keeps saying so while the pane the create made warms up", func(t *testing.T) {
		frames := &drawReporter{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor()))
		answerAgentCreate(harness, "%9")
		harness.press("g", "g")

		harness.press(submit...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "starting the agent…")
		assert.Contains(t, frame, "check CI", "the form is still up")

		frames.isDrawn = true
		harness.fireTimers()

		frame = harness.readFrame()
		assert.NotContains(t, frame, "starting the agent…")
		assert.NotContains(t, frame, "check CI", "the form goes with the reveal")
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a second submit sends nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(create)
		harness.press(submit...)

		harness.press("ctrl+s", "!")

		assert.Len(t, harness.readSent(create), 1, "the key never reached the submit path")
		assert.Contains(t, harness.readFrame(), "check CI", "and the form is as it was")
		assert.NotContains(t, harness.readFrame(), "check CI!")
	})

	t.Run("esc hands the panel back and the create still lands without taking the cursor", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.hold(create)
		harness.press(submit...)

		harness.press("esc")

		assert.NotContains(t, harness.readFrame(), "check CI", "the form it was holding is gone")
		assert.Contains(t, readLegend(harness.readFrame()), "⠋ starting", "the create it left is drawn in the corner")
		harness.press("j")
		assert.Equal(t, "  author", readHoveredRow(harness.readFrame()), "the panel takes keys again")

		harness.release(create)
		harness.watch(buildPuts(newAgent("a2", "check-ci", "w1")))

		frame := harness.readFrame()
		assert.Equal(t, "\uf444 local", readEngineLine(frame), "nothing is reported")
		assert.Equal(t, "  author", readHoveredRow(frame), "the cursor stays where the reader left it")
	})

	t.Run("an escaped create landing leaves a later form held", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(create)
		harness.press(submit...)
		harness.press("esc")
		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("the second one"), []string{"ctrl+s"})...)

		harness.release(create)

		frame := harness.readFrame()
		assert.Contains(t, frame, "starting the agent…", "the second form is still held")
		assert.Contains(t, frame, "the second one")
	})
}

// TestFormsArchivePrompt covers the ways the agent archive menu can be answered.
func TestFormsArchivePrompt(t *testing.T) {
	t.Run("the menu opens on the reversible answer", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("j", "space", "a")

		frame := harness.readFrame()
		assert.Contains(t, frame, "› archive  ")
		assert.Contains(t, frame, "archive and delete for good")
		assert.Contains(t, frame, "back")
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
			name:  "one row down archives and then destroys once confirmed",
			press: []string{"down", "ctrl+s", "y"},
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
			harness := openForms(t, newFormWorkflow(), newFormAuthor())
			harness.press("j", "space", "a")
			before := harness.readRequests()

			harness.press(tt.press...)

			harness.assertSent(append(before, tt.wantSent...)...)
			assert.Equal(t, agentsScreen, harness.readScreen(), "every answer lands back on the list")
		})
	}

	t.Run("the workflow menu stays up until the archive lands", func(t *testing.T) {
		archive := workflowv1connect.WorkflowServiceArchiveWorkflowProcedure
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(archive)

		harness.press("space", "a", "a")

		frame := harness.readFrame()
		assert.Contains(t, frame, "archive with its agents", "the menu is still drawn")
		assert.Contains(t, frame, "archiving api…")

		harness.release(archive)

		assert.NotContains(t, harness.readFrame(), "archive with its agents")
	})

	t.Run("a refused archive takes the menu down", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived")))

		harness.press("j", "space", "a", "a")

		assert.NotContains(t, harness.readFrame(), "archive and delete for good")
		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("a confirmed archive still in flight keeps a rebuilt panel up", func(t *testing.T) {
		binary := &fakeBinary{}
		harness := newPanelHarness(t, panelHarnessParams{binary: binary})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor()))
		harness.hold(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.press("g", "g", "j", "space", "a", "ctrl+s")

		binary.isReplaced = true
		harness.fireTimers()
		require.NotContains(t, harness.readFrame(), "komrad was rebuilt", "the write is still in flight")

		harness.release(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.fireTimers()
		assert.Contains(t, harness.readFrame(), "komrad was rebuilt")
	})

	t.Run("a refused workflow archive says why and keeps the workflow", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.refuse(workflowv1connect.WorkflowServiceArchiveWorkflowProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("agents still running")))

		harness.press(slices.Concat([]string{"space", "a", "ctrl+s"}, widenListPresses)...)

		assert.Contains(t, readListRows(harness.readFrame()), "api")
		assert.Contains(t, readOpenMessages(harness), "agents still running")
	})

	t.Run("a refused archive destroys nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived")))

		harness.press(slices.Concat([]string{"j", "space", "a", "down", "ctrl+s", "y"}, widenListPresses)...)

		assert.Contains(t, harness.readFrame(), `Could not archive and delete agent "author"`)
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceDeleteAgentsProcedure))
	})
}

// TestFormsKeys covers the keys a form answers to.
func TestFormsKeys(t *testing.T) {
	t.Run("enter on the last field of a form leaves it up", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), slices.Repeat([]string{"enter"}, 8))...)

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure))
		assert.Contains(t, harness.readFrame(), "Initial prompt")
	})

	t.Run("enter breaks a line in a text field rather than moving to the next field", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"enter"}, spellKeys("then fix it"), []string{"ctrl+s"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "check CI\nthen fix it", create.GetPrompt())
	})

	t.Run("enter in an input leaves the cursor on it", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "c", "tab"}, spellKeys("ci"), []string{"enter"}, spellKeys("-v2"), []string{"ctrl+s"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "ci-v2", create.GetTitle())
	})

	t.Run("a workflow edited to a blank name sends nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("space", "E", "ctrl+u", "space", "space", "ctrl+s")

		assert.Empty(t, harness.readSent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure))
	})

	t.Run("the adopt key on an agent row opens nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("j", "a")

		assert.NotContains(t, harness.readFrame(), "Adopt")
		assert.Equal(t, "  author", readHoveredRow(harness.readFrame()))
	})
}

// TestFormsWorkflowProvisioner covers the workflow form asking only what its provisioner needs.
func TestFormsWorkflowProvisioner(t *testing.T) {
	harness := openForms(t, newFormWorkflow(), newFormAuthor())

	harness.press(slices.Concat([]string{"space", "w"}, slices.Repeat([]string{"tab"}, 7))...)
	frame := harness.readFrame()
	assert.Contains(t, frame, "None: agents keep the directory they are given", "neither option is hidden by the other being picked")
	assert.Less(t, strings.Index(frame, "None: agents keep"), strings.Index(frame, "Worktree: a git worktree per agent"), "and the options keep their order")
	assert.Contains(t, frame, "Worktree base branch", "worktree is the default, so its fields are already on screen")

	harness.press(slices.Concat([]string{"tab", "tab", "tab"}, spellKeys(".env"), []string{"shift+tab", "shift+tab", "shift+tab", "up"})...)
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
		{Event: config.HookPreResume, Script: "warm-up"},
		{Event: config.HookPostResume, Script: "announce-back"},
		{Event: config.HookPreArchive, Script: "check-in"},
		{Event: config.HookPostArchive, Script: "give-back-the-slot"},
		{Event: config.HookPreRestore, Script: "lock"},
		{Event: config.HookPostRestore, Script: "unlock"},
		{Event: config.HookPreDelete, Script: "take-the-stack-down"},
		{Event: config.HookPostDelete, Script: "say-goodbye"},
	}

	t.Run("a picker offers every hook and opens on the one the workflow names", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: config.HookPreLaunch, Script: "copy-env"}}, "install-deps", "copy-env")

		frame := harness.readFrame()
		assert.Contains(t, frame, "install-deps")
		assert.Contains(t, frame, "› copy-env")
	})

	t.Run("a hook still waiting on a chmod is offered", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: []*hookv1.Hook{{Name: "unrunnable"}}}), nil
		})

		harness.press("space", "E")

		assert.Contains(t, harness.readFrame(), "unrunnable")
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
		harness := openHookForm(t, every, "install-deps", "announce", "warm-up", "announce-back", "check-in",
			"give-back-the-slot", "lock", "unlock", "take-the-stack-down", "say-goodbye")

		harness.press(slices.Concat(slices.Repeat([]string{"enter"}, 25), []string{"ctrl+s"})...)

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

// TestFormsFieldEditor covers the editor key opening a form's text field in the reader's editor.
func TestFormsFieldEditor(t *testing.T) {
	t.Run("the field under the cursor is written out for the editor", func(t *testing.T) {
		harness := openFieldEditor(t)

		harness.press("space", "E", "tab", "ctrl+e")

		draft, err := os.ReadFile(ui.FormFieldDraftPath("context"))
		require.NoError(t, err)
		assert.Equal(t, "shared notes", string(draft))
	})

	t.Run("a form with no text field of its own leaves the key to the form", func(t *testing.T) {
		harness := openFieldEditor(t)

		harness.press("r", "ctrl+e")

		_, err := os.Stat(ui.FormFieldDraftPath("name"))
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	tests := []struct {
		name  string
		press []string
		field string
		want  *workflowv1.CreateWorkflowRequest
	}{
		{
			name:  "the context of a workflow that provisions nothing",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), slices.Repeat([]string{"tab"}, 7), []string{"up"}, slices.Repeat([]string{"shift+tab"}, 6)),
			field: "context",
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Context:  "what the editor wrote!",
				Worktree: &workflowv1.WorktreeConfig{},
			},
		},
		{
			name:  "a list field of a workflow that makes worktrees",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), slices.Repeat([]string{"tab"}, 10)),
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
			harness.press(append(tt.press, "ctrl+e")...)

			harness.send(harness.panel.NewFieldEdited(tt.field, "what the editor wrote\n"))
			harness.press("!", "ctrl+s")

			harness.assertSentTo(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, tt.want)
		})
	}
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
			press: []string{"space", "w"},
			want:  "New workflow name",
		},
		{
			name:  "a confirmation takes the window beside the list",
			press: []string{"space", "a", "d"},
			want:  `Permanently delete agent "author"?`,
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

	t.Run("the list's cursor stands still beside a form", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			press      []string
			wantCursor bool
		}{
			{
				name: "the list alone blinks its cursor out",
			},
			{
				name:       "a form holding the keys leaves it drawn",
				press:      []string{"space", "w"},
				wantCursor: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := openForms(t, newFormWorkflow(), newFormAuthor())
				harness.press(tt.press...)

				harness.fireTimers()

				assert.Equal(t, tt.wantCursor, readHoveredRow(harness.readFrame()) != "")
			})
		}
	})

	t.Run("a held form draws its notice under it at every height", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name   string
			height int
		}{
			{
				name:   "a short window",
				height: 24,
			},
			{
				name:   "a middling window",
				height: 32,
			},
			{
				name:   "a tall window",
				height: 40,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := newPanelHarness(t, panelHarnessParams{
					width:  160,
					height: tt.height,
				})
				answerWorktreeReads(harness)
				harness.watch(buildSnapshot(newFormWorkflow()))
				harness.hold(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)

				harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"ctrl+s"})...)

				lines := splitLines(strings.TrimSuffix(harness.readFrame(), "\n"))
				assert.Len(t, lines, tt.height)
				assert.NotContains(t, lines[0], "creating workflow", "the panel keeps its top row")
				assert.Contains(t, findLastFilled(lines), "creating workflow web…", "the notice sits under the form")
			})
		}
	})

	t.Run("a window too narrow for both halves draws the form whole", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 60})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newAgent("a1", "author", "w1")))

		harness.press("space", "w")

		frame := harness.readFrame()
		assert.Contains(t, splitLines(frame)[0], "New workflow name")
		assert.NotContains(t, frame, "author", "the list gives the form its room")
	})

	t.Run("a refusal is read in full from inside the form", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 160})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow()))
		harness.refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))
		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"ctrl+s"})...)

		harness.press("ctrl+g")
		frame := harness.readFrame()
		assert.Contains(t, frame, "Could not create the workflow")
		assert.NotContains(t, frame, "New workflow name", "the form gives up what is drawn in its column")

		harness.press("ctrl+g")
		assert.Contains(t, harness.readFrame(), "New workflow name", "and takes it back")
	})
}

// TestFormsFitTheWindow covers a form opening at its top and staying inside the window.
func TestFormsFitTheWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		height    int
		isRefused bool
		want      string
	}{
		{
			name:      "a refused form in a short window",
			height:    25,
			isRefused: true,
			want:      "Could not create the workflow",
		},
		{
			name:      "a refused form in a middling window",
			height:    32,
			isRefused: true,
			want:      "Could not create the workflow",
		},
		{
			name:      "a refused form in a tall window",
			height:    40,
			isRefused: true,
			want:      "Could not create the workflow",
		},
		{
			name:   "a form in a very short window",
			height: 16,
			want:   "New workflow name",
		},
		{
			name:   "a form in a short window",
			height: 24,
			want:   "New workflow name",
		},
		{
			name:   "a form in a middling window",
			height: 32,
			want:   "New workflow name",
		},
		{
			name:   "a form in a very tall window",
			height: 60,
			want:   "New workflow name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := newPanelHarness(t, panelHarnessParams{width: 160, height: tt.height})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(newFormWorkflow()))
			press := []string{"space", "w"}
			if tt.isRefused {
				harness.refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))
				press = slices.Concat(press, spellKeys("web"), []string{"ctrl+s"})
			}

			harness.press(press...)

			frame := harness.readFrame()
			assert.LessOrEqual(t, len(splitLines(frame)), tt.height)
			assert.Contains(t, frame, tt.want)
		})
	}
}

func openForms(t *testing.T, workflow *workflowv1.Workflow, author *agentv1.Agent) *panelHarness {
	t.Helper()

	harness := openAgents(t, workflow, author)
	harness.press("g", "g")

	return harness
}

func openFieldEditor(t *testing.T) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{Editor: "true"}})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newConfiguredWorkflow(), newFormAuthor()))
	harness.press("g", "g")
	t.Cleanup(func() {
		for _, key := range []string{"context", "name", "worktree_copy_paths"} {
			_ = os.Remove(ui.FormFieldDraftPath(key))
		}
	})

	return harness
}

func openHookForm(t *testing.T, hooks []*workflowv1.WorkflowHook, available ...string) *panelHarness {
	t.Helper()

	workflow := newFormWorkflow()
	workflow.Hooks = hooks
	harness := openForms(t, workflow, newFormAuthor())
	harness.answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: xslice.Map(available, func(name string) *hookv1.Hook {
			return &hookv1.Hook{Name: name, Executable: true}
		})}), nil
	})

	harness.press("space", "E")

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
	author := newAgent("a1", "author", "w1")
	author.Cwd = "/work/api"
	author.Harness = "claude"
	author.Flags = "--model opus"

	return author
}

func newLiveAuthor() *agentv1.Agent {
	author := newAgent("a1", "author", "w1")
	author.PaneId = "%9"
	author.Alive = xptr.Ptr(true)

	return author
}

func spellKeys(text string) []string {
	return strings.Split(text, "")
}

func findLastFilled(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}

	return ""
}
