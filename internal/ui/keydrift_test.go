package ui

import (
	"reflect"
	"slices"
	"testing"

	"weave/config"
	"weave/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// undrawn is a key a screen takes on purpose without naming it in its legend, carrying the reason
// it is a decision rather than an oversight.
type undrawn struct {
	key    string
	reason string
}

// takenAndUndrawn is the allowlist the drift test measures against.
var takenAndUndrawn = map[screen][]undrawn{
	screenAgents: {
		{key: "ctrl+c", reason: "quit is not worth a row on any screen"},
		{key: "esc", reason: "on the root list this is the quit key by another name"},
		{key: "V", reason: "select to nearest is named once the selection it needs is standing"},
		{key: "L", reason: "the earlier run is named only while the pane is showing one"},
	},
	screenSchedules: {
		{key: "ctrl+c", reason: "quit is not worth a row on any screen"},
		{key: "L", reason: "the earlier run is named only while the pane is showing one"},
	},
	screenArchive: {
		{key: "ctrl+c", reason: "quit is not worth a row on any screen"},
		{key: "V", reason: "select to nearest is named once the selection it needs is standing"},
	},
	screenSearch: {
		{key: "ctrl+c", reason: "quit is not worth a row on any screen"},
	},
	screenReview: {
		{key: "ctrl+c", reason: "quit is not worth a row on any screen"},
		{key: "ctrl+n", reason: "the mouse wheel presses it, and paging is the only scroll the diff names"},
		{key: "ctrl+p", reason: "the mouse wheel presses it, and paging is the only scroll the diff names"},
	},
	screenWorktrees: {
		{key: "ctrl+c", reason: "quit is not worth a row on any screen"},
		{key: "V", reason: "select to nearest is named once the selection it needs is standing"},
	},
}

// TestEveryKeyTakenIsDrawn presses every configured key against every screen and fails on one the
// dispatch answers without the legend naming it, the panel building the two in unrelated places.
func TestEveryKeyTakenIsDrawn(t *testing.T) {
	legends := map[screen]func(model) keyMap{
		screenAgents:    model.agentHelp,
		screenSchedules: model.scheduleHelp,
		screenArchive:   model.archiveHelp,
		screenSearch:    model.searchHelp,
		screenReview:    model.reviewHelp,
		screenWorktrees: model.worktreesHelp,
	}

	pressable := configuredKeys(t, config.DefaultKeybindings())

	for s, legend := range legends {
		drawn := drawnKeys(legend(driftModel(t, s)))
		allowed := takenAndUndrawn[s]

		for _, pressed := range pressable {
			if slices.ContainsFunc(allowed, func(u undrawn) bool { return u.key == pressed }) {
				continue
			}

			// A fresh model every press.
			if !isKeyTaken(t, driftModel(t, s), pressed) {
				continue
			}

			assert.Contains(t, drawn, pressed,
				"screen %d answers %q and its legend does not name it", s, pressed)
		}
	}
}

// TestTheAllowlistIsSpent pins that every key excused from the drift test is still taken, so an
// entry outlives the behaviour it was written for by one commit rather than forever.
func TestTheAllowlistIsSpent(t *testing.T) {
	for s, allowed := range takenAndUndrawn {
		for _, u := range allowed {
			assert.True(t, isKeyTaken(t, driftModel(t, s), u.key),
				"screen %d no longer answers %q, so the allowlist entry can go: %s", s, u.key, u.reason)
		}
	}
}

// isKeyTaken presses one key and reports whether any dispatch arm matched it, asked of the
// bindings themselves and not of what changed.
func isKeyTaken(t *testing.T, m model, pressed string) bool {
	t.Helper()

	taken := false
	keyProbe = func([]string) { taken = true }
	defer func() { keyProbe = nil }()

	m.route(keyMsgOf(t, pressed))

	return taken
}

// drawnKeys is every key string the legend names, short form and expanded together.
func drawnKeys(k keyMap) []string {
	var out []string
	for _, binding := range slices.Concat(k.short, k.full) {
		out = append(out, binding.Keys()...)
	}

	return out
}

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
