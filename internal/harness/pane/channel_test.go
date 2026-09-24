package pane

import (
	"errors"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/harnesstest"

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
			name:  "a pane showing nothing weave can read",
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
			terminal := &fakeTerminal{
				frame:  tt.frame,
				inMode: tt.inMode,
				shell:  tt.shell,
			}
			p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}})

			ready, err := p.Ready(t.Context(), target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ready)
			assert.Empty(t, terminal.actions, "asking whether a box would take something touches nothing")
		})
	}
}

// TestSendPastesIntoAnEmptyBox pins that an idle pane takes the paste and nothing around it.
func TestSendPastesIntoAnEmptyBox(t *testing.T) {
	terminal := &fakeTerminal{frame: harnesstest.FrameIdle}
	p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}})

	sent, err := p.Send(t.Context(), target, "weave: read_mail")
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"paste"}, terminal.actions, "an empty box needs no cut")
	assert.Equal(t, []string{"weave: read_mail"}, terminal.pasted)
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
			terminal := &fakeTerminal{frame: harnesstest.FrameIdle, shell: true}
			p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: tt.harnesses})

			sent, err := p.Send(t.Context(), target, "weave: read_mail")
			require.NoError(t, err)
			assert.False(t, sent)
			assert.Empty(t, terminal.actions)
		})
	}
}

// TestSendToAnUnknownHarnessPastesAnyway pins that a frame nothing can read is written to
// rather than held forever.
func TestSendToAnUnknownHarnessPastesAnyway(t *testing.T) {
	terminal := &fakeTerminal{frame: harnesstest.FrameWorking}
	p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{}})

	sent, err := p.Send(t.Context(), target, "weave: read_mail")
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"paste"}, terminal.actions)
}

// TestSendToAComposingPane pins which draft weave cuts to get a line past and which it leaves
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
			terminal := &fakeTerminal{
				frame:      frame,
				afterCut:   tt.afterCut,
				clientAt:   tt.clientAt,
				staleReads: tt.staleReads,
			}
			p := composingPane(t, terminal, &now)

			sent, err := deliver(t.Context(), t, p)
			require.NoError(t, err)
			require.False(t, sent, "the first look at a draft only starts its clock")
			require.Empty(t, terminal.actions)

			now = now.Add(tt.held)

			sent, err = deliver(t.Context(), t, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, sent)
			assert.Equal(t, tt.wantActions, terminal.actions)

			if tt.want {
				require.Len(t, terminal.pasted, 1)
				assert.Contains(t, terminal.pasted[0], "read_mail")
			} else {
				assert.Empty(t, terminal.pasted, "a draft weave will not cut is never pasted over")
			}
		})
	}
}

// TestSendTimesADraftFromWhenItLastMoved pins that a box somebody is editing is never still,
// however long weave has been watching it.
func TestSendTimesADraftFromWhenItLastMoved(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing + ": one"}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)
	terminal.frame = harnesstest.FrameComposing + ": one and a bit"

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	assert.False(t, sent, "the text moved, so the clock starts again")
	assert.Empty(t, terminal.actions)

	now = now.Add(draftStaleAfter)

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"cut", "paste", "yank"}, terminal.actions)
}

// TestSendForgetsADraftOnceTheBoxIsEmpty pins that text typed after the box was emptied is timed
// from when it appeared rather than from the draft before it.
func TestSendForgetsADraftOnceTheBoxIsEmpty(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)
	terminal.frame = harnesstest.FrameIdle

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, []string{"paste"}, terminal.actions, "an empty box needs no cut")

	terminal.frame = harnesstest.FrameComposing
	terminal.actions = nil

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	assert.False(t, sent, "the box was empty since, so this draft is new")
	assert.Empty(t, terminal.actions)
}

// TestSendSaysOnceThatAMultiLineDraftIsInTheWay pins the one thing weave does about a box it
// will not touch.
func TestSendSaysOnceThatAMultiLineDraftIsInTheWay(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing + ": first|second"}
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
	assert.Empty(t, terminal.actions)
}

// TestForgetDropsWhatWasRememberedOfABox pins that an agent nothing will look at again leaves
// nothing behind in the draft clock.
func TestForgetDropsWhatWasRememberedOfABox(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)
	require.Len(t, p.drafts.boxes, 1, "the draft was seen, so it is being timed")

	p.Forget(target.AgentID)

	assert.Empty(t, p.drafts.boxes, "nothing will read this box again")
}

// TestSendHandsBackADraftWhenThePasteFails pins that a cut weave made is put back however the
// paste after it turns out.
func TestSendHandsBackADraftWhenThePasteFails(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing, pasteErr: errors.New("tmux said no")}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)

	sent, err = deliver(t.Context(), t, p)
	require.Error(t, err)
	assert.False(t, sent)
	assert.Equal(t, []string{"cut", "paste-failed", "yank"}, terminal.actions)
}

// TestSendReportsAYankItCouldNotMake pins that a hand-back tmux refuses is said out loud
// rather than failing the send that already landed.
func TestSendReportsAYankItCouldNotMake(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing, yankErr: errors.New("tmux said no")}
	p := composingPane(t, terminal, &now)

	ctx, records := loggedContext(t)

	sent, err := deliver(ctx, t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)

	sent, err = deliver(ctx, t, p)
	require.NoError(t, err, "the line landed, so the send is not failed over the hand-back")
	assert.True(t, sent)
	assert.Equal(t, []string{"cut", "paste", "yank-failed"}, terminal.actions)

	written := records()
	require.Len(t, written, 1)
	assert.Equal(t, "ERROR", written[0]["severity_text"])
	assert.Contains(t, written[0]["body"], "kill ring")
}

// TestSendHandsBackADraftWhenTheFrameGoesUnreadable pins the hand-back on the one way out of a
// cut that never learns whether the box came back empty.
func TestSendHandsBackADraftWhenTheFrameGoesUnreadable(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: harnesstest.FrameComposing}
	harnesses := &harnessesFailingAfter{harness: &harnesstest.Fake{}, answer: 3}
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
	assert.Equal(t, []string{"cut", "yank"}, terminal.actions)
	assert.Empty(t, terminal.pasted)
}

// TestInterruptSendsTheHarnessItsOwnKeys pins that stopping an agent is the CLI's own interrupt
// rather than anything weave decides.
func TestInterruptSendsTheHarnessItsOwnKeys(t *testing.T) {
	terminal := &fakeTerminal{frame: harnesstest.FrameWorking}
	p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{InterruptWith: []string{"Escape", "Escape"}}}})

	require.NoError(t, p.Interrupt(t.Context(), target))
	assert.Equal(t, []string{"keys:Escape,Escape"}, terminal.actions)
}

// TestInterruptRefusesWhatItCannotStop pins the two agents there is no interrupt to send.
func TestInterruptRefusesWhatItCannotStop(t *testing.T) {
	tests := []struct {
		name      string
		target    harness.Target
		harnesses harnessGetter
	}{
		{
			name:      "an agent holding no pane",
			target:    harness.Target{AgentID: "agent-a", HarnessID: harnesstest.FakeID},
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		},
		{
			name:      "a harness weave cannot look up",
			target:    target,
			harnesses: harnesstest.Registry{},
		},
		{
			name:      "a harness with no keys that stop it",
			target:    target,
			harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{InterruptWith: []string{}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := &fakeTerminal{frame: harnesstest.FrameWorking}
			p := NewPane(NewPaneParams{Terminal: terminal, Harnesses: tt.harnesses})

			require.Error(t, p.Interrupt(t.Context(), tt.target))
			assert.Empty(t, terminal.actions)
		})
	}
}
