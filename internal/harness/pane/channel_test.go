package pane

import (
	"errors"
	"testing"
	"time"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadyRefusesAPaneThatIsNotListening pins the frames a channel will not write into at all.
func TestReadyRefusesAPaneThatIsNotListening(t *testing.T) {
	tests := []struct {
		name   string
		frame  string
		inMode bool
		want   bool
	}{
		{
			name:  "an agent part-way through a turn",
			frame: busyFrame,
		},
		{
			name:   "a pane sitting in one of tmux's own modes",
			frame:  idleFrame,
			inMode: true,
		},
		{
			name:  "a pane showing nothing weave can read",
			frame: "",
		},
		{
			name:  "an empty input box",
			frame: idleFrame,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := &fakeTerminal{frame: tt.frame, inMode: tt.inMode}
			p := New(Params{Terminal: terminal, Harnesses: fakeHarnesses{harness: &fakeHarness{}}})

			ready, err := p.Ready(t.Context(), target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ready)
			assert.Empty(t, terminal.actions, "asking whether a box would take something touches nothing")
		})
	}
}

// TestSendPastesIntoAnEmptyBox pins the whole of what an idle pane takes, which is the paste and
// nothing around it.
func TestSendPastesIntoAnEmptyBox(t *testing.T) {
	terminal := &fakeTerminal{frame: idleFrame}
	p := New(Params{Terminal: terminal, Harnesses: fakeHarnesses{harness: &fakeHarness{}}})

	sent, err := p.Send(t.Context(), target, "weave: read_mail")
	require.NoError(t, err)
	assert.True(t, sent)
	assert.Equal(t, []string{"paste"}, terminal.actions, "an empty box needs no cut")
	assert.Equal(t, []string{"weave: read_mail"}, terminal.pasted)
}

// TestSendToAnUnknownHarnessPastesAnyway pins that a frame nothing can read is written to rather
// than held forever, since no frame of an unknown harness will ever be recognised.
func TestSendToAnUnknownHarnessPastesAnyway(t *testing.T) {
	terminal := &fakeTerminal{frame: busyFrame}
	p := New(Params{Terminal: terminal, Harnesses: fakeHarnesses{}})

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
			frame: composingFrame + ": first|second",
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
			afterCut:    composingFrame,
			wantActions: []string{"cut", "yank"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := tt.frame
			if frame == "" {
				frame = composingFrame
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
	terminal := &fakeTerminal{frame: composingFrame + ": one"}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)
	terminal.frame = composingFrame + ": one and a bit"

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
	terminal := &fakeTerminal{frame: composingFrame}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)

	now = now.Add(draftStaleAfter)
	terminal.frame = idleFrame

	sent, err = deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.True(t, sent)
	require.Equal(t, []string{"paste"}, terminal.actions, "an empty box needs no cut")

	terminal.frame = composingFrame
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
	terminal := &fakeTerminal{frame: composingFrame + ": first|second"}
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
	terminal := &fakeTerminal{frame: composingFrame}
	p := composingPane(t, terminal, &now)

	sent, err := deliver(t.Context(), t, p)
	require.NoError(t, err)
	require.False(t, sent)
	require.Len(t, p.drafts.boxes, 1, "the draft was seen, so it is being timed")

	p.Forget(target.AgentID)

	assert.Empty(t, p.drafts.boxes, "nothing will read this box again")
}

// TestSendHandsBackADraftWhenThePasteFails pins that a cut weave made is put back however the
// paste after it turns out, since the line is a human's and nothing else will ever restore it.
func TestSendHandsBackADraftWhenThePasteFails(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: composingFrame, pasteErr: errors.New("tmux said no")}
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

// TestSendReportsAYankItCouldNotMake pins that a hand-back tmux refuses is said out loud rather
// than failing the send that already landed, since only the person at the pane can reach the
// kill ring holding the line.
func TestSendReportsAYankItCouldNotMake(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	terminal := &fakeTerminal{frame: composingFrame, yankErr: errors.New("tmux said no")}
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
	terminal := &fakeTerminal{frame: composingFrame}
	harnesses := &harnessesFailingAfter{harness: &fakeHarness{}, answer: 3}
	p := New(Params{Terminal: terminal, Harnesses: harnesses, Now: func() time.Time { return now }})

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
	terminal := &fakeTerminal{frame: busyFrame}
	p := New(Params{Terminal: terminal, Harnesses: fakeHarnesses{harness: &fakeHarness{interruptKeys: []string{"Escape", "Escape"}}}})

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
			target:    harness.Target{AgentID: "agent-a", HarnessID: testHarnessID},
			harnesses: fakeHarnesses{harness: &fakeHarness{}},
		},
		{
			name:      "a harness weave cannot look up",
			target:    target,
			harnesses: fakeHarnesses{},
		},
		{
			name:      "a harness with no keys that stop it",
			target:    target,
			harnesses: fakeHarnesses{harness: &fakeHarness{interruptKeys: []string{}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := &fakeTerminal{frame: busyFrame}
			p := New(Params{Terminal: terminal, Harnesses: tt.harnesses})

			require.Error(t, p.Interrupt(t.Context(), tt.target))
			assert.Empty(t, terminal.actions)
		})
	}
}
