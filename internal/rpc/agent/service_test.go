package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	agentv1weave "weave/proto/gen/agent/v1/agentv1weave"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/git"
	"weave/internal/logging"
	"weave/internal/logtest"
	"weave/internal/mail"
	"weave/internal/marks"
	"weave/internal/reviews"
	"weave/internal/rpc/render"
	"weave/internal/store"
	"weave/internal/storetest"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	paginationv1 "weave/proto/gen/pagination/v1"
	viewv1 "weave/proto/gen/view/v1"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	runstore "weave/internal/store/run"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const missingID = "00000000-0000-0000-0000-000000000000"

// TestCreateAgent pins what the handler passes down and that a launcher's refusal reaches the caller as a code, the mapping itself being rpcerr's own.
func TestCreateAgent(t *testing.T) {
	workflow := "wf-1"

	tests := []struct {
		name     string
		req      *agentv1.CreateAgentRequest
		launchEr error
		wantCode connect.Code
		check    func(t *testing.T, got agents.NewAgentParams)
	}{
		{
			name:     "a launch that cannot start is refused rather than reported as spawned",
			req:      &agentv1.CreateAgentRequest{Prompt: "go"},
			launchEr: errors.New("tmux is not running"),
			wantCode: connect.CodeInternal,
		},
		{
			name: "an omitted directory reaches the launcher empty, for the workflow to fill",
			req:  &agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: workflow},
			check: func(t *testing.T, got agents.NewAgentParams) {
				assert.Empty(t, got.Dir)
				assert.Equal(t, workflow, got.WorkflowID)
			},
		},
		{
			name: "every field reaches the launcher",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "audit the migrations",
				Title:      "audit",
				Dir:        "/repo",
				Flags:      "--model opus",
				WorkflowId: workflow,
			},
			check: func(t *testing.T, got agents.NewAgentParams) {
				assert.Equal(t, "audit the migrations", got.Prompt)
				assert.Equal(t, "audit", got.Title)
				assert.Equal(t, "/repo", got.Dir)
				assert.Equal(t, "--model opus", got.Flags)
				assert.Equal(t, workflow, got.WorkflowID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			launcher.err = tt.launchEr

			created, err := svc.CreateAgent(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "spawned", created.Msg.GetAgent().GetName())
			tt.check(t, launcher.got)
		})
	}
}

// TestRequestReviewPassesTheLaunchDown pins that what an asker says about its reviewer's CLI
// reaches the launcher rather than stopping at the handler.
func TestRequestReviewPassesTheLaunchDown(t *testing.T) {
	svc, _, launcher := serviceWithLauncher(t)

	_, err := svc.RequestReview(t.Context(), connect.NewRequest(&agentv1.RequestReviewRequest{
		Prompt:  "read the parser change",
		Title:   "second-reader",
		Harness: "codex",
		Flags:   "--sandbox read-only",
		AgentId: xptr.Ptr(missingID),
	}))
	require.NoError(t, err)

	assert.Equal(t, "read the parser change", launcher.reviewed.Prompt)
	assert.Equal(t, "second-reader", launcher.reviewed.Title)
	assert.Equal(t, "codex", launcher.reviewed.Harness)
	assert.Equal(t, "--sandbox read-only", launcher.reviewed.Flags)
	assert.Equal(t, missingID, launcher.reviewed.AgentID)
}

// TestAdopt pins what the handler passes down and that a launcher's refusal reaches the caller as a code, the mapping itself being rpcerr's own.
func TestAdopt(t *testing.T) {
	workflow := "wf-1"

	tests := []struct {
		name     string
		req      *agentv1.AdoptRequest
		adoptErr error
		wantCode connect.Code
		check    func(t *testing.T, got agents.AdoptParams)
	}{
		{
			name:     "a refusal is answered rather than reported as adopted",
			req:      &agentv1.AdoptRequest{Ref: "main", WorkflowId: workflow},
			adoptErr: agents.ErrAdoptNeedsWorktree,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "every field reaches the launcher",
			req: &agentv1.AdoptRequest{
				Ref:        "https://github.com/o/r/pull/1069",
				Prompt:     "pick it up",
				Title:      "logo",
				Flags:      "--model opus",
				Harness:    "claude",
				WorkflowId: workflow,
			},
			check: func(t *testing.T, got agents.AdoptParams) {
				assert.Equal(t, "https://github.com/o/r/pull/1069", got.Ref)
				assert.Equal(t, "pick it up", got.Prompt)
				assert.Equal(t, "logo", got.Title)
				assert.Equal(t, "--model opus", got.Flags)
				assert.Equal(t, "claude", got.Harness)
				assert.Equal(t, workflow, got.WorkflowID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			launcher.err = tt.adoptErr

			adopted, err := svc.Adopt(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "spawned", adopted.Msg.GetAgent().GetName())
			tt.check(t, launcher.adopted)
		})
	}
}

// TestGetAgent pins that an id nobody holds is not found, and that an agent that exists comes
// back with its cwd, its state, and a creation time rendered as RFC 3339.
func TestGetAgent(t *testing.T) {
	tests := []struct {
		name     string
		seed     string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an agent that exists",
			seed: "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			lookup := missingID
			if tt.seed != "" {
				lookup = st.SeedAgent(t, agentstore.Agent{
					Name:  tt.seed,
					Cwd:   "/tmp",
					State: agentstore.StateThinking,
				}).ID
			}

			got, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: lookup}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/tmp", got.Msg.GetAgent().GetCwd())
			assert.Equal(t, agentv1.AgentState_AGENT_STATE_THINKING, got.Msg.GetAgent().GetState())

			created := got.Msg.GetAgent().GetCreatedAt()
			require.NotEmpty(t, created, "the creation time is emitted")
			ts, err := time.Parse(time.RFC3339, created)
			require.NoError(t, err, "as RFC 3339")
			assert.WithinDuration(t, time.Now(), ts, time.Minute)
		})
	}
}

// TestListAgents covers the listing and its liveness flag.
func TestListAgents(t *testing.T) {
	tests := []struct {
		name  string
		seed  []string
		alive bool
	}{
		{
			name: "an empty store lists nothing",
		},
		{
			name:  "a dormant agent reports itself as such",
			seed:  []string{"alpha"},
			alive: false,
		},
		{
			name:  "a live agent is flagged alive",
			seed:  []string{"alpha", "beta"},
			alive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, launcher := serviceWithLauncher(t)
			launcher.alive = tt.alive

			for _, name := range tt.seed {
				st.SeedAgent(t, agentstore.Agent{
					Name:  name,
					Pane:  "%1",
					State: agentstore.StateThinking,
				})
			}

			listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetAgents(), len(tt.seed))

			for _, s := range listed.Msg.GetAgents() {
				assert.Equal(t, tt.alive, s.GetAlive())
			}

			assert.Equal(t, 1, launcher.runningCalls, "liveness is read once for the page, not once per agent")
		})
	}
}

// TestListAgentsReportsUnreadMail covers the unread flag.
func TestListAgentsReportsUnreadMail(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	_, err := svc.SendMail(t.Context(), connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "refunded_cents",
	}))
	require.NoError(t, err)

	listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetAgents(), 2)

	unread := make(map[string]bool, 2)
	for _, a := range listed.Msg.GetAgents() {
		unread[a.GetId()] = a.GetUnreadMail()
	}
	assert.False(t, unread[alpha.ID], "alpha sent the mail, it did not receive any")
	assert.True(t, unread[beta.ID], "beta has not read what alpha sent")
}

