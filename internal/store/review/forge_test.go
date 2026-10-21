package review

import (
	"testing"
	"time"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// heldThread is a forge thread under a held root, with a trusted forge reply and the agent's answer.
type heldThread struct {
	thread   *Thread
	rootID   string
	snapshot *Snapshot
}

func (st *fixture) seedHeldThread(t *testing.T, rootHeld, replyHeld bool) heldThread {
	t.Helper()
	ctx := t.Context()

	agentID := st.seedAgent(t, "worker")
	snapshot := st.takeSnapshot(t, agentID, "tree-one")
	opener := st.openForgeReview(t, snapshot, "mallory")
	replier := st.openForgeReview(t, snapshot, "alice")

	thread, err := st.CreateThread(ctx, CreateThreadParams{
		TaskID:      st.taskOf(t, agentID),
		ReviewID:    &opener.ID,
		SnapshotID:  snapshot.ID,
		FilePath:    "internal/thing.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        "root",
		AuthorKind:  AuthorForge,
		Held:        rootHeld,
	})
	require.NoError(t, err)
	_, err = st.CreateMessage(ctx, CreateMessageParams{
		ThreadID:   thread.ID,
		ReviewID:   &replier.ID,
		AuthorKind: AuthorForge,
		Body:       "reply",
		Held:       replyHeld,
	})
	require.NoError(t, err)
	_, err = st.CreateMessage(ctx, CreateMessageParams{
		ThreadID:      thread.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &agentID,
		Body:          "answer",
	})
	require.NoError(t, err)

	messages, err := st.ListMessages(ctx, []string{thread.ID})
	require.NoError(t, err)

	return heldThread{
		thread:   &thread.Thread,
		rootID:   messages[0].ID,
		snapshot: snapshot,
	}
}

// TestHeldForgeThreads covers which of a forge thread's messages are held from the agent, as the
// panel lists them and as the agent's reader login can query them.
func TestHeldForgeThreads(t *testing.T) {
	tests := []struct {
		name     string
		seed     func(t *testing.T, st *fixture) heldThread
		held     []string
		readable []string
	}{
		{
			name: "a held root holds the trusted reply under it but not the agent's answer",
			seed: func(t *testing.T, st *fixture) heldThread {
				return st.seedHeldThread(t, true, false)
			},
			held:     []string{"root", "reply"},
			readable: []string{"answer"},
		},
		{
			name: "a held reply under a trusted root is held on its own",
			seed: func(t *testing.T, st *fixture) heldThread {
				return st.seedHeldThread(t, false, true)
			},
			held:     []string{"reply"},
			readable: []string{"root", "answer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			ctx := t.Context()
			held := tt.seed(t, st)

			messages, err := st.ListMessages(ctx, []string{held.thread.ID})
			require.NoError(t, err)
			var listed []string
			for _, message := range messages {
				if message.Held {
					listed = append(listed, message.Body)
				}
			}
			assert.ElementsMatch(t, tt.held, listed)

			reader := st.openReader(t)
			rows, err := reader.Query(ctx, "SELECT body FROM review_messages WHERE thread_id = $1", held.thread.ID)
			require.NoError(t, err)
			readable, err := pgx.CollectRows(rows, pgx.RowTo[string])
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.readable, readable)
		})
	}
}

// TestHeldForgeThreadHiddenFromReader covers a thread whose only message is a held root, which the
// reader cannot see at all.
func TestHeldForgeThreadHiddenFromReader(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	snapshot := st.takeSnapshot(t, agentID, "tree-one")
	opener := st.openForgeReview(t, snapshot, "mallory")

	thread, err := st.CreateThread(ctx, CreateThreadParams{
		TaskID:      st.taskOf(t, agentID),
		ReviewID:    &opener.ID,
		SnapshotID:  snapshot.ID,
		FilePath:    "internal/thing.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        "root",
		AuthorKind:  AuthorForge,
		Held:        true,
	})
	require.NoError(t, err)

	var count int
	require.NoError(t, st.openReader(t).QueryRow(ctx, "SELECT count(*) FROM review_threads WHERE id = $1", thread.ID).Scan(&count))
	assert.Zero(t, count)
}

