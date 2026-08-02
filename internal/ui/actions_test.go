package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/appconfig"
	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestActionKeys covers what a keypress on the actions list does. Running does not move off the
// list, since firing an action is closer to a webhook than to opening something, so there is
// nowhere to navigate to and the cursor stays on the agent to fire a second one at.
func TestActionKeys(t *testing.T) {
	keys := appconfig.DefaultKeybindings()

	tests := []struct {
		name string
		// empty starts the screen with no actions, for the keys that act on a row.
		empty      bool
		key        string
		wantScreen screen
		wantFired  []string
	}{
		{
			name:       "run on an empty list fires nothing",
			empty:      true,
			key:        "enter",
			wantScreen: screenActions,
		},
		{
			name:       "back returns to the agents list without firing anything",
			key:        "esc",
			wantScreen: screenAgents,
		},
		{
			name:       "down moves to the next action",
			key:        "j",
			wantScreen: screenActions,
		},
		{
			name:       "run fires the action under the cursor and stays put",
			key:        "enter",
			wantScreen: screenActions,
			wantFired:  []string{"a1/open-pr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeActions{}
			m := actionsModel(client, keys)
			if tt.empty {
				m.actions.items = nil
			}

			updated, cmd := m.updateActions(keyMsg(tt.key))
			pressed, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantFired, client.fired)
			assert.Nil(t, got.banner, "nothing here was refused")
		})
	}
}

// TestActionsFireInSuccession covers what staying on the list is for. Two actions on the same
// agent is the ordinary case, opening a pull request and then the issue behind it, and leaving
// after the first would mean walking back to the same row to fire the second.
func TestActionsFireInSuccession(t *testing.T) {
	client := &fakeActions{}
	m := actionsModel(client, appconfig.DefaultKeybindings())

	fired, cmd := m.updateActions(keyMsg("enter"))
	m, ok := fired.(model)
	require.True(t, ok)
	m = settle(t, m, cmd)

	moved, _ := m.updateActions(keyMsg("j"))
	m, ok = moved.(model)
	require.True(t, ok)

	fired, cmd = m.updateActions(keyMsg("enter"))
	pressed, ok := fired.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	assert.Equal(t, screenActions, got.screen)
	assert.Equal(t, []string{"a1/open-pr", "a1/unrunnable"}, client.fired, "both fired without leaving the list")
}

// TestActionRunIsReported covers a refusal from the service. Every failure happens before the
// process starts, so the panel is the only thing that can say so, and the list stays up because
// the row it is about is on it.
func TestActionRunIsReported(t *testing.T) {
	client := &fakeActions{err: connect.NewError(
		connect.CodeFailedPrecondition,
		errors.New("the file is not executable: run chmod +x on it"),
	)}
	m := actionsModel(client, appconfig.DefaultKeybindings())

	updated, cmd := m.updateActions(keyMsg("enter"))
	pressed, ok := updated.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	assert.Equal(t, screenActions, got.screen, "the list stays up so the row can be seen")
	require.NotNil(t, got.banner)
	assert.Contains(t, got.banner.Summary, "open-pr")
	assert.Contains(t, got.banner.Detail, "chmod", "the reason comes off the service, not the panel")
}

// TestActionOpensOnTheHoveredAgent covers the list being opened from a row. It carries the agent
// it was opened on rather than reading the cursor back later, since the refresh keeps running
// while it is up and can move the rows under it.
func TestActionOpensOnTheHoveredAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)
	m.api = api{actions: &fakeActions{}}

	entered, cmd := m.updateAgents(keyMsg("a"))
	opened, ok := entered.(model)
	require.True(t, ok)
	require.Equal(t, screenActions, opened.screen)
	assert.Equal(t, "auth", opened.actions.agent.Name)

	got := settle(t, opened, cmd)

	require.Len(t, got.actions.items, 2, "the list is filled in from the service")
	assert.Equal(t, "open-pr", got.actions.items[0].name)
	assert.Contains(t, stagedDocument(t), "echo hello", "the file's own text is what the stage shows")
}

