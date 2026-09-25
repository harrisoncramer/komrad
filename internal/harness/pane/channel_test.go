package pane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/logging"
	"komrad/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadyRefusesAPaneThatIsNotListening pins the frames a channel will not write into at all.
func TestReadyRefusesAPaneThatIsNotListening(t *testing.T) {
	tests := []struct {
		name   string
		frame  string
		inMode bool
		shell  bool
		want   bool
	}{
		{
			name:  "an agent part-way through a turn",
			frame: harnesstest.FrameWorking,
		},
		{
			name:   "a pane sitting in one of tmux's own modes",
			frame:  harnesstest.FrameIdle,
			inMode: true,
		},
		{
			name:  "an input box left on screen by a CLI that has exited",
			frame: harnesstest.FrameIdle,
			shell: true,
		},
		{
			name:  "a pane showing nothing komrad can read",
			frame: "",
		},
		{
			name:  "an empty input box",
			frame: harnesstest.FrameIdle,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := terminalShowing(tt.frame)
			terminal.InMode[target.Pane] = tt.inMode
			if tt.shell {
				terminal.Commands[target.Pane] = "zsh"
			}
			pane := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}})

			ready, err := pane.Ready(t.Context(), target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ready)
			assert.Empty(t, terminal.Sent, "asking whether a box would take something touches nothing")
			assert.Empty(t, terminal.Edits, "asking whether a box would take something touches nothing")
		})
	}
}

// TestSendPastesIntoAnEmptyBox pins that an idle pane takes the paste and nothing around it.
func TestSendPastesIntoAnEmptyBox(t *testing.T) {
	terminal := terminalShowing(harnesstest.FrameIdle)
	pane := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}})

	sent, err := pane.Send(t.Context(), target, "komrad: read_mail")
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"paste"}, terminal.Edits[target.Pane], "an empty box needs no cut")
	assert.Equal(t, [][]string{{"komrad: read_mail"}}, terminal.Sent[target.Pane])
}

// TestSendRefusesAPaneWhoseCLIHasExited pins that a pane whose foreground program is a shell is
// never written into, whatever its frame still shows.
func TestSendRefusesAPaneWhoseCLIHasExited(t *testing.T) {
	tests := []struct {
		name      string
		harnesses harnessGetter
	}{
		{
			name:      "an input box the dead CLI left drawn",
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		},
		{
			name:      "a frame no harness can read",
			harnesses: harnesstest.Registry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := terminalShowing(harnesstest.FrameIdle)
			terminal.Commands[target.Pane] = "zsh"
			p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: tt.harnesses})

			sent, err := p.Send(t.Context(), target, "komrad: read_mail")
			require.NoError(t, err)
			assert.False(t, sent)
			assert.Empty(t, terminal.Sent)
			assert.Empty(t, terminal.Edits)
		})
	}
}

// TestSendToAnUnknownHarnessPastesAnyway pins that a frame nothing can read is written to
// rather than held forever.
func TestSendToAnUnknownHarnessPastesAnyway(t *testing.T) {
	terminal := terminalShowing(harnesstest.FrameWorking)
	pane := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{}})

	sent, err := pane.Send(t.Context(), target, "komrad: read_mail")
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"paste"}, terminal.Edits[target.Pane])
}

// TestSendToAComposingPane pins which draft komrad cuts to get a line past and which it leaves
// where it is.
func TestSendToAComposingPane(t *testing.T) {
	tests := []struct {
		name        string
		frame       string
		afterCut    string
		clientAt    bool
		staleReads  int
		held        time.Duration
		want        bool
		wantActions []string
	}{
		{
			name: "a draft somebody may still be in the middle of typing",
			held: draftStaleAfter - time.Second,
		},
		{
			name:        "a draft nobody has moved for long enough, at a pane nobody is at",
			held:        draftStaleAfter,
			want:        true,
			wantActions: []string{"cut", "paste", "yank"},
		},
		{
			name:     "a still draft at a pane somebody is sitting at",
			held:     draftStaleAfter,
			clientAt: true,
		},
		{
			name:  "a still draft spread over more than one line",
			frame: harnesstest.FrameComposing + ": first|second",
			held:  draftStaleAfter,
		},
		{
			name:        "a cut whose frame the CLI has yet to repaint",
			held:        draftStaleAfter,
			staleReads:  5,
			want:        true,
			wantActions: []string{"cut", "paste", "yank"},
		},
		{
			name:        "a cut the frame never shows, which is a box that held more than the cut took",
			held:        draftStaleAfter,
			staleReads:  cutSettleReads,
			wantActions: []string{"cut", "yank"},
		},
		{
			name:        "a cut that left text in the box",
			held:        draftStaleAfter,
			afterCut:    harnesstest.FrameComposing,
			wantActions: []string{"cut", "yank"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := tt.frame
			if frame == "" {
				frame = harnesstest.FrameComposing
			}

			now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
			afterCut := tt.afterCut
			if afterCut == "" {
				afterCut = harnesstest.FrameIdle
			}

			terminal := terminalShowing(frame)
			terminal.ClientAt[target.Pane] = tt.clientAt
			terminal.CutFrames[target.Pane] = append(slices.Repeat([]string{frame}, tt.staleReads), afterCut)
			p := composingPane(t, terminal, &now)

			sent, err := deliver(t.Context(), t, p)
			require.NoError(t, err)
			require.False(t, sent, "the first look at a draft only starts its clock")
			require.Empty(t, terminal.Edits[target.Pane])

			now = now.Add(tt.held)

			sent, err = deliver(t.Context(), t, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, sent)
			assert.Equal(t, tt.wantActions, terminal.Edits[target.Pane])

			if tt.want {
				require.Len(t, terminal.Sent[target.Pane], 1)
				assert.Contains(t, terminal.Sent[target.Pane][0][0], "read_mail")
			} else {
				assert.Empty(t, terminal.Sent[target.Pane], "a draft komrad will not cut is never pasted over")
			}
		})
	}
}

