package agents

import (
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewChildAgentTakesItsLaunchFromTheParent pins what a child is given rather than asked
// for: the parent's checkout, no provisioner of its own, and the parent's workflow.
func TestNewChildAgentTakesItsLaunchFromTheParent(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	dir := t.TempDir()
	parent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        dir,
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	child, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, child)

	assert.Equal(t, dir, child.Cwd, "a child runs where its parent runs")
	require.NotNil(t, child.ParentAgentID)
	assert.Equal(t, parent.ID, *child.ParentAgentID)
	assert.Equal(t, parent.WorkflowID, child.WorkflowID)

	// An unset provisioner falls back to the workflow's, which for a worktree-enabled workflow
	// would tear the parent's checkout down, so the blank has to be on the row itself.
	require.NotNil(t, child.ProvisionEnabled)
	assert.False(t, *child.ProvisionEnabled)

	assert.Empty(t, deprovisionActionFor(ctx, st.Workflows, *child), "and a provisioner turned off leaves nothing to run against it")
}

// TestNewChildAgentRefusals pins that a request asking for what the parent decides is answered
// rather than quietly overridden, since a caller told nothing would never know.
func TestNewChildAgentRefusals(t *testing.T) {
	tests := []struct {
		name      string
		params    func(parent agentstore.Agent) NewAgentParams
		wantErrIs error
	}{
		{
			name: "a directory of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{
					Dir:           "/tmp",
					WorkflowID:    parent.WorkflowID,
					ParentAgentID: parent.ID,
				}
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a provisioner of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{
					ProvisionEnabled: xptr.Ptr(true),
					WorkflowID:       parent.WorkflowID,
					ParentAgentID:    parent.ID,
				}
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a workflow other than its parent's",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{WorkflowID: "b9b1a1de-0000-7000-8000-000000000000", ParentAgentID: parent.ID}
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a parent nothing answers to",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{WorkflowID: parent.WorkflowID, ParentAgentID: "b9b1a1de-0000-7000-8000-000000000000"}
			},
			wantErrIs: ErrParentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			require.NoError(t, st.Update(t.Context(), withCwd(parent, t.TempDir())))

			params := tt.params(parent)
			params.Prompt = "read the change"
			params.Title = "reviewer"

			created, err := manager.launch.NewAgent(t.Context(), params)
			require.ErrorIs(t, err, tt.wantErrIs)
			assert.Nil(t, created)

			live, listErr := st.List(t.Context())
			require.NoError(t, listErr)
			assert.Len(t, live, 1, "the refusal records nothing beside the parent")
		})
	}
}

// TestNewChildAgentRefusesAnArchivedParent pins that a child is never attached to an agent
// already on its way out, since it would be archived on the next sweep with nothing to answer.
func TestNewChildAgentRefusesAnArchivedParent(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))

	_, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.ErrorIs(t, err, ErrAgentArchived)
}

// TestNewChildAgentRefusesAParentWithNoDirectory pins that a child is never launched into the
// workflow's own checkout, which is what the dir fallback would hand it.
func TestNewChildAgentRefusesAParentWithNoDirectory(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.Empty(t, parent.Cwd)

	_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.ErrorIs(t, err, ErrChildNeedsParentDirectory)
}

// TestNewChildAgentRefusesAChildOfAChild pins the one level, which is what keeps the ride-along
// rules a single hop and the sidebar two deep under a workflow.
func TestNewChildAgentRefusesAChildOfAChild(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:         "second-reviewer",
		Prompt:        "read the review",
		WorkflowID:    child.WorkflowID,
		ParentAgentID: child.ID,
	})
	require.ErrorIs(t, err, ErrParentIsChild)
}

// TestNewChildAgentUnderTheChildCeiling pins the per-parent cap, which is lower than the
// panel's own because every child of one parent edits the one checkout.
func TestNewChildAgentUnderTheChildCeiling(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:        tmux,
		harnesses:   fakeHarnesses{harness: &fakeHarness{}},
		addr:        "127.0.0.1:9999",
		maxChildren: 1,
	})
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Update(ctx, withCwd(parent, t.TempDir())))

	attach := func(title string) error {
		_, err := manager.launch.NewAgent(ctx, NewAgentParams{
			Title:         title,
			Prompt:        "read the change",
			WorkflowID:    parent.WorkflowID,
			ParentAgentID: parent.ID,
		})

		return err
	}

	require.NoError(t, attach("reviewer"))
	require.ErrorIs(t, attach("second-reviewer"), ErrTooManyChildren)
}

func withCwd(agent agentstore.Agent, dir string) agentstore.Agent {
	agent.Cwd = dir

	return agent
}