// TestActionItemsSkipUnderscoredNames covers the naming convention that keeps a file off the
// list. Shell an action sources for a shared function is still a file the service offers, and
// prefixing it is how the user says it is not something to run on its own.
func TestActionItemsSkipUnderscoredNames(t *testing.T) {
	tests := []struct {
		name   string
		listed []*actionv1.Action
		want   []string
	}{
		{
			name:   "an underscored action is left out",
			listed: []*actionv1.Action{{Name: "_setup"}},
			want:   []string{},
		},
		{
			name:   "an underscore inside a name is not a prefix",
			listed: []*actionv1.Action{{Name: "open_pr"}},
			want:   []string{"open_pr"},
		},
		{
			name:   "the rest of the directory is listed around one",
			listed: []*actionv1.Action{{Name: "open-pr"}, {Name: "_setup"}, {Name: "close-pr"}},
			want:   []string{"open-pr", "close-pr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := actionItemsFrom(tt.listed)

			got := make([]string, 0, len(items))
			for _, item := range items {
				got = append(got, item.name)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestActionOnAWorkflowHeaderDoesNothing pins that the key needs a row. Every other per-row key
// behaves this way on a header, and there is no such thing as an action on a workflow.
func TestActionOnAWorkflowHeaderDoesNothing(t *testing.T) {
	m := infoModel(&recordingTmux{})
	m.api = api{actions: &fakeActions{}}
	m.tree.cursor = 0

	updated, _ := m.updateAgents(keyMsg("a"))
	got, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, got.screen)
}

// TestActionListingFailureIsReported covers a service that would not answer. The list stays
// empty, and the banner says so rather than the stage claiming the directory holds nothing.
func TestActionListingFailureIsReported(t *testing.T) {
	client := &fakeActions{listErr: errors.New("connection refused")}
	m := actionsModel(client, appconfig.DefaultKeybindings())
	m.actions.items = nil

	got := settle(t, m, m.listActionsCmd())

	assert.Empty(t, got.actions.items)
	require.NotNil(t, got.banner)
	assert.Contains(t, got.banner.Summary, "Could not read your actions")
}

// TestActionListingArrivingAfterTheScreenWasLeft covers the listing outliving the visit that
// asked for it. It is read once on the way in and the directory can be slow, so backing out
// before it lands is ordinary. It has to be dropped whole, the way the refresh drops a pass the
// panel has moved past, or it writes the stage of whichever screen is up by then.
func TestActionListingArrivingAfterTheScreenWasLeft(t *testing.T) {
	client := &fakeActions{}
	m := actionsModel(client, appconfig.DefaultKeybindings())
	pending := m.listActionsCmd()

	// The list carries a row of its own, so a listing that lands writes over something
	// recognisable rather than filling in what was empty anyway.
	m.actions.items = []actionItem{{name: "from-the-visit-that-ended"}}

	left, _ := m.updateActions(keyMsg("esc"))
	departed, ok := left.(model)
	require.True(t, ok)
	require.Equal(t, screenAgents, departed.screen)

	got := settle(t, departed, pending)

	assert.Equal(t, screenAgents, got.screen, "a listing does not pull the panel back")
	require.Len(t, got.actions.items, 1)
	assert.Equal(t, "from-the-visit-that-ended", got.actions.items[0].name, "the listing was dropped")
}

// TestActionListingFailureAfterTheScreenWasLeftIsSilent covers the same race on the failing
// side. A directory that could not be read is worth a banner over the list it is about and
// nothing at all over a screen it says nothing about.
func TestActionListingFailureAfterTheScreenWasLeftIsSilent(t *testing.T) {
	client := &fakeActions{listErr: errors.New("connection refused")}
	m := actionsModel(client, appconfig.DefaultKeybindings())
	pending := m.listActionsCmd()

	left, _ := m.updateActions(keyMsg("esc"))
	departed, ok := left.(model)
	require.True(t, ok)

	got := settle(t, departed, pending)

	assert.Equal(t, screenAgents, got.screen)
	assert.Nil(t, got.banner, "nothing banners over a screen the failure is not about")
}

// TestFencedBlock covers the shared block any screen wraps somebody else's text in. The fence has
// to beat whatever is inside it, or text carrying a fence of its own closes the block early and
// leaves the rest of itself rendering as prose.
func TestFencedBlock(t *testing.T) {
	tests := []struct {
		name        string
		text        []byte
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "a compiled program has nothing to read",
			text:        []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01},
			wantContain: []string{"not text"},
			wantAbsent:  []string{"```", "ELF"},
		},
		{
			name:        "nothing at all says so rather than drawing an empty block",
			text:        []byte("\n"),
			wantContain: []string{"_empty_"},
			wantAbsent:  []string{"```"},
		},
		{
			name:        "text carrying a fence is wrapped in a longer one",
			text:        []byte("echo '```go'\n"),
			wantContain: []string{"````\necho '```go'\n````"},
		},
		{
			name:        "text carrying a longer fence is wrapped in a longer one still",
			text:        []byte("````\n"),
			wantContain: []string{"`````\n````\n`````"},
		},
		{
			name:        "plain text goes in as it was written",
			text:        []byte("#!/bin/sh\nopen \"$url\"\n"),
			wantContain: []string{"```\n#!/bin/sh\nopen \"$url\"\n```"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fencedBlock(tt.text)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestActionMarkdown covers what the action document adds around the shared block, which is the
// name, the chmod a file without the bit wants, and whether the service read the file whole.
func TestActionMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		action      actionItem
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "a file without the executable bit says what it wants",
			action:      actionItem{name: "unrunnable", preview: []byte("#!/bin/sh\n")},
			wantContain: []string{"# unrunnable", "chmod +x"},
		},
		{
			name: "a binary read to the limit does not claim to be a partial script",
			action: actionItem{
				name:       "compiled",
				executable: true,
				preview:    []byte{0x7f, 'E', 'L', 'F', 0x00},
				truncated:  true,
			},
			wantContain: []string{"not text"},
			wantAbsent:  []string{"only the start"},
		},
		{
			name: "a head of a longer file says it is only the start",
			action: actionItem{
				name:       "long",
				executable: true,
				preview:    []byte("#!/bin/sh\necho one\n"),
				truncated:  true,
			},
			wantContain: []string{"only the start of the file is shown"},
		},
		{
			name: "a runnable script is its name over its own text",
			action: actionItem{
				name:       "open-pr",
				executable: true,
				preview:    []byte("#!/bin/sh\nopen \"$url\"\n"),
			},
			wantContain: []string{"# open-pr", "```\n#!/bin/sh"},
			wantAbsent:  []string{"chmod", "only the start"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionMarkdown(tt.action)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestActionsViewNamesTheAgentAndDimsWhatCannotRun covers what the list says. The agent is named
// because the list acts on the row it was opened from rather than on whatever the agents list is
// hovering later, and the legend names the chmod, since a dim row cannot say why it is dim.
func TestActionsViewNamesTheAgentAndDimsWhatCannotRun(t *testing.T) {
	m := actionsModel(&fakeActions{}, appconfig.DefaultKeybindings())
	m.actions.cursor = 1

	view := m.actionsView()

	assert.Contains(t, view, "Run Action")
	assert.Contains(t, view, "open-pr")
	assert.Contains(t, view, "unrunnable")
	assert.Contains(t, view, "chmod +x to run", "the legend says what the run key wants")
}

// TestNoActionsSaysWhereToPutThem covers the empty case. The panel no longer finds the
// directory, so the pane beside an empty list says what an action is and where the default lives.
func TestNoActionsSaysWhereToPutThem(t *testing.T) {
	assert.Contains(t, noActionsMarkdown, ".config/weave/actions")
	assert.Contains(t, noActionsMarkdown, "WEAVE_AGENT")
}

// TestActionPreviewRedrawsOnlyOnAChange covers the stage leaving its pane alone while what it
// holds is unchanged, which is what keeps a restage from restarting the pager. The document
// carries the file's text, so a listing that came back changed is a different document.
func TestActionPreviewRedrawsOnlyOnAChange(t *testing.T) {
	tmux := &recordingTmux{}
	m := actionsModel(&fakeActions{}, appconfig.DefaultKeybindings())
	m.stage = NewStage(tmux, appconfig.DefaultLayout())
	m.stage.Init("%0")

	m.stageAction()
	m.stageAction()
	require.Len(t, tmux.respawns, 1, "showing the same document twice draws once")

	m.actions.items[0].preview = []byte("#!/bin/sh\necho edited\n")
	m.stageAction()

	require.Len(t, tmux.respawns, 2, "an edited file is a different document")
	assert.Contains(t, stagedDocument(t), "echo edited")
}

// actionsModel builds the panel sitting on the actions list of one agent, with a runnable action
// and one that is not executable.
func actionsModel(client actionv1connect.ActionServiceClient, keys appconfig.Keybindings) model {
	tmux := &recordingTmux{}
	stage := NewStage(tmux, appconfig.DefaultLayout())
	stage.Init("%0")

	return model{
		api:    api{actions: client},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		stage:  stage,
		keys:   keys,
		screen: screenActions,
		actions: actionState{
			items: actionItemsFrom(listedActions()),
			agent: agentstore.Agent{ID: "a1", Name: "auth", Cwd: "/repo"},
		},
	}
}

// listedActions is what the fake service answers with, one action that will run, one waiting
// on a chmod, and one underscored, which the service offers and the list leaves out.
func listedActions() []*actionv1.Action {
	return []*actionv1.Action{
		{Name: "open-pr", Executable: true, Preview: []byte("#!/bin/sh\necho hello\n")},
		{Name: "unrunnable", Preview: []byte("#!/bin/sh\necho hello\n")},
		{Name: "_internal", Executable: true, Preview: []byte("#!/bin/sh\necho hello\n")},
	}
}

// fakeActions answers the calls the screen makes, recording what it was asked to fire as
// agent/action so a case can state both at once. The embedded interface is nil, so any other
// method panics rather than quietly returning a zero value.
type fakeActions struct {
	actionv1connect.ActionServiceClient
	// err refuses a run and listErr refuses a listing, which are the two things the panel
	// has to report since neither is visible anywhere else.
	err     error
	listErr error
	fired   []string
}

func (f *fakeActions) ListActions(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[actionv1.ListActionsResponse], error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return connect.NewResponse(&actionv1.ListActionsResponse{Actions: listedActions()}), nil
}

func (f *fakeActions) RunAction(_ context.Context, req *connect.Request[actionv1.RunActionRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.fired = append(f.fired, req.Msg.GetAgentId()+"/"+req.Msg.GetAction())

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// stagedDocument is the text of what the stage last rendered, read back off the file the pager in
// its pane is showing. The colours around a word sit between it and the next, so they are
// stripped to read it.
func stagedDocument(t *testing.T) string {
	t.Helper()

	shown, err := os.ReadFile(filepath.Join(os.TempDir(), "weave-stage.ansi"))
	require.NoError(t, err)

	return ansi.Strip(string(shown))
}