// TestSendTimesADraftFromWhenItLastMoved pins that a box somebody is editing is never still,
// however long komrad has been watching it.
func TestSendTimesADraftFromWhenItLastMoved(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing + ": one")
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)
	terminal.Captures[target.Pane] = harnesstest.FrameComposing + ": one and a bit"

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	assert.False(t, sent, "the text moved, so the clock starts again")
	assert.Empty(t, terminal.Edits[target.Pane])

	now = now.Add(draftStaleAfter)

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"cut", "paste", "yank"}, terminal.Edits[target.Pane])
}

// TestSendForgetsADraftOnceTheBoxIsEmpty pins that text typed after the box was emptied is timed
// from when it appeared rather than from the draft before it.
func TestSendForgetsADraftOnceTheBoxIsEmpty(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing)
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)
	terminal.Captures[target.Pane] = harnesstest.FrameIdle

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, []string{"paste"}, terminal.Edits[target.Pane], "an empty box needs no cut")

	terminal.Captures[target.Pane] = harnesstest.FrameComposing
	terminal.Edits[target.Pane] = nil

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	assert.False(t, sent, "the box was empty since, so this draft is new")
	assert.Empty(t, terminal.Edits[target.Pane])
}

// TestSendSaysOnceThatAMultiLineDraftIsInTheWay pins the one thing komrad does about a box it
// will not touch.
func TestSendSaysOnceThatAMultiLineDraftIsInTheWay(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing + ": first|second")
	p := composingPane(t, terminal, &now)

	ctx, records := loggedContext(t)

	sent, err := deliver(ctx, t, p)
	require.NoError(t, err)
	require.False(t, sent)
	require.Empty(t, records(), "a draft that has not stood long enough is nobody's business yet")

	now = now.Add(draftStaleAfter)

	for range 3 {
		sent, err = deliver(ctx, t, p)
		require.NoError(t, err)
		require.False(t, sent)
	}

	written := records()
	require.Len(t, written, 1, "three passes over the same draft say it once")
	assert.Equal(t, "WARN", written[0]["severity_text"])
	assert.Empty(t, terminal.Edits[target.Pane])
}

// TestForgetDropsWhatWasRememberedOfABox pins that an agent nothing will look at again leaves
// nothing behind in the draft clock.
func TestForgetDropsWhatWasRememberedOfABox(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing)
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)
	require.Len(t, p.drafts.boxes, 1, "the draft was seen, so it is being timed")

	p.Forget(target.AgentID)

	assert.Empty(t, p.drafts.boxes, "nothing will read this box again")
}

// TestSendHandsBackADraftWhenThePasteFails pins that a cut komrad made is put back however the
// paste after it turns out.
func TestSendHandsBackADraftWhenThePasteFails(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing)
	terminal.PasteErr = errors.New("tmux said no")
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)

	sent, err = deliver(t.Context(), t, p)
	require.Error(t, err)
	assert.False(t, sent)
	assert.Equal(t, []string{"cut", "paste", "yank"}, terminal.Edits[target.Pane])
	assert.Empty(t, terminal.Sent[target.Pane])
}