// TestListAgentsReportsAFailedRun covers the mark a failed script puts on an agent's row, which
// is the whole reason the runs are recorded rather than only logged.
func TestListAgentsReportsAFailedRun(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	failed, err := st.Runs.Begin(t.Context(), runstore.BeginParams{
		AgentID:   beta.ID,
		Trigger:   "post-launch",
		Script:    "install-deps",
		LogNumber: xptr.Ptr(int32(4)),
	})
	require.NoError(t, err)
	require.NoError(t, st.Runs.Finish(t.Context(), runstore.FinishParams{ID: failed, Failure: "a hook failed"}))

	fine, err := st.Runs.Begin(t.Context(), runstore.BeginParams{
		AgentID: alpha.ID,
		Trigger: "post-launch",
		Script:  "install-deps",
	})
	require.NoError(t, err)
	require.NoError(t, st.Runs.Finish(t.Context(), runstore.FinishParams{ID: fine, ExitCode: xptr.Ptr(int32(0))}))

	flags := func() map[string]bool {
		listed, listErr := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
		require.NoError(t, listErr)
		require.Len(t, listed.Msg.GetAgents(), 2)

		held := make(map[string]bool, 2)
		for _, a := range listed.Msg.GetAgents() {
			held[a.GetId()] = a.GetFailedRun()
		}

		return held
	}

	held := flags()
	assert.False(t, held[alpha.ID], "a run that succeeded is nothing to read")
	assert.True(t, held[beta.ID])

	got, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: beta.ID}))
	require.NoError(t, err)
	assert.True(t, got.Msg.GetAgent().GetFailedRun(), "the single lookup reads it the same way")
	assert.Equal(t, int32(4), got.Msg.GetAgent().GetFailedRunLog(), "and names the run to open on")

	_, err = svc.MarkRunsRead(t.Context(), connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: beta.ID}))
	require.NoError(t, err)

	assert.False(t, flags()[beta.ID], "looking at the run log clears the mark")
}

// TestGetAgentReportsUnreadMail covers the single-agent lookup reading the mailbox the same
// way the listing does.
func TestGetAgentReportsUnreadMail(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	_, err := svc.SendMail(t.Context(), connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "refunded_cents",
	}))
	require.NoError(t, err)

	got, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: beta.ID}))
	require.NoError(t, err)
	assert.True(t, got.Msg.GetAgent().GetUnreadMail())

	got, err = svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: alpha.ID}))
	require.NoError(t, err)
	assert.False(t, got.Msg.GetAgent().GetUnreadMail())
}

// TestListAgentsIdempotencyKey covers the creator's key reaching the listing.
func TestListAgentsIdempotencyKey(t *testing.T) {
	tests := []struct {
		name string
		key  *string
		want string
	}{
		{
			name: "an agent created without a claim carries no key",
			want: "",
		},
		{
			name: "a claimed agent reports the key it was created under",
			key:  xptr.Ptr("triage:ENG-5260:2026-07-29T15:30:00Z"),
			want: "triage:ENG-5260:2026-07-29T15:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			st.SeedAgent(t, agentstore.Agent{
				Name:           "alpha",
				Pane:           "%1",
				State:          agentstore.StateThinking,
				IdempotencyKey: tt.key,
			})

			listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetAgents(), 1)

			assert.Equal(t, tt.want, listed.Msg.GetAgents()[0].GetIdempotencyKey())
		})
	}
}

// TestListAgentsArchived covers the flag that swaps which set is listed.
func TestListAgentsArchived(t *testing.T) {
	tests := []struct {
		name           string
		archived       bool
		wantNames      []string
		wantArchivedAt bool
	}{
		{
			name:      "the default listing is the live agents",
			wantNames: []string{"live"},
		},
		{
			name:           "asking for the archived ones excludes the live",
			archived:       true,
			wantNames:      []string{"archived"},
			wantArchivedAt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, _ := serviceWithLauncher(t)
			ctx := t.Context()

			archived := st.SeedAgent(t, agentstore.Agent{
				Name:  "archived",
				Pane:  "%1",
				State: agentstore.StateThinking,
			})
			_, archiveErr := st.Agents.Archive(ctx, []string{archived.ID})
			require.NoError(t, archiveErr)
			st.SeedAgent(t, agentstore.Agent{
				Name:  "live",
				Pane:  "%2",
				State: agentstore.StateThinking,
			})

			listed, err := svc.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Archived: tt.archived}))
			require.NoError(t, err)

			names := make([]string, 0, len(listed.Msg.GetAgents()))
			for _, agent := range listed.Msg.GetAgents() {
				names = append(names, agent.GetName())
			}
			assert.Equal(t, tt.wantNames, names)

			for _, agent := range listed.Msg.GetAgents() {
				if !tt.wantArchivedAt {
					assert.Nil(t, agent.ArchivedAt, "a live agent carries no archive time")

					continue
				}

				require.NotNil(t, agent.ArchivedAt, "an archived agent says when it was archived")
				_, err := time.Parse(time.RFC3339, agent.GetArchivedAt())
				require.NoError(t, err, "and says it as an RFC 3339 timestamp")
			}
		})
	}
}

// TestUpdateAgent covers renaming by id, where a name another agent holds comes back as a
// conflict rather than as the driver's own message.
func TestUpdateAgent(t *testing.T) {
	tests := []struct {
		name     string
		seed     []string
		rename   string
		to       string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			to:       "whatever",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a name a live agent already holds",
			seed:     []string{"alpha", "beta"},
			rename:   "beta",
			to:       "alpha",
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:   "a free name is taken and the id is kept",
			seed:   []string{"alpha"},
			rename: "alpha",
			to:     "renamed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			workflow := st.SeedWorkflow(t, workflowstore.Config{Name: "backend"})

			id := "ghost"
			for _, name := range tt.seed {
				agent := st.SeedAgent(t, agentstore.Agent{Name: name, WorkflowID: workflow.ID})
				if name == tt.rename {
					id = agent.ID
				}
			}

			_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{Id: id, Name: tt.to}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				assert.NotContains(t, err.Error(), "constraint", "the driver's message stays server-side")

				return
			}

			require.NoError(t, err)

			renamed, err := st.LiveAgentByName(ctx, tt.to)
			require.NoError(t, err)
			require.NotNil(t, renamed)
			assert.Equal(t, id, renamed.ID, "a rename preserves the id")

			_, err = st.LiveAgentByName(ctx, tt.rename)
			require.ErrorIs(t, err, store.ErrNotFound, "the old name no longer finds anything")
		})
	}
}

// TestSetHidden covers hiding over the wire, which must land on the agent the caller named
// and refuse an id nobody holds.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name      string
		seed      bool
		hideFirst bool
		hidden    bool
		wantCode  connect.Code
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			hidden:   true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "an agent is hidden",
			seed:   true,
			hidden: true,
		},
		{
			name:      "and brought back again",
			seed:      true,
			hideFirst: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID
			}
			if tt.hideFirst {
				changed, err := st.Agents.SetHidden(ctx, id, true)
				require.NoError(t, err)
				require.True(t, changed)
			}

			_, err := svc.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: id, Hidden: tt.hidden}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
			require.NoError(t, err)
			assert.Equal(t, tt.hidden, got.Msg.GetAgent().GetHidden())
		})
	}
}

// TestSetMark covers the mark over the wire, across both kinds of row, where a call that
// reached nothing live is refused and one that took the letter off another row is not.
func TestSetMark(t *testing.T) {
	t.Run("an id nobody holds is not a quiet success", func(t *testing.T) {
		svc, _ := service(t)

		_, err := svc.SetMark(t.Context(), connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: uuid.NewString()},
			Mark:   xptr.Ptr("a"),
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("an agent takes a letter and reads it back off its row", func(t *testing.T) {
		svc, st := service(t)
		ctx := t.Context()
		id := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID

		_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: id},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		assert.Equal(t, xptr.Ptr("a"), got.Msg.GetAgent().Mark)
	})

	t.Run("a letter an agent holds moves to the workflow named", func(t *testing.T) {
		svc, st := service(t)
		ctx := t.Context()
		held := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})

		_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: held.ID},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		_, err = svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_WorkflowId{WorkflowId: held.WorkflowID},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		gone, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: held.ID}))
		require.NoError(t, err)
		assert.Nil(t, gone.Msg.GetAgent().Mark, "the agent that held it is left with nothing")
	})

	t.Run("an unset mark clears the one the row carries", func(t *testing.T) {
		svc, st := service(t)
		ctx := t.Context()
		id := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID

		_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: id},
			Mark:   xptr.Ptr("a"),
		}))
		require.NoError(t, err)

		_, err = svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
			Target: &agentv1.SetMarkRequest_AgentId{AgentId: id},
		}))
		require.NoError(t, err)

		got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		assert.Nil(t, got.Msg.GetAgent().Mark)
	})
}