// TestArchiveTakesTheChildren pins that a child goes with its parent, whether or not the call
// named it too.
func TestArchiveTakesTheChildren(t *testing.T) {
	tests := []struct {
		name    string
		archive func(parent, child agentstore.Agent) []string
	}{
		{
			name:    "the parent takes the child with it",
			archive: func(parent, _ agentstore.Agent) []string { return []string{parent.ID} },
		},
		{
			name:    "naming both is the same archive",
			archive: func(parent, child agentstore.Agent) []string { return []string{parent.ID, child.ID} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			child := seedChild(t, st, tmux, parent)

			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, tt.archive(parent, child)))

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, live, "the family goes together")
			assert.Contains(t, tmux.killedPanes, child.Pane, "and the child's pane goes with its row")
		})
	}
}

// TestArchiveOfAChildLeavesTheParent pins that finishing with one child says nothing about the
// agent it was reading for, which keeps working with its row where it was.
func TestArchiveOfAChildLeavesTheParent(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManagerWithNotifier(tmux, st, newNotifier(st))
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{child.ID}))

	live, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1, "only the child goes")
	assert.Equal(t, parent.ID, live[0].ID)
	assert.Contains(t, tmux.killedPanes, child.Pane)

	claimed, err := st.Mail.Claim(ctx, parent.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1, "the parent hears that what it was waiting on is gone")
	assert.Contains(t, claimed[0].Body, "archived")
}

// TestArchiveOfAParentTellsTheChildNothing pins that a child archived alongside its parent
// raises no lifecycle mail, since the only agent it could be addressed to is going too.
func TestArchiveOfAParentTellsTheChildNothing(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManagerWithNotifier(tmux, st, newNotifier(st))
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))

	claimed, err := st.Mail.Claim(ctx, parent.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)
}

// TestArchiveOfAParentTellsAThirdPartySpawner pins that an orchestrator which attached a child
// to some other agent still hears the child is gone, since it is the one waiting on its mail.
func TestArchiveOfAParentTellsAThirdPartySpawner(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManagerWithNotifier(tmux, st, newNotifier(st))
	ctx := t.Context()

	orchestrator := seedRunning(t, st, tmux, "orchestrator", "%orchestrator", agentstore.StateIdle)
	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	seedChildSpawnedBy(t, st, tmux, parent, orchestrator.ID)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))

	claimed, err := st.Mail.Claim(ctx, orchestrator.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Contains(t, claimed[0].Body, "archived")
}

// TestDeleteTakesTheChildren pins that deleting a parent removes the children too, and that
// their panes are dead before the parent's cleanup runs against the checkout they shared.
func TestDeleteTakesTheChildren(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{parent.ID}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "nothing is left of the family")
	assert.Contains(t, tmux.killedPanes, child.Pane)
}

// TestDeleteTakesAChildAlone pins that a child can be destroyed by itself, since it holds no
// directory of its own for the delete to tear down.
func TestDeleteTakesAChildAlone(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManagerWithNotifier(tmux, st, newNotifier(st))
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{child.ID}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the parent is left as it was")
	assert.Equal(t, parent.ID, all[0].ID)
	assert.Contains(t, tmux.killedPanes, child.Pane)
}

// TestRestoreBringsTheChildrenBack pins that a parent taken out of the archive arrives with
// the children it was archived with, rather than alone with rows left behind it.
func TestRestoreBringsTheChildrenBack(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{parent.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2)

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 2, "the family comes back together")

	assert.Equal(t, child.ID, agentByID(t, st, child.ID).ID)
	assert.Nil(t, agentByID(t, st, child.ID).ArchivedAt)
}

// TestRestoreOfAChildNeedsItsParentBack pins the one direction the family rule still holds in:
// a child comes back only once there is a live row for it to be drawn under.
func TestRestoreOfAChildNeedsItsParentBack(t *testing.T) {
	tests := []struct {
		name        string
		restoreBoth bool
		wantErrIs   error
	}{
		{
			name:        "the parent is live again",
			restoreBoth: true,
		},
		{
			name:      "the parent is still archived",
			wantErrIs: ErrParentStillArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManagerWithNotifier(tmux, st, newNotifier(st))
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			child := seedChild(t, st, tmux, parent)

			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))
			if tt.restoreBoth {
				_, err := manager.lifecycle.RestoreAgents(ctx, []string{parent.ID})
				require.NoError(t, err)
				require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{child.ID}))
			}

			restored, err := manager.lifecycle.RestoreAgents(ctx, []string{child.ID})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.NotNil(t, agentByID(t, st, child.ID).ArchivedAt, "the refusal restores nothing")

				return
			}

			require.NoError(t, err)
			assert.Len(t, restored, 1)
			assert.Nil(t, agentByID(t, st, child.ID).ArchivedAt)
		})
	}
}

