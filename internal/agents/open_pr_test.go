package agents

import (
	"context"
	"errors"
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenPullRequestRefusals covers everything answered before the desktop is asked anything,
// since each of these is a banner on the panel rather than a window that never appeared.
func TestOpenPullRequestRefusals(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		agent   *agentstore.Agent
		wantIs  error
	}{
		{
			name:    "an agent that is not there",
			agentID: "missing",
			agent:   &agentstore.Agent{ID: "a1"},
			wantIs:  ErrAgentNotFound,
		},
		{
			name:    "an agent with nothing recorded",
			agentID: "a1",
			agent:   &agentstore.Agent{ID: "a1"},
			wantIs:  ErrNoPullRequest,
		},
		{
			name:    "a key recorded empty, which the panel's own key reads as none",
			agentID: "a1",
			agent: &agentstore.Agent{
				ID:       "a1",
				Metadata: []agentstore.Metadata{{Key: "pr", Value: ""}},
			},
			wantIs: ErrNoPullRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opener := &recordingOpener{}
			service := NewPullRequestOpener(PullRequestOpenerParams{
				Agents: fakeAgentReader{agent: tt.agent},
				Opener: opener,
			})

			err := service.OpenPullRequest(t.Context(), tt.agentID)

			require.ErrorIs(t, err, tt.wantIs)
			assert.Empty(t, opener.targets, "the desktop was never asked")
		})
	}
}

// TestOpenPullRequestCarriesWhyTheOpenerFailed covers the one failure a person can do
// something about, which reaches them only if it travels with the refusal.
func TestOpenPullRequestCarriesWhyTheOpenerFailed(t *testing.T) {
	service := NewPullRequestOpener(PullRequestOpenerParams{
		Agents: fakeAgentReader{agent: &agentstore.Agent{
			ID:       "a1",
			Metadata: []agentstore.Metadata{{Key: "pr", Value: "https://github.test/weave/pull/7"}},
		}},
		Opener: &recordingOpener{err: errors.New("no application knows how to open it")},
	})

	err := service.OpenPullRequest(t.Context(), "a1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://github.test/weave/pull/7")
	assert.Contains(t, err.Error(), "no application knows how to open it")
}

// TestOpenPullRequestHandsOverTheRecordedURL covers the whole of the happy path, which is one
// value read off the agent and passed on.
func TestOpenPullRequestHandsOverTheRecordedURL(t *testing.T) {
	opener := &recordingOpener{}
	service := NewPullRequestOpener(PullRequestOpenerParams{
		Agents: fakeAgentReader{agent: &agentstore.Agent{
			ID: "a1",
			Metadata: []agentstore.Metadata{
				{Key: "branch", Value: "agent/a1"},
				{Key: "pr", Value: "https://github.test/weave/pull/7"},
			},
		}},
		Opener: opener,
	})

	require.NoError(t, service.OpenPullRequest(t.Context(), "a1"))

	assert.Equal(t, []string{"https://github.test/weave/pull/7"}, opener.targets)
}

type recordingOpener struct {
	targets []string
	err     error
}

func (o *recordingOpener) OpenURL(_ context.Context, target string) error {
	o.targets = append(o.targets, target)

	return o.err
}