// TestSetMetadata covers an agent recording a fact about its own work and reading it back off the
// resource.
func TestSetMetadata(t *testing.T) {
	tests := []struct {
		name        string
		seed        bool
		recordFirst []agentstore.Metadata
		key         string
		value       *string
		wantCode    connect.Code
		wantStored  []agentstore.Metadata
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			key:      "linear_issue",
			value:    xptr.Ptr("https://linear.app/x/issue/ENG-1"),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "the pull request key is refused, since the forge fills it",
			seed:     true,
			key:      "pr",
			value:    xptr.Ptr("https://github.com/o/r/pull/1"),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "clearing the pull request key is refused too",
			seed:     true,
			key:      "pr",
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "clearing a key nothing held is served, not refused",
			seed: true,
			key:  "linear_issue",
		},
		{
			name:  "an agent records a fact",
			seed:  true,
			key:   "linear_issue",
			value: xptr.Ptr("https://linear.app/x/issue/ENG-1"),
			wantStored: []agentstore.Metadata{
				{Key: "linear_issue", Value: "https://linear.app/x/issue/ENG-1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID
			}
			for _, entry := range tt.recordFirst {
				changed, err := st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{
					ID:    id,
					Key:   entry.Key,
					Value: xptr.Ptr(entry.Value),
				})
				require.NoError(t, err)
				require.True(t, changed)
			}

			_, err := svc.SetMetadata(ctx, connect.NewRequest(&agentv1.SetMetadataRequest{
				Id:    id,
				Key:   tt.key,
				Value: tt.value,
			}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
			require.NoError(t, err)
			assert.Equal(t, tt.wantStored, storedMetadata(got.Msg.GetAgent()))
		})
	}
}

// TestLog covers the channel a running script uses to say what it did, and the refusal of an
// id nobody holds.
func TestLog(t *testing.T) {
	tests := []struct {
		name     string
		seed     bool
		explicit bool
		callerIs bool
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			explicit: true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an explicit id is used as given",
			seed:     true,
			explicit: true,
		},
		{
			name:     "an unset id falls back to the caller's own",
			seed:     true,
			callerIs: true,
		},
		{
			name:     "an unset id with no caller identity is refused",
			seed:     true,
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{
					Name:  "alpha",
					Cwd:   "/repo",
					State: agentstore.StateThinking,
				}).ID
			}

			ctx := t.Context()
			if tt.callerIs {
				ctx = caller.With(ctx, caller.Caller{AgentID: id})
			}

			var written strings.Builder
			ctx = logging.WithLogger(ctx, logging.New(&written, slog.LevelInfo))

			req := &agentv1.LogRequest{Message: "fetched origin/main"}
			if tt.explicit {
				req.Id = &id
			}

			_, err := svc.Log(ctx, connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			record := logtest.Record(t, written.String())
			assert.Equal(t, "fetched origin/main", record["body"])
			assert.Equal(t, id, record[logging.TargetAgentID])
			assert.Equal(t, "/repo", record[logging.ScriptAgentCwd])
			assert.NotContains(t, record, logging.ScriptName, "no script was given")
		})
	}
}

type fakeReconciler struct {
	recorded []string
	reasons  []string
}

func (f *fakeReconciler) RecordTurnBoundary(agentID string) {
	f.recorded = append(f.recorded, agentID)
}

func (f *fakeReconciler) RecordReportedReason(_, reason string) {
	f.reasons = append(f.reasons, reason)
}

// TestHoldAgentRefusesHoldingYourself pins that an agent asking to hold itself is turned away
// before the interrupt.
func TestHoldAgentRefusesHoldingYourself(t *testing.T) {
	svc, st := service(t)
	holder := &fakeHolder{}
	svc.Impl.(*AgentService).holder = holder

	mine := st.SeedAgent(t, agentstore.Agent{Name: "mine", State: agentstore.StateThinking}).ID
	theirs := st.SeedAgent(t, agentstore.Agent{Name: "theirs", State: agentstore.StateIdle}).ID
	ctx := caller.With(t.Context(), caller.Caller{AgentID: mine})

	_, err := svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: mine}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Empty(t, holder.held, "the holder is never reached, so no interrupt is sent")

	_, err = svc.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: theirs}))
	require.NoError(t, err, "but holding somebody else is untouched")
	assert.Equal(t, []string{theirs}, holder.held)

	_, err = svc.HoldAgent(t.Context(), connect.NewRequest(&agentv1.HoldAgentRequest{Id: mine}))
	require.NoError(t, err, "and a person, who carries no agent id, may hold anything")
	assert.Equal(t, []string{theirs, mine}, holder.held)
}

type fakeHolder struct {
	held []string
}

func (f *fakeHolder) Hold(_ context.Context, agentID string) (*agentstore.Agent, error) {
	f.held = append(f.held, agentID)

	return &agentstore.Agent{ID: agentID, Name: "held"}, nil
}

func (f *fakeHolder) Release(_ context.Context, params agents.ReleaseParams) (*agentstore.Agent, error) {
	return &agentstore.Agent{ID: params.AgentID}, nil
}

// TestRecordReportRefusesACallerWithNoIdentity pins that a hook that cannot say which agent it
// speaks for is turned away rather than stamping somebody.
func TestRecordReportRefusesACallerWithNoIdentity(t *testing.T) {
	reconciler := &fakeReconciler{}
	svc := agentv1weave.AgentServiceAdapter{Impl: NewAgentService(NewAgentServiceParams{Writer: rowWriterOver(nil, reconciler)})}

	_, err := svc.RecordReport(t.Context(), connect.NewRequest(&agentv1.RecordReportRequest{TurnEnded: true}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Empty(t, reconciler.recorded)
	assert.Empty(t, reconciler.reasons)
}

// TestRecordReportTellsTheReconcilerAboutTheTurn pins the two things one reported turn end does:
// the reconciler hears about the boundary, and the reason lands on the row.
func TestRecordReportTellsTheReconcilerAboutTheTurn(t *testing.T) {
	svc, st := service(t)
	reconciler := &fakeReconciler{}
	svc.Impl.(*AgentService).writer = rowWriterOver(st, reconciler)

	id := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateIdle}).ID

	ctx := caller.With(t.Context(), caller.Caller{AgentID: id})
	_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{TurnEnded: true}))
	require.NoError(t, err)

	assert.Equal(t, []string{id}, reconciler.recorded)
	assert.Equal(t, []string{""}, reconciler.reasons)

	stamped, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	assert.NotNil(t, stamped.TurnEndedAt)
}

// TestRecordReportKeepsTheReasonWithoutEndingTheTurn covers the tier every event but a turn end
// lands in, where the reason goes on the row and nothing about the turn is claimed.
func TestRecordReportKeepsTheReasonWithoutEndingTheTurn(t *testing.T) {
	svc, st := service(t)
	reconciler := &fakeReconciler{}
	svc.Impl.(*AgentService).writer = rowWriterOver(st, reconciler)

	id := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID
	ctx := caller.With(t.Context(), caller.Caller{AgentID: id})

	_, err := svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{Reason: "Bash"}))
	require.NoError(t, err)

	reported, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
	require.NoError(t, err)
	assert.Equal(t, "Bash", reported.Msg.GetAgent().GetReportedReason())
	assert.Empty(t, reconciler.recorded)
	assert.Equal(t, []string{"Bash"}, reconciler.reasons)

	unstamped, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	assert.Nil(t, unstamped.TurnEndedAt, "nothing reported a turn ending")

	_, err = svc.RecordReport(ctx, connect.NewRequest(&agentv1.RecordReportRequest{TurnEnded: true}))
	require.NoError(t, err)

	cleared, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
	require.NoError(t, err)
	assert.Empty(t, cleared.Msg.GetAgent().GetReportedReason(), "a turn ending leaves nothing to say the agent is doing")
}