// TestSendReportsAYankItCouldNotMake pins that a hand-back tmux refuses is said out loud
// rather than failing the send that already landed.
func TestSendReportsAYankItCouldNotMake(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing)
	terminal.YankErr = errors.New("tmux said no")
	p := composingPane(t, terminal, &now)

	ctx, records := loggedContext(t)

	sent, err := deliver(ctx, t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)

	sent, err = deliver(ctx, t, p)
	require.NoError(t, err, "the line landed, so the send is not failed over the hand-back")
	assert.True(t, sent)
	assert.Equal(t, []string{"cut", "paste", "yank"}, terminal.Edits[target.Pane])

	written := records()
	require.Len(t, written, 1)
	assert.Equal(t, "ERROR", written[0]["severity_text"])
	assert.Contains(t, written[0]["body"], "kill ring")
}

// TestSendHandsBackADraftWhenTheFrameGoesUnreadable pins the hand-back on the one way out of a
// cut that never learns whether the box came back empty.
func TestSendHandsBackADraftWhenTheFrameGoesUnreadable(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := terminalShowing(harnesstest.FrameComposing)
	reads := 0
	harnesses := harnesstest.Registry{
		Harness: &harnesstest.Fake{},
		BeforeGet: func(string) error {
			reads++
			if reads > 3 {
				return errors.New("the frame went unreadable")
			}

			return nil
		},
	}
	p := NewPane(NewPaneParams{
		Terminal:  terminal,
		Harnesses: harnesses,
		Now:       func() time.Time { return now },
	})

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)

	sent, err = deliver(t.Context(), t, p)
	require.Error(t, err, "the frame the cut is read out of went unreadable")
	assert.False(t, sent)
	assert.Equal(t, []string{"cut", "yank"}, terminal.Edits[target.Pane])
	assert.Empty(t, terminal.Sent[target.Pane])
}

// TestInterruptSendsTheHarnessItsOwnKeys pins that stopping an agent is the CLI's own interrupt
// rather than anything komrad decides.
func TestInterruptSendsTheHarnessItsOwnKeys(t *testing.T) {
	terminal := terminalShowing(harnesstest.FrameWorking)
	pane := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{InterruptWith: []string{"Escape", "Escape"}}}})

	require.NoError(t, pane.Interrupt(t.Context(), target))
	assert.Equal(t, [][]string{{"Escape", "Escape"}}, terminal.Sent[target.Pane])
}

// TestInterruptRefusesWhatItCannotStop pins the two agents there is no interrupt to send.
func TestInterruptRefusesWhatItCannotStop(t *testing.T) {
	tests := []struct {
		name      string
		target    harness.Target
		harnesses harnessGetter
		wantErr   string
	}{
		{
			name:      "an agent holding no pane",
			target:    harness.Target{AgentID: "agent-a", HarnessID: harnesstest.FakeID},
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
			wantErr:   "not in a pane",
		},
		{
			name:      "a harness komrad cannot look up",
			target:    target,
			harnesses: harnesstest.Registry{},
			wantErr:   "failed to find the harness",
		},
		{
			name:      "a harness with no keys that stop it",
			target:    target,
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{InterruptWith: []string{}}},
			wantErr:   "no keys that interrupt it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := terminalShowing(harnesstest.FrameWorking)
			p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: tt.harnesses})

			require.ErrorContains(t, p.Interrupt(t.Context(), tt.target), tt.wantErr)
			assert.Empty(t, terminal.Sent)
		})
	}
}

// target is the agent every test in this package speaks to.
var target = harness.Target{
	AgentID:   "agent-a",
	Pane:      "%1",
	HarnessID: harnesstest.FakeID,
}

// terminalShowing is a tmux whose pane shows the frame under a running CLI, and an empty box once its line is cut.
func terminalShowing(frame string) *tmuxtest.Fake {
	terminal := tmuxtest.NewFake()
	terminal.Captures[target.Pane] = frame
	terminal.Commands[target.Pane] = "claude"
	terminal.CutFrames[target.Pane] = []string{harnesstest.FrameIdle}

	return terminal
}

// composingPane is a channel over one pane, whose clock a test moves by writing to the time it
// returns.
func composingPane(t *testing.T, terminal *tmuxtest.Fake, now *time.Time) *Pane {
	t.Helper()

	return NewPane(NewPaneParams{
		Terminal:  terminal,
		Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		Now:       func() time.Time { return *now },
	})
}

// deliver is the two calls the delivery service makes: ask whether the line would be taken,
// then say it.
func deliver(ctx context.Context, t *testing.T, p *Pane) (bool, error) {
	t.Helper()

	ready, err := p.Ready(ctx, target)
	if err != nil || !ready {
		return false, err
	}

	return p.Send(ctx, target, "komrad: 1 piece(s) of unread mail, call read_mail")
}

func loggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelDebug))

	return ctx, func() []map[string]any {
		records := make([]map[string]any, 0, 2)
		for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			records = append(records, record)
		}

		return records
	}
}
