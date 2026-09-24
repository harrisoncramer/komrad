package ui

import (
	"reflect"
	"slices"
	"testing"

	"weave/config"
	"weave/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// configuredKeys is every key string the default configuration binds anywhere, walked off the
// struct.
func configuredKeys(t *testing.T, keys config.Keybindings) []string {
	t.Helper()

	var out []string

	groups := reflect.ValueOf(keys)
	for i := range groups.NumField() {
		group := groups.Field(i)
		for j := range group.NumField() {
			field, ok := group.Field(j).Interface().([]string)
			require.True(t, ok, "%s.%s is not a key list", groups.Type().Field(i).Name, group.Type().Field(j).Name)

			for _, k := range field {
				if !slices.Contains(out, k) {
					out = append(out, k)
				}
			}
		}
	}

	return out
}

// keyMsgOf turns a configured key string back into the press bubbletea would deliver, and
// checks the round trip.
func keyMsgOf(t *testing.T, pressed string) tea.KeyPressMsg {
	t.Helper()

	msg := keyMsg(pressed)

	require.Equal(t, pressed, msg.String(), "the press this test builds is not the key it was asked for")

	return msg
}

func driftModel(t *testing.T, s screen) model {
	t.Helper()

	if s == screenReview {
		return driftReviewModel(t)
	}

	m := infoModel(&recordingTmux{width: 200, height: 30})
	m.width, m.height = 200, 30
	m.screen = s
	m.api = driftAPI()

	// The archive draws the keys that act on a row only while it is holding one, so an empty
	// archive would report every one of them as undrawn.
	if s == screenArchive {
		m.listing.archivedAgents = archivedItems([]panel.Agent{{
			ID:         "a2",
			Name:       "old",
			WorkflowID: "w1",
			State:      panel.StateIdle,
		}})
		m.archive.cursor = 1
	}

	// So does the worktrees screen.
	if s == screenWorktrees {
		m.worktrees.items = worktreeFixture()
		m.worktrees.cursor = 1
	}

	return m
}

func driftReviewModel(t *testing.T) model {
	t.Helper()

	m, _ := buildReviewModel(t, &recordingReviewAgents{})
	m = openReview(t, m)

	api := driftAPI()
	api.agents = m.api.agents
	m.api = api

	return m
}

// driftAPI answers every client a key might reach for.
func driftAPI() api {
	return api{
		agents:    emptyLists{},
		workflows: emptyLists{},
		schedules: emptyLists{},
		worktrees: emptyLists{},
		hooks:     &fakeHooks{},
	}
}