// TestSendMail pins the code each way of addressing mail wrongly is answered with, and that a
// name reaches its holder as an id does.
func TestSendMail(t *testing.T) {
	tests := []struct {
		name     string
		to       string
		callerIs string
		wantCode connect.Code
	}{
		{
			name:     "an id no agent holds",
			to:       missingID,
			callerIs: "alpha",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an agent addressing itself",
			to:       "alpha",
			callerIs: "alpha",
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a caller claiming an agent that does not exist",
			to:       "beta-id",
			callerIs: uuid.NewString(),
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an agent messaging another by id",
			to:       "beta-id",
			callerIs: "alpha",
		},
		{
			name:     "an agent messaging another by name",
			to:       "beta",
			callerIs: "alpha",
		},
		{
			name: "the panel messaging an agent as nobody",
			to:   "beta-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			alpha := st.SeedAgent(t, agentstore.Agent{
				Name:  "alpha",
				Cwd:   "/repo",
				State: agentstore.StateIdle,
			})
			beta := st.SeedAgent(t, agentstore.Agent{
				Name:  "beta",
				Cwd:   "/repo",
				State: agentstore.StateIdle,
			})

			to := tt.to
			switch to {
			case "alpha":
				to = alpha.ID
			case "beta-id":
				to = beta.ID
			}

			ctx := t.Context()
			switch tt.callerIs {
			case "":
			case "alpha":
				ctx = caller.With(ctx, caller.Caller{AgentID: alpha.ID})
			default:
				ctx = caller.With(ctx, caller.Caller{AgentID: tt.callerIs})
			}

			sent, err := svc.SendMail(ctx, connect.NewRequest(&agentv1.SendMailRequest{
				To:   to,
				Body: "which column holds the refund?",
			}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, beta.ID, sent.Msg.GetMail().GetToAgentId())
			assert.Equal(t, "which column holds the refund?", sent.Msg.GetMail().GetBody())

			if tt.callerIs == "alpha" {
				assert.Equal(t, alpha.ID, sent.Msg.GetMail().GetFromAgentId())
				assert.Equal(t, "alpha", sent.Msg.GetMail().GetFromName())

				return
			}

			assert.Empty(t, sent.Msg.GetMail().GetFromAgentId())
		})
	}
}

// TestSendMailByName pins that a name is looked for in the sender's own workflow before every
// workflow, and that one held by two other workflows is refused naming both holders.
func TestSendMailByName(t *testing.T) {
	svc, st := service(t)
	chariot := st.SeedWorkflow(t, workflowstore.Config{Name: "chariot"})
	weave := st.SeedWorkflow(t, workflowstore.Config{Name: "weave"})
	docs := st.SeedWorkflow(t, workflowstore.Config{Name: "docs"})

	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:       "alpha",
		Cwd:        "/repo",
		WorkflowID: chariot.ID,
	})
	reviewerHere := st.SeedAgent(t, agentstore.Agent{
		Name:       "reviewer",
		Cwd:        "/repo",
		WorkflowID: chariot.ID,
	})
	st.SeedAgent(t, agentstore.Agent{
		Name:       "reviewer",
		Cwd:        "/repo",
		WorkflowID: weave.ID,
	})
	testerWeave := st.SeedAgent(t, agentstore.Agent{
		Name:       "tester",
		Cwd:        "/repo",
		WorkflowID: weave.ID,
	})
	testerDocs := st.SeedAgent(t, agentstore.Agent{
		Name:       "tester",
		Cwd:        "/repo",
		WorkflowID: docs.ID,
	})

	asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})

	sent, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   "reviewer",
		Body: "which column holds the refund?",
	}))
	require.NoError(t, err)
	assert.Equal(t, reviewerHere.ID, sent.Msg.GetMail().GetToAgentId())

	_, err = svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   "tester",
		Body: "does the suite pass?",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), testerWeave.ID+" in workflow \"weave\"")
	assert.Contains(t, err.Error(), testerDocs.ID+" in workflow \"docs\"")

	_, err = st.Agents.Archive(t.Context(), []string{testerWeave.ID})
	require.NoError(t, err)

	sent, err = svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   "tester",
		Body: "does the suite pass?",
	}))
	require.NoError(t, err)
	assert.Equal(t, testerDocs.ID, sent.Msg.GetMail().GetToAgentId())
}

// TestSendMailWarnsOfADormantRecipient pins that a send to an agent nothing runs behind says so.
func TestSendMailWarnsOfADormantRecipient(t *testing.T) {
	tests := []struct {
		name        string
		alive       bool
		state       agentstore.State
		wantWarning bool
	}{
		{
			name:        "an idle agent whose pane is gone",
			alive:       false,
			state:       agentstore.StateIdle,
			wantWarning: true,
		},
		{
			name:        "an agent still being set up has no pane yet",
			alive:       false,
			state:       agentstore.StateSettingUp,
			wantWarning: false,
		},
		{
			name:        "an idle agent in its pane",
			alive:       true,
			state:       agentstore.StateIdle,
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, launcher := serviceWithLauncher(t)
			launcher.alive = tt.alive
			alpha := st.SeedAgent(t, agentstore.Agent{
				Name:  "alpha",
				Cwd:   "/repo",
				State: agentstore.StateIdle,
			})
			beta := st.SeedAgent(t, agentstore.Agent{
				Name:  "beta",
				Cwd:   "/repo",
				State: tt.state,
			})

			asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})
			sent, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
				To:   beta.ID,
				Body: "which column holds the refund?",
			}))
			require.NoError(t, err)
			assert.Equal(t, beta.ID, sent.Msg.GetMail().GetToAgentId())

			if !tt.wantWarning {
				assert.Nil(t, sent.Msg.Warning)

				return
			}

			require.NotNil(t, sent.Msg.Warning)
			assert.Contains(t, sent.Msg.GetWarning(), "beta")
			assert.Contains(t, sent.Msg.GetWarning(), "dormant")
		})
	}
}

// TestSendMailPastTheExchangeLimitIsResourceExhausted pins that a pair that has written each
// other as much as the configured limit allows is refused rather than allowed to keep going.
func TestSendMailPastTheExchangeLimitIsResourceExhausted(t *testing.T) {
	st := storetest.Open(t)
	mailer := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: st.Mail,
		Agents:         st.Agents,
		Config:         fakeMailConfig(1),
	})
	liveness := &fakeLauncher{}
	svc := agentv1weave.AgentServiceAdapter{Impl: NewAgentService(NewAgentServiceParams{
		Agents:      st.Agents,
		Rows:        rowReaderOver(st, liveness, fakeTrees{}),
		Transcripts: st.Transcripts,
		Mail:        mailer,
		Liveness:    liveness,
		Marks:       markService(st),
	})}

	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})
	first, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "which column?",
	}))
	require.NoError(t, err)

	asBeta := caller.With(t.Context(), caller.Caller{AgentID: beta.ID})
	replyTo := first.Msg.GetMail().GetId()
	_, err = svc.SendMail(asBeta, connect.NewRequest(&agentv1.SendMailRequest{
		To:      alpha.ID,
		Body:    "refunded_cents",
		ReplyTo: &replyTo,
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
	require.ErrorIs(t, err, mail.ErrExchangeCapped)
}

// TestReadMail pins that an agent reading its own mailbox is handed what is waiting there.
func TestReadMail(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})
	_, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "refunded_cents",
	}))
	require.NoError(t, err)

	asBeta := caller.With(t.Context(), caller.Caller{AgentID: beta.ID})
	read, err := svc.ReadMail(asBeta, connect.NewRequest(&agentv1.ReadMailRequest{}))
	require.NoError(t, err)
	require.Len(t, read.Msg.GetMail(), 1)
	assert.Equal(t, "refunded_cents", read.Msg.GetMail()[0].GetBody())
	assert.Equal(t, "alpha", read.Msg.GetMail()[0].GetFromName())

	again, err := svc.ReadMail(asBeta, connect.NewRequest(&agentv1.ReadMailRequest{}))
	require.NoError(t, err)
	assert.Empty(t, again.Msg.GetMail(), "claimed mail is not handed out twice")
}