// TestListForgeMirrorTargets covers the tasks a pull request's forge threads are mirrored onto.
func TestListForgeMirrorTargets(t *testing.T) {
	tests := []struct {
		name  string
		seed  func(t *testing.T, st *fixture, agentID string)
		check func(t *testing.T, targets []ForgeMirrorTarget, taskID string)
	}{
		{
			name: "an agent with no snapshot has nothing to mirror onto",
			seed: func(*testing.T, *fixture, string) {},
			check: func(t *testing.T, targets []ForgeMirrorTarget, _ string) {
				assert.Empty(t, targets)
			},
		},
		{
			name: "an archived agent is left out",
			seed: func(t *testing.T, st *fixture, agentID string) {
				st.takeSnapshot(t, agentID, "tree-one")
				_, err := st.agents.Archive(t.Context(), []string{agentID})
				require.NoError(t, err)
			},
			check: func(t *testing.T, targets []ForgeMirrorTarget, _ string) {
				assert.Empty(t, targets)
			},
		},
		{
			name: "a task with a reviewer on it is mirrored onto once",
			seed: func(t *testing.T, st *fixture, agentID string) {
				st.takeSnapshot(t, agentID, "tree-one")
				st.seedAgentLike(t, agentstore.Agent{
					Name:   "reviewer",
					Role:   agentstore.RoleReviewer,
					TaskID: xptr.Ptr(st.taskOf(t, agentID)),
				})
			},
			check: func(t *testing.T, targets []ForgeMirrorTarget, taskID string) {
				require.Len(t, targets, 1)
				assert.Equal(t, taskID, targets[0].TaskID)
			},
		},
		{
			name: "an agent on the pull request is mirrored onto at its newest snapshot",
			seed: func(t *testing.T, st *fixture, agentID string) {
				st.takeSnapshot(t, agentID, "tree-one")
				st.takeSnapshot(t, agentID, "tree-two")
			},
			check: func(t *testing.T, targets []ForgeMirrorTarget, taskID string) {
				require.Len(t, targets, 1)
				assert.Equal(t, taskID, targets[0].TaskID)
				assert.Equal(t, "snapshot-tree-two", targets[0].SnapshotSHA)
				assert.Equal(t, "base-tree-two", targets[0].BaseSHA)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			agentID := st.seedAgent(t, "worker")
			pullRequestID := st.seedPullRequest(t, agentID)
			tt.seed(t, st, agentID)

			targets, err := st.ListForgeMirrorTargets(t.Context(), pullRequestID)
			require.NoError(t, err)
			tt.check(t, targets, st.taskOf(t, agentID))
		})
	}
}

func (st *fixture) openForgeReview(t *testing.T, snapshot *Snapshot, login string) *Review {
	t.Helper()

	review, err := st.OpenForgeReview(t.Context(), OpenForgeReviewParams{
		TaskID:     snapshot.TaskID,
		SnapshotID: snapshot.ID,
		Login:      login,
	})
	require.NoError(t, err)
	require.NotNil(t, review)

	return review
}

// seedPullRequest opens a pull request from the agent's branch and returns its id.
func (st *fixture) seedPullRequest(t *testing.T, agentID string) string {
	t.Helper()
	ctx := t.Context()

	repository, branch := "https://github.com/harrisoncramer/komrad", "agent/worker"
	st.stores.SeedTaskHead(t, agentID, taskstore.SetHeadParams{
		RepositoryURL: &repository,
		Branch:        &branch,
	})
	require.NoError(t, pullrequeststore.NewPullRequestSyncStore(st.pool).EnsureSync(ctx, repository))

	pullRequests := pullrequeststore.NewPullRequestStore(st.pool)
	opened := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	_, err := pullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_1",
		URL:               repository + "/pull/1",
		RepositoryURL:     repository,
		HeadRepositoryURL: repository,
		Number:            1,
		HeadBranch:        branch,
		BaseBranch:        "main",
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened,
	})
	require.NoError(t, err)
	_, err = pullRequests.Observe(ctx)
	require.NoError(t, err)

	pullRequest, err := pullRequests.GetByIdentity(ctx, "github.com", "PR_1")
	require.NoError(t, err)
	require.NotNil(t, pullRequest)

	return pullRequest.ID
}

// openReader opens the engine's read-only login, which the agent's database queries run as.
func (st *fixture) openReader(t *testing.T) *pgxpool.Pool {
	t.Helper()

	reader, err := store.OpenReadOnly(t.Context(), st.reader, st.engineID)
	require.NoError(t, err)
	t.Cleanup(reader.Close)

	return reader
}