// TestChildMailStaysInTheFamily pins who a child may reach: its parent and that parent's other
// children, and nobody else.
func TestChildMailStaysInTheFamily(t *testing.T) {
	tests := []struct {
		name      string
		from      string
		to        string
		wantErrIs error
	}{
		{
			name: "a child answers its parent",
			from: "child-one",
			to:   "parent",
		},
		{
			name: "a parent reaches its child",
			from: "parent",
			to:   "child-one",
		},
		{
			name: "one child reaches its sibling",
			from: "child-one",
			to:   "child-two",
		},
		{
			name:      "a child reaching an unrelated agent",
			from:      "child-one",
			to:        "stranger",
			wantErrIs: ErrOutsideFamily,
		},
		{
			name:      "a child reaching another parent's child",
			from:      "child-one",
			to:        "cousin",
			wantErrIs: ErrOutsideFamily,
		},
		{
			name:      "an unrelated agent reaching a child",
			from:      "stranger",
			to:        "child-one",
			wantErrIs: ErrOutsideFamily,
		},
		{
			name: "two agents that are nobody's children",
			from: "parent",
			to:   "stranger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailbox := &fakeMailbox{}
			svc := NewMailService(NewMailServiceParams{
				MailReadWriter: mailbox,
				AgentFinder:    newFakeFamily(),
				Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
			})

			sent, err := svc.SendMail(t.Context(), SendMailParams{
				From:      tt.from,
				ToAgentID: tt.to,
				Body:      "hello",
			})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Nil(t, sent)
				assert.Empty(t, mailbox.sent)

				return
			}

			require.NoError(t, err)
			require.Len(t, mailbox.sent, 1)
			assert.Equal(t, tt.to, mailbox.sent[0].ToAgentID)
		})
	}
}

// TestSystemMailReachesAChild pins that weave's own mail, and a person's, is not held to the
// family: these rules bound what a child may do rather than what may be done to it.
func TestSystemMailReachesAChild(t *testing.T) {
	mailbox := &fakeMailbox{}
	svc := NewMailService(NewMailServiceParams{
		MailReadWriter: mailbox,
		AgentFinder:    newFakeFamily(),
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})

	_, err := svc.SendSystemMail(t.Context(), "child-one", "your parent was archived")
	require.NoError(t, err)
	require.Len(t, mailbox.sent, 1)
	assert.Equal(t, mailstore.KindPlain, mailbox.sent[0].Kind)
	assert.Equal(t, WeaveSenderName, mailbox.sent[0].FromName)
}

// newFakeFamily is one parent with two children, an unrelated agent, and another parent's
// child, which is every side of the family rule.
func newFakeFamily() *fakeDirectory {
	parent := agentstore.Agent{ID: "parent", Name: "author"}
	other := agentstore.Agent{ID: "other-parent", Name: "other-author"}

	return &fakeDirectory{
		byID: map[string]agentstore.Agent{
			parent.ID: parent,
			other.ID:  other,
			"child-one": {
				ID:            "child-one",
				Name:          "reviewer",
				ParentAgentID: xptr.Ptr(parent.ID),
			},
			"child-two": {
				ID:            "child-two",
				Name:          "test-runner",
				ParentAgentID: xptr.Ptr(parent.ID),
			},
			"cousin": {
				ID:            "cousin",
				Name:          "their-reviewer",
				ParentAgentID: xptr.Ptr(other.ID),
			},
			"stranger": {ID: "stranger", Name: "unrelated"},
		},
	}
}

// TestChildBriefingReplacesTheDelegateOne pins what a child is told, since the rules it runs
// under are the reason it was made this way rather than as an agent of its own.
func TestChildBriefingReplacesTheDelegateOne(t *testing.T) {
	launcher := NewLauncher(LauncherParams{
		Tmux:    newFakeTmux(),
		Addr:    "127.0.0.1:9999",
		LogPath: briefedLogPath,
	})

	prompt := launcher.appendedSystemPrompt(launchParams{
		SpawnedByAgentID: "agent-parent",
		SpawnedByName:    "author",
		ParentAgentID:    "agent-parent",
		ParentName:       "author",
	})

	assert.Contains(t, prompt, "You are a child of agent author (agent-parent)")
	assert.Contains(t, prompt, "do not commit")
	assert.Contains(t, prompt, "you go when the parent goes")
	assert.NotContains(t, prompt, "You were spawned by agent",
		"the spawner paragraph would understate what a child runs under")
}