// TestReadMailScope pins which mailbox a read lands on, and that neither a missing id nor an
// id nobody holds passes quietly.
func TestReadMailScope(t *testing.T) {
	tests := []struct {
		name     string
		explicit bool
		callerIs bool
		wantCode connect.Code
	}{
		{
			name:     "no id and no caller identity is refused",
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an id nobody holds is not a quiet success",
			explicit: true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an unset id falls back to the caller's own",
			callerIs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			seeded := st.SeedAgent(t, agentstore.Agent{
				Name:  "alpha",
				Cwd:   "/repo",
				State: agentstore.StateIdle,
			})

			ctx := t.Context()
			if tt.callerIs {
				ctx = caller.With(ctx, caller.Caller{AgentID: seeded.ID})
			}

			req := &agentv1.ReadMailRequest{}
			if tt.explicit {
				ghost := uuid.NewString()
				req.Id = &ghost
			}

			read, err := svc.ReadMail(ctx, connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Empty(t, read.Msg.GetMail())
		})
	}
}

// TestCrossAgentMailReadIsRecorded pins that reading somebody else's mailbox is written to
// the log naming whose it was.
func TestCrossAgentMailReadIsRecorded(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	var written strings.Builder
	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
	ctx = caller.With(ctx, caller.Caller{AgentID: alpha.ID})

	_, err := svc.ReadMail(ctx, connect.NewRequest(&agentv1.ReadMailRequest{Id: &beta.ID}))
	require.NoError(t, err)

	record := logtest.Record(t, written.String())
	assert.Equal(t, "agent read another agent's mail", record["body"])
	assert.Equal(t, beta.ID, record[logging.TargetAgentID])
}

// TestListMail pins that a listing shows what is in the mailbox without claiming it.
func TestListMail(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})
	_, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "refunded_cents",
	}))
	require.NoError(t, err)

	asBeta := caller.With(t.Context(), caller.Caller{AgentID: beta.ID})
	listed, err := svc.ListMail(asBeta, connect.NewRequest(&agentv1.ListMailRequest{}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetMail(), 1)
	assert.Equal(t, "refunded_cents", listed.Msg.GetMail()[0].GetBody())
	assert.Nil(t, listed.Msg.GetMail()[0].ClaimedAt, "listing does not claim the mail")

	again, err := svc.ListMail(asBeta, connect.NewRequest(&agentv1.ListMailRequest{}))
	require.NoError(t, err)
	require.Len(t, again.Msg.GetMail(), 1, "listing twice still finds the mail, unlike reading")
}

// TestListMailDirection pins which half of an agent's correspondence a listing reaches, the
// default being its mailbox and an explicit direction what it sent.
func TestListMailDirection(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})
	_, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "what alpha said",
	}))
	require.NoError(t, err)

	asBeta := caller.With(t.Context(), caller.Caller{AgentID: beta.ID})
	_, err = svc.SendMail(asBeta, connect.NewRequest(&agentv1.SendMailRequest{
		To:   alpha.ID,
		Body: "what beta said",
	}))
	require.NoError(t, err)

	tests := []struct {
		name      string
		direction agentv1.MailDirection
		want      string
	}{
		{
			name: "an unset direction reads the mailbox",
			want: "what beta said",
		},
		{
			name:      "received reads the mailbox too",
			direction: agentv1.MailDirection_MAIL_DIRECTION_RECEIVED,
			want:      "what beta said",
		},
		{
			name:      "sent reads what the agent wrote",
			direction: agentv1.MailDirection_MAIL_DIRECTION_SENT,
			want:      "what alpha said",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed, err := svc.ListMail(asAlpha, connect.NewRequest(&agentv1.ListMailRequest{
				Direction: tt.direction,
			}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetMail(), 1)
			assert.Equal(t, tt.want, listed.Msg.GetMail()[0].GetBody())
		})
	}
}

// TestListMailScope pins which mailbox a listing lands on, and that neither a missing id nor
// an id nobody holds passes quietly.
func TestListMailScope(t *testing.T) {
	tests := []struct {
		name     string
		explicit bool
		callerIs bool
		wantCode connect.Code
	}{
		{
			name:     "no id and no caller identity is refused",
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an id nobody holds is not a quiet success",
			explicit: true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an unset id falls back to the caller's own",
			callerIs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			seeded := st.SeedAgent(t, agentstore.Agent{
				Name:  "alpha",
				Cwd:   "/repo",
				State: agentstore.StateIdle,
			})

			ctx := t.Context()
			if tt.callerIs {
				ctx = caller.With(ctx, caller.Caller{AgentID: seeded.ID})
			}

			req := &agentv1.ListMailRequest{}
			if tt.explicit {
				ghost := uuid.NewString()
				req.Id = &ghost
			}

			listed, err := svc.ListMail(ctx, connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Empty(t, listed.Msg.GetMail())
		})
	}
}

// TestCrossAgentMailListIsRecorded pins that listing somebody else's mailbox is written to
// the log naming whose it was.
func TestCrossAgentMailListIsRecorded(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	var written strings.Builder
	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
	ctx = caller.With(ctx, caller.Caller{AgentID: alpha.ID})

	_, err := svc.ListMail(ctx, connect.NewRequest(&agentv1.ListMailRequest{Id: &beta.ID}))
	require.NoError(t, err)

	record := logtest.Record(t, written.String())
	assert.Equal(t, "agent listed another agent's mail", record["body"])
	assert.Equal(t, beta.ID, record[logging.TargetAgentID])
}

// TestDeleteMail pins that deleted mail is gone from the mailbox it was addressed to.
func TestDeleteMail(t *testing.T) {
	svc, st := service(t)
	alpha := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})
	beta := st.SeedAgent(t, agentstore.Agent{
		Name:  "beta",
		Cwd:   "/repo",
		State: agentstore.StateIdle,
	})

	asAlpha := caller.With(t.Context(), caller.Caller{AgentID: alpha.ID})
	sent, err := svc.SendMail(asAlpha, connect.NewRequest(&agentv1.SendMailRequest{
		To:   beta.ID,
		Body: "refunded_cents",
	}))
	require.NoError(t, err)

	_, err = svc.DeleteMail(t.Context(), connect.NewRequest(&agentv1.DeleteMailRequest{
		Ids: []string{sent.Msg.GetMail().GetId()},
	}))
	require.NoError(t, err)

	asBeta := caller.With(t.Context(), caller.Caller{AgentID: beta.ID})
	read, err := svc.ReadMail(asBeta, connect.NewRequest(&agentv1.ReadMailRequest{}))
	require.NoError(t, err)
	assert.Empty(t, read.Msg.GetMail(), "deleted mail is never delivered")
}

// TestDeleteMailUnknownIDIsNotFound pins that a delete naming mail nobody holds is refused
// rather than reported as done.
func TestDeleteMailUnknownIDIsNotFound(t *testing.T) {
	svc, _ := service(t)

	_, err := svc.DeleteMail(t.Context(), connect.NewRequest(&agentv1.DeleteMailRequest{
		Ids: []string{uuid.NewString()},
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestLogTagsTheScript covers the script field.
func TestLogTagsTheScript(t *testing.T) {
	svc, st := service(t)
	id := st.SeedAgent(t, agentstore.Agent{
		Name:  "alpha",
		Cwd:   "/repo",
		State: agentstore.StateThinking,
	}).ID

	var written strings.Builder
	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))

	script := "create-worktree"
	_, err := svc.Log(ctx, connect.NewRequest(&agentv1.LogRequest{
		Id:      &id,
		Message: "preparing",
		Script:  &script,
	}))
	require.NoError(t, err)

	record := logtest.Record(t, written.String())
	assert.Equal(t, "create-worktree", record[logging.ScriptName])
}

// allSearchHits flattens both sections of a response, for the tests that care which turns
// came back rather than which way each one matched.
func allSearchHits(msg *agentv1.SearchTranscriptsResponse) []*agentv1.SearchHit {
	return append(append([]*agentv1.SearchHit{}, msg.GetMatches()...), msg.GetNear()...)
}

// TestSearchTranscripts covers every field on a hit.
func TestSearchTranscripts(t *testing.T) {
	tests := []struct {
		name     string
		turns    []string
		archive  bool
		query    string
		wantSeqs []int32
	}{
		{
			name:  "a query nothing said matches nothing",
			turns: []string{"talking about postgres"},
			query: "kubernetes",
		},
		{
			name:     "a hit names the turn that matched rather than the first one",
			turns:    []string{"opening question", "the reconciler derives its markers"},
			query:    "reconciler",
			wantSeqs: []int32{1},
		},
		{
			name:     "an archived agent is still searchable, and says so",
			turns:    []string{"the reconciler derives its markers"},
			archive:  true,
			query:    "reconciler",
			wantSeqs: []int32{0},
		},
		{
			name:     "a subject raised twice is a hit per turn",
			turns:    []string{"first on postgres", "later on postgres again"},
			query:    "postgres",
			wantSeqs: []int32{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
			content := seedTranscript(t, st, agent.ID, tt.turns)
			if tt.archive {
				_, archiveErr := st.Agents.Archive(ctx, []string{agent.ID})
				require.NoError(t, archiveErr)
			}

			found, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
				Query: tt.query,
			}))
			require.NoError(t, err)
			require.Len(t, allSearchHits(found.Msg), len(tt.wantSeqs))

			seqs := make([]int32, 0, len(tt.wantSeqs))
			for _, hit := range allSearchHits(found.Msg) {
				seqs = append(seqs, hit.GetSeq())

				assert.Equal(t, agent.ID, hit.GetAgentId())
				assert.Equal(t, "alpha", hit.GetName(), "the hit carries the agent's name, not its id alone")
				assert.Equal(t, tt.archive, hit.GetArchived())
				assert.Equal(t, "user", hit.GetRole())
				assert.Contains(t, hit.GetSnippet(), tt.query, "the snippet holds the text that matched")

				turn := tt.turns[hit.GetSeq()]
				assert.Equal(t, turn, content[hit.GetContentOffset():hit.GetContentOffset()+int64(len(turn))])
			}

			assert.ElementsMatch(t, tt.wantSeqs, seqs)
		})
	}
}

