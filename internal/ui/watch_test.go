package ui

import (
	"errors"
	"testing"
	"time"

	"weave/config"
	"weave/internal/client"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func watchedPanel() model {
	return model{
		api: api{
			agents:    emptyLists{},
			workflows: emptyLists{},
			schedules: emptyLists{},
		},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}
}

func update(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)

	updated, ok := next.(model)
	require.True(t, ok)

	return updated, cmd
}

// hasListed says whether the tick's command asked the service for a listing.
func hasListed(cmd tea.Cmd) bool {
	for _, msg := range produced(cmd) {
		if _, ok := msg.(refreshResult); ok {
			return true
		}
	}

	return false
}

// TestListingArrivesFromEitherTrigger covers the one apply path under two triggers: the
// listing the watch built lands the way the poll's does, and the poll's still lands after it.
func TestListingArrivesFromEitherTrigger(t *testing.T) {
	m := watchedPanel()

	m, _ = update(t, m, watchedResultFrom(client.WatchListing{
		Agents:    []*agentv1.Agent{{Id: "a1", Name: "api-refactor", WorkflowId: "w1", Mark: proto.String("a")}},
		Workflows: []*workflowv1.Workflow{{Id: "w1", Name: "api"}},
	}))

	require.Len(t, m.listing.agents, 1, "the watch's listing is applied")
	assert.Equal(t, "api-refactor", m.listing.agents[0].agent.Name)
	assert.Equal(t, map[string]string{agentRowKey("a1"): "a"}, m.listing.marks)
	assert.True(t, m.watching)

	selected, ok := m.agents.selectedAgent(m.shared())
	require.True(t, ok, "and the cursor is placed the way the first poll places it")
	assert.Equal(t, "api-refactor", selected.agent.Name)

	m, _ = update(t, m, refreshResult{
		epoch:        m.epoch,
		watchedEpoch: m.watchedEpoch,
		workflows:    []panel.Workflow{{ID: "w1", Name: "api"}},
		agents:       []agentItem{{agent: panel.Agent{ID: "a1", Name: "renamed", WorkflowID: "w1"}}},
	})

	assert.Equal(t, "renamed", m.listing.agents[0].agent.Name, "the poll's listing lands the same way")
}

// TestWatchedListingIsNotDroppedAsStale covers a listing the watch built arriving after a
// write bumped the epoch, which is fresher than any poll and so is taken.
func TestWatchedListingIsNotDroppedAsStale(t *testing.T) {
	m := watchedPanel()
	m.epoch = 4

	m, _ = update(t, m, watchedResultFrom(client.WatchListing{
		Agents: []*agentv1.Agent{{Id: "a1", Name: "api-refactor"}},
	}))

	assert.Len(t, m.listing.agents, 1)
}

// TestWatchedListingOutranksAPollOnItsWay covers a poll sent before the watch delivered and
// landing after it, which would put back rows the stream has since replaced.
func TestWatchedListingOutranksAPollOnItsWay(t *testing.T) {
	m := watchedPanel()

	polled := refreshResult{
		epoch:  m.epoch,
		agents: []agentItem{{agent: panel.Agent{ID: "a1", Name: "before"}}},
	}

	m, _ = update(t, m, watchedResultFrom(client.WatchListing{
		Agents: []*agentv1.Agent{{Id: "a1", Name: "after"}},
	}))
	m, _ = update(t, m, polled)

	assert.Equal(t, "after", m.listing.agents[0].agent.Name, "the poll that read the store first is dropped")
}

// TestPollBehindTheWatchStillCarriesTheArchive covers the same poll on the archive screen, where
// its archive half is the only source of those rows and is applied though its live half is not.
func TestPollBehindTheWatchStillCarriesTheArchive(t *testing.T) {
	m := watchedPanel()
	m.screen = screenArchive

	polled := refreshResult{
		epoch:          m.epoch,
		agents:         []agentItem{{agent: panel.Agent{ID: "a1", Name: "before"}}},
		archived:       []agentItem{{agent: panel.Agent{ID: "a0", Name: "retired"}}},
		archivedListed: true,
	}

	m, _ = update(t, m, watchedResultFrom(client.WatchListing{
		Agents: []*agentv1.Agent{{Id: "a1", Name: "after"}},
	}))
	m, _ = update(t, m, polled)

	assert.Equal(t, "after", m.listing.agents[0].agent.Name)
	require.Len(t, m.listing.archivedAgents, 1, "the archive the watch never carries is taken")
	assert.Equal(t, "retired", m.listing.archivedAgents[0].agent.Name)
}

// TestWatchedListingLandsUnderAWrite covers the watch delivering while a write is in flight,
// which is taken as it comes rather than held back for a poll.
func TestWatchedListingLandsUnderAWrite(t *testing.T) {
	m := watchedPanel()
	watched := watchedResultFrom(client.WatchListing{
		Agents: []*agentv1.Agent{{Id: "a1", Name: "api-refactor"}},
	})

	write := m.writeCmd("Could not write", func(api) error { return nil })

	m, _ = update(t, m, watched)
	assert.Len(t, m.listing.agents, 1, "a listing arriving under a write is applied")
	assert.True(t, m.watching)

	m = settle(t, m, write)
	require.Zero(t, m.pending)
	assert.Len(t, m.listing.agents, 1)
}

// TestTickPollsSlowlyWhileTheWatchDelivers covers the poll as the safety net: every second
// until the watch has delivered, every pollInterval while it does, and every second again once
// the stream is lost.
func TestTickPollsSlowlyWhileTheWatchDelivers(t *testing.T) {
	m := watchedPanel()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	m, cmd := update(t, m, refreshTickMsg{at: now})
	assert.True(t, hasListed(cmd), "the first tick lists")

	m, cmd = update(t, m, refreshTickMsg{at: now.Add(time.Second)})
	assert.True(t, hasListed(cmd), "and so does every tick while the watch has delivered nothing")

	m, _ = update(t, m, watchedResultFrom(client.WatchListing{}))

	m, cmd = update(t, m, refreshTickMsg{at: now.Add(2 * time.Second)})
	assert.False(t, hasListed(cmd), "a tick under the watch leaves the listing to it")

	m, cmd = update(t, m, refreshTickMsg{at: now.Add(time.Second + pollInterval)})
	assert.True(t, hasListed(cmd), "until the poll falls due")

	m, _ = update(t, m, watchLostMsg{err: errors.New("the server hung up")})

	_, cmd = update(t, m, refreshTickMsg{at: now.Add(2*time.Second + pollInterval)})
	assert.True(t, hasListed(cmd), "and every tick lists again once the stream is lost")
}

// TestTickPollsEveryTimeOnAListingTheWatchLacks covers the archive screen, whose listing rides
// no stream, keeping its poll under the watch.
func TestTickPollsEveryTimeOnAListingTheWatchLacks(t *testing.T) {
	m := watchedPanel()
	m.screen = screenArchive
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	m, _ = update(t, m, watchedResultFrom(client.WatchListing{}))

	m, cmd := update(t, m, refreshTickMsg{at: now})
	assert.True(t, hasListed(cmd))

	_, cmd = update(t, m, refreshTickMsg{at: now.Add(time.Second)})
	assert.True(t, hasListed(cmd), "the archive is listed on every tick whatever the watch carries")
}