// TestSearchTranscriptsScope covers who a search reaches, which the handler works out from
// the caller rather than from the request alone.
func TestSearchTranscriptsScope(t *testing.T) {
	tests := []struct {
		name      string
		callerIs  string
		asksFor   string
		wantNames []string
	}{
		{
			name:      "an agent that names nobody reads its own conversation",
			callerIs:  "alpha",
			wantNames: []string{"alpha"},
		},
		{
			name:      "an agent that names another reads that one",
			callerIs:  "alpha",
			asksFor:   "beta",
			wantNames: []string{"beta"},
		},
		{
			name:      "an agent that names itself is where it started",
			callerIs:  "alpha",
			asksFor:   "alpha",
			wantNames: []string{"alpha"},
		},
		{
			name:      "the panel is not an agent and reads every conversation",
			wantNames: []string{"alpha", "beta"},
		},
		{
			name:      "and can still narrow to one",
			asksFor:   "beta",
			wantNames: []string{"beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			ids := map[string]string{}
			for _, name := range []string{"alpha", "beta"} {
				agent := st.SeedAgent(t, agentstore.Agent{Name: name, State: agentstore.StateThinking})
				ids[name] = agent.ID
				seedTranscript(t, st, agent.ID, []string{"the reconciler derives its markers"})
			}

			ctx := t.Context()
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[tt.callerIs]})
			}

			req := &agentv1.SearchTranscriptsRequest{Query: "reconciler"}
			if tt.asksFor != "" {
				req.AgentId = xptr.Ptr(ids[tt.asksFor])
			}

			found, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))
			require.NoError(t, err)

			hits := allSearchHits(found.Msg)

			names := make([]string, 0, len(hits))
			for _, hit := range hits {
				names = append(names, hit.GetName())
			}
			assert.ElementsMatch(t, tt.wantNames, names)
		})
	}
}

// TestPagedReadsRefuseAMangledToken covers a token that decodes but holds an id no row can have.
func TestPagedReadsRefuseAMangledToken(t *testing.T) {
	const (
		listToken   = "eyJpIjoibm90LWFuLWlkIn0="
		searchToken = "eyJtIjp7InIiOjAuNSwiaSI6Im5vdC1hbi1pZCIsInMiOjF9fQ=="
	)

	tests := []struct {
		name  string
		token string
		call  func(t *testing.T, svc agentv1weave.AgentServiceAdapter, page *paginationv1.Pagination) error
	}{
		{
			name:  "listing agents",
			token: listToken,
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, page *paginationv1.Pagination) error {
				_, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{Pagination: page}))
				return err
			},
		},
		{
			name:  "searching transcripts",
			token: searchToken,
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, page *paginationv1.Pagination) error {
				_, err := svc.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
					Query:      "reconciler",
					Pagination: page,
				}))

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)

			err := tt.call(t, svc, &paginationv1.Pagination{Cursor: proto.String(tt.token)})
			require.Error(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// TestSearchTranscriptsScopeSurvivesACursor covers the second page of a scoped search,
// which a replayed token must not widen.
func TestSearchTranscriptsScopeSurvivesACursor(t *testing.T) {
	svc, st := service(t)

	ids := map[string]string{}
	for _, name := range []string{"alpha", "beta"} {
		agent := st.SeedAgent(t, agentstore.Agent{Name: name, State: agentstore.StateThinking})
		ids[name] = agent.ID
		seedTranscript(t, st, agent.ID, []string{"the reconciler derives its markers", "more on the reconciler"})
	}

	ctx := caller.With(t.Context(), caller.Caller{AgentID: ids["alpha"]})

	var (
		names []string
		token *string
	)
	for range 4 {
		req := &agentv1.SearchTranscriptsRequest{
			Query:      "reconciler",
			Pagination: &paginationv1.Pagination{Limit: proto.Int32(1), Cursor: token},
		}

		found, err := svc.SearchTranscripts(ctx, connect.NewRequest(req))
		require.NoError(t, err)
		if len(allSearchHits(found.Msg)) == 0 {
			break
		}

		for _, hit := range allSearchHits(found.Msg) {
			names = append(names, hit.GetName())
		}

		token = found.Msg.NextPageToken
		if token == nil {
			break
		}
	}

	assert.Equal(t, []string{"alpha", "alpha"}, names, "both of its own turns, and neither of the other agent's")
}

// TestCrossAgentReadIsRecorded covers the line a read of somebody else's conversation
// leaves, and the silence a read of your own does.
func TestCrossAgentReadIsRecorded(t *testing.T) {
	tests := []struct {
		name       string
		callerIs   string
		read       func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string)
		wantRecord bool
	}{
		{
			name:     "an agent searching another agent",
			callerIs: "alpha",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
					Query:   "reconciler",
					AgentId: xptr.Ptr(target),
				}))
				require.NoError(t, err)
			},
			wantRecord: true,
		},
		{
			name:     "an agent fetching another agent's whole conversation",
			callerIs: "alpha",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: target}))
				require.NoError(t, err)
			},
			wantRecord: true,
		},
		{
			name:     "an agent fetching another agent's last turns",
			callerIs: "alpha",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.GetLastTurns(ctx, connect.NewRequest(&agentv1.GetLastTurnsRequest{AgentId: target}))
				require.NoError(t, err)
			},
			wantRecord: true,
		},
		{
			name:     "an agent searching itself",
			callerIs: "beta",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, _ string) {
				t.Helper()
				_, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "reconciler"}))
				require.NoError(t, err)
			},
		},
		{
			name: "the panel reading anyone, which is what it is for",
			read: func(t *testing.T, svc agentv1weave.AgentServiceAdapter, ctx context.Context, target string) {
				t.Helper()
				_, err := svc.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: target}))
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			ids := map[string]string{}
			for _, name := range []string{"alpha", "beta"} {
				agent := st.SeedAgent(t, agentstore.Agent{Name: name, State: agentstore.StateThinking})
				ids[name] = agent.ID
				seedTranscript(t, st, agent.ID, []string{"the reconciler derives its markers"})
			}

			var written strings.Builder
			ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))
			if tt.callerIs != "" {
				ctx = caller.With(ctx, caller.Caller{AgentID: ids[tt.callerIs]})
			}

			tt.read(t, svc, ctx, ids["beta"])

			if !tt.wantRecord {
				assert.Empty(t, written.String(), "reading your own conversation is not worth a line")
				return
			}

			record := logtest.Record(t, written.String())
			assert.Equal(t, "agent read another agent's transcript", record["body"])
			assert.Equal(t, ids["beta"], record[logging.TargetAgentID])
		})
	}
}

// TestGetTranscript covers the agent being looked up first, an unknown agent and one with
// nothing synced yet both reading as empty content.
func TestGetTranscript(t *testing.T) {
	tests := []struct {
		name     string
		agentID  string
		turns    []string
		wantCode connect.Code
		want     string
	}{
		{
			name:     "an id no agent holds is refused",
			agentID:  uuid.NewString(),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "and so is an id that is not an id",
			agentID:  "not-a-uuid",
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an agent with nothing synced yet is empty rather than missing",
			want: "",
		},
		{
			name:  "a synced agent hands back the whole conversation",
			turns: []string{"opening question", "the answer"},
			want:  "opening question\n\nthe answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
			seedTranscript(t, st, agent.ID, tt.turns)

			id := tt.agentID
			if id == "" {
				id = agent.ID
			}

			got, err := svc.GetTranscript(t.Context(), connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: id}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Msg.GetContent())
		})
	}
}

// TestAgentLifecycleGoesThroughTheService covers delete, archive, and restore handing off to
// the thing that can stop an agent rather than editing rows behind its back.
func TestAgentLifecycleGoesThroughTheService(t *testing.T) {
	tests := []struct {
		name       string
		call       func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error
		fail       func(l *fakeLauncher)
		wantCode   connect.Code
		wantCalled func(t *testing.T, l *fakeLauncher)
	}{
		{
			name: "deleting an agent nobody holds",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{"ghost"}}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.deleteErr = agents.ErrAgentNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "archiving an agent nobody holds",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{"ghost"}}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.archiveErr = agents.ErrAgentNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "restoring an id nobody holds",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: []string{"ghost"}}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.restoreErr = agents.ErrAgentNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "deleting stops the agent",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{"alpha"}}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"alpha"}, l.deleted)
			},
		},
		{
			name: "archiving stops the agent",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{"alpha"}}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"alpha"}, l.archived)
			},
		},
		{
			name: "restoring relaunches it",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: []string{"id-1"}}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"id-1"}, l.restored)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			if tt.fail != nil {
				tt.fail(launcher)
			}

			err := tt.call(t, svc)
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			tt.wantCalled(t, launcher)
		})
	}
}

// fakeLauncher stands in for every service the handler talks to at once, so a test can wire one
// fake into all five of its params rather than five.
type fakeLauncher struct {
	t              *testing.T
	st             *storetest.Stores
	got            agents.NewAgentParams
	adopted        agents.AdoptParams
	created        *agentstore.Agent
	err            error
	alive          bool
	deleted        []string
	deleteErr      error
	archived       []string
	archiveErr     error
	restored       []string
	restoredAgents []agentstore.Agent
	restoreErr     error
	resumed        []string
	resumeErr      error
	runningCalls   int
	reviewed       agents.RequestReviewParams
	guidance       string
}

// spawned is the agent the fake stands up, written to the store the way a real launch writes
// its row.
func (f *fakeLauncher) spawned() *agentstore.Agent {
	if f.created == nil {
		f.created = f.st.SeedAgent(f.t, agentstore.Agent{
			Name:  "spawned",
			Cwd:   "/repo",
			State: agentstore.StateIdle,
		})
	}

	return f.created
}

func (f *fakeLauncher) NewAgent(_ context.Context, p agents.NewAgentParams) (*agentstore.Agent, error) {
	f.got = p
	if f.err != nil {
		return nil, f.err
	}

	return f.spawned(), nil
}

func (f *fakeLauncher) RequestReview(_ context.Context, p agents.RequestReviewParams) (*agentstore.Agent, string, error) {
	f.reviewed = p
	if f.err != nil {
		return nil, "", f.err
	}

	return f.spawned(), f.guidance, nil
}

func (f *fakeLauncher) Adopt(_ context.Context, p agents.AdoptParams) (*agentstore.Agent, error) {
	f.adopted = p
	if f.err != nil {
		return nil, f.err
	}

	return f.spawned(), nil
}

// IsAgentRunning answers for the fake's agents, which are live exactly when the test says so.
func (f *fakeLauncher) IsAgentRunning(agentstore.Agent) bool { return f.alive }

// GetRunningAgents answers a whole listing the same way, recording the call so a test can
// hold a listing to one lookup rather than one per agent.
func (f *fakeLauncher) GetRunningAgents(_ context.Context, agents []agentstore.Agent) map[string]bool {
	f.runningCalls++

	running := make(map[string]bool, len(agents))
	for _, agent := range agents {
		running[agent.ID] = f.alive
	}

	return running
}

func (f *fakeLauncher) DeleteAgents(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids...)

	return f.deleteErr
}

func (f *fakeLauncher) ArchiveAgents(_ context.Context, ids []string) error {
	f.archived = append(f.archived, ids...)

	return f.archiveErr
}

func (f *fakeLauncher) RestoreAgents(_ context.Context, ids []string) ([]agentstore.Agent, error) {
	f.restored = append(f.restored, ids...)
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	if f.restoredAgents != nil {
		return f.restoredAgents, nil
	}

	return []agentstore.Agent{*f.spawned()}, nil
}

func (f *fakeLauncher) ResumeAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	f.resumed = append(f.resumed, id)
	if f.resumeErr != nil {
		return nil, f.resumeErr
	}

	return f.spawned(), nil
}

func (f *fakeLauncher) RetryCreate(_ context.Context, _ agents.RetryCreateParams) (*agentstore.Agent, error) {
	return f.spawned(), nil
}

type fakeMailConfig int

func (fakeMailConfig) GetDeliveryDeadline() (time.Duration, error) { return time.Hour, nil }

func (s fakeMailConfig) GetMaxExchange() (int, error) { return int(s), nil }

// markService is the real thing over the test stores, a mark spanning two tables a fake would
// only agree with itself about.
func markService(st *storetest.Stores) *marks.MarkService {
	return marks.NewMarkService(marks.NewMarkServiceParams{Marks: st.Marks, Tx: st.Pool})
}

func service(t *testing.T) (agentv1weave.AgentServiceAdapter, *storetest.Stores) {
	t.Helper()
	svc, st, _ := serviceWithLauncher(t)

	return svc, st
}

func seedTranscript(t *testing.T, st *storetest.Stores, agentID string, turns []string) string {
	t.Helper()

	if len(turns) == 0 {
		return ""
	}

	st.SeedTranscript(t, agentID, turns...)

	content, err := st.Transcripts.Get(t.Context(), agentID)
	require.NoError(t, err)

	return content
}

func storedMetadata(agent *agentv1.Agent) []agentstore.Metadata {
	if len(agent.GetMetadata()) == 0 {
		return nil
	}

	stored := make([]agentstore.Metadata, 0, len(agent.GetMetadata()))
	for _, entry := range agent.GetMetadata() {
		stored = append(stored, agentstore.Metadata{Key: entry.GetKey(), Value: entry.GetValue()})
	}

	return stored
}

func serviceWithLauncher(t *testing.T) (agentv1weave.AgentServiceAdapter, *storetest.Stores, *fakeLauncher) {
	t.Helper()

	st := storetest.Open(t)
	launcher := &fakeLauncher{t: t, st: st}

	mailer := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: st.Mail,
		Agents:         st.Agents,
		Workflows:      st.Workflows,
		Config:         fakeMailConfig(config.DefaultMaxExchange),
	})

	rounds := reviewstore.New(st.Pool.GetDB())
	review := reviews.NewReviewService(reviews.NewReviewServiceParams{
		Threads:   rounds,
		Messages:  rounds,
		Viewed:    rounds,
		Rounds:    rounds,
		Reviews:   rounds,
		Snapshots: fakeSnapshotter{},
		Workflows: st.Workflows,
		StateDir:  t.TempDir(),
		Agents:    st.Agents,
		Mailer:    mailer,
	})

	return agentv1weave.AgentServiceAdapter{Impl: NewAgentService(NewAgentServiceParams{
		Agents:      st.Agents,
		Rows:        rowReaderOver(st, launcher, fakeTrees{}),
		Writer:      rowWriterOver(st, &fakeReconciler{}),
		Transcripts: st.Transcripts,
		Mail:        mailer,
		Review:      review,
		Marks:       markService(st),
		Creator:     launcher,
		Adopter:     launcher,
		Liveness:    launcher,
		Lifecycle:   launcher,
		Resumer:     launcher,
	})}, st, launcher
}

// rowWriterOver is the real thing over the test stores, with the fake standing in for the
// reconciler.
func rowWriterOver(st *storetest.Stores, reconciler *fakeReconciler) *agents.RowWriter {
	params := agents.NewRowWriterParams{Reconciler: reconciler}
	if st != nil {
		params.Agents = st.Agents
		params.Runs = st.Runs
	}

	return agents.NewRowWriter(params)
}

// rowReaderOver is the real thing over the test stores, with the fakes standing in for tmux and
// the cwd sync.
func rowReaderOver(st *storetest.Stores, liveness *fakeLauncher, trees fakeTrees) *agents.RowReader {
	return agents.NewRowReader(agents.NewRowReaderParams{
		Rows:     st.Rows,
		Liveness: liveness,
		Trees:    trees,
	})
}

// fakeTrees is where each agent's worktree stands against the one round it was compared to,
// and unmoved against any other.
type fakeTrees map[string]standingAt

type standingAt struct {
	snapshotSHA string
	standing    git.Standing
}

func (f fakeTrees) GetStanding(agentID, snapshotSHA string) git.Standing {
	if at, ok := f[agentID]; ok && at.snapshotSHA == snapshotSHA {
		return at.standing
	}

	return git.StandingSame
}

// TestEmptyArchiveQueuesTheSweep covers the request coming back once the work is queued, and a
// queue that refuses saying so rather than reading as a sweep under way.
func TestEmptyArchiveQueuesTheSweep(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{
			name: "a queued sweep answers straight away",
		},
		{
			name:     "a queue that refuses is reported",
			err:      errors.New("the database is gone"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := &fakeArchiveEmptier{err: tt.err}
			svc := agentv1weave.AgentServiceAdapter{Impl: NewAgentService(NewAgentServiceParams{Archive: archive})}

			_, err := svc.EmptyArchive(t.Context(), connect.NewRequest(&emptypb.Empty{}))

			assert.Equal(t, 1, archive.calls, "the handler queues the sweep either way")

			if tt.wantCode == 0 {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

type fakeArchiveEmptier struct {
	calls int
	err   error
}

func (f *fakeArchiveEmptier) EmptyArchive(context.Context) error {
	f.calls++

	return f.err
}

// TestBatchLifecycleErrorsMapToCodes covers a refused batch naming which condition stopped it.
func TestBatchLifecycleErrorsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error
		fail     func(l *fakeLauncher)
		wantCode connect.Code
	}{
		{
			name: "an archive naming an agent nobody holds",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
					Ids: []string{"alpha", "ghost"},
				}))

				return err
			},
			fail:     func(l *fakeLauncher) { l.archiveErr = fmt.Errorf("%w: ghost", agents.ErrAgentNotFound) },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a delete naming one",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{
					Ids: []string{"alpha", "ghost"},
				}))

				return err
			},
			fail:     func(l *fakeLauncher) { l.deleteErr = fmt.Errorf("%w: ghost", agents.ErrAgentNotFound) },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a restore that would take the panel past its ceiling",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{
					Ids: []string{"alpha", "beta"},
				}))

				return err
			},
			fail:     func(l *fakeLauncher) { l.restoreErr = agents.ErrTooManyAgents },
			wantCode: connect.CodeResourceExhausted,
		},
		{
			name: "a restore onto a name a live agent has taken",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{
					Ids: []string{"alpha", "beta"},
				}))

				return err
			},
			fail:     func(l *fakeLauncher) { l.restoreErr = fmt.Errorf("%w: auth", store.ErrNameTaken) },
			wantCode: connect.CodeAlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			tt.fail(launcher)

			err := tt.call(t, svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestBatchLifecycleReachesTheService covers the whole list arriving rather than the first id.
func TestBatchLifecycleReachesTheService(t *testing.T) {
	tests := []struct {
		name       string
		call       func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error
		wantCalled func(l *fakeLauncher) []string
	}{
		{
			name: "archiving",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
					Ids: []string{"a1", "a2", "a3"},
				}))

				return err
			},
			wantCalled: func(l *fakeLauncher) []string { return l.archived },
		},
		{
			name: "deleting",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{
					Ids: []string{"a1", "a2", "a3"},
				}))

				return err
			},
			wantCalled: func(l *fakeLauncher) []string { return l.deleted },
		},
		{
			name: "restoring",
			call: func(t *testing.T, svc agentv1weave.AgentServiceAdapter) error {
				_, err := svc.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{
					Ids: []string{"a1", "a2", "a3"},
				}))

				return err
			},
			wantCalled: func(l *fakeLauncher) []string { return l.restored },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)

			require.NoError(t, tt.call(t, svc))
			assert.Equal(t, []string{"a1", "a2", "a3"}, tt.wantCalled(launcher))
		})
	}
}

// TestRestoreAgentsRendersEveryAgent covers the batch coming back whole rather than as the
// first agent restored.
func TestRestoreAgentsRendersEveryAgent(t *testing.T) {
	svc, st, launcher := serviceWithLauncher(t)
	ctx := t.Context()

	auth := st.SeedAgent(t, agentstore.Agent{Name: "auth", State: agentstore.StateIdle})
	api := st.SeedAgent(t, agentstore.Agent{Name: "api", State: agentstore.StateIdle})
	launcher.restoredAgents = []agentstore.Agent{*auth, *api}

	restored, err := svc.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
		Ids: []string{auth.ID, api.ID},
	}))
	require.NoError(t, err)

	names := make([]string, 0, len(restored.Msg.GetAgents()))
	for _, agent := range restored.Msg.GetAgents() {
		names = append(names, agent.GetName())
	}

	assert.Equal(t, []string{"auth", "api"}, names)
	assert.Equal(t, 1, launcher.runningCalls, "liveness is read once for the batch, not once per agent")
}

// TestListAgentsSummarisesThePrompt covers the default view holding back the bulk of a page,
// which is the prompt.
func TestListAgentsSummarisesThePrompt(t *testing.T) {
	svc, st := service(t)
	prompt := strings.Repeat("a", render.SummaryHead+50)
	st.SeedAgent(t, agentstore.Agent{
		Name:   "alpha",
		State:  agentstore.StateIdle,
		Prompt: prompt,
	})
	st.SeedAgent(t, agentstore.Agent{
		Name:   "beta",
		State:  agentstore.StateIdle,
		Prompt: "fix auth",
	})

	summary, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
	require.NoError(t, err)
	byName := agentsByName(summary.Msg.GetAgents())
	assert.Equal(t, prompt[:render.SummaryHead], byName["alpha"].GetPrompt())
	assert.True(t, byName["alpha"].GetPromptTruncated())
	assert.Equal(t, "fix auth", byName["beta"].GetPrompt(), "a prompt inside the head is whole")
	assert.False(t, byName["beta"].GetPromptTruncated())

	full, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{
		View: viewv1.View_VIEW_FULL,
	}))
	require.NoError(t, err)
	byName = agentsByName(full.Msg.GetAgents())
	assert.Equal(t, prompt, byName["alpha"].GetPrompt())
	assert.False(t, byName["alpha"].GetPromptTruncated())
}

// TestGetAgentKeepsTheWholePrompt covers the one-record endpoint, which no view narrows since a
// caller asking for a single agent has already narrowed it.
func TestGetAgentKeepsTheWholePrompt(t *testing.T) {
	svc, st := service(t)
	prompt := strings.Repeat("a", render.SummaryHead+50)
	seeded := st.SeedAgent(t, agentstore.Agent{
		Name:   "alpha",
		State:  agentstore.StateIdle,
		Prompt: prompt,
	})

	got, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: seeded.ID}))
	require.NoError(t, err)
	assert.Equal(t, prompt, got.Msg.GetAgent().GetPrompt())
	assert.False(t, got.Msg.GetAgent().GetPromptTruncated())
}

func agentsByName(listed []*agentv1.Agent) map[string]*agentv1.Agent {
	byName := make(map[string]*agentv1.Agent, len(listed))
	for _, a := range listed {
		byName[a.GetName()] = a
	}

	return byName
}
