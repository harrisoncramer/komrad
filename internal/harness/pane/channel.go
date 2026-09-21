package pane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/harness"
	"weave/internal/logging"
)

// plan is what weave may do to an agent's input box.
type plan int

const (
	planRefuse plan = iota

	// planPaste is an empty box, which takes the text on its own.
	planPaste

	// planCutAndRestore is a box holding a draft nobody is moving.
	planCutAndRestore
)

// Ready reports whether the agent's input box would take something right now, leaving the pane
// as it found it.
func (p *Pane) Ready(ctx context.Context, target harness.Target) (bool, error) {
	if p.terminal.IsPaneInMode(target.Pane) {
		return false, nil
	}

	return p.planFor(ctx, target) != planRefuse, nil
}

// Send puts text in the agent's input box and submits it, reporting whether the box took it.
func (p *Pane) Send(ctx context.Context, target harness.Target, text string) (bool, error) {
	chosen := p.planFor(ctx, target)
	if chosen == planRefuse {
		return false, nil
	}

	if chosen == planCutAndRestore {
		cut, err := p.cutDraft(ctx, target)
		if err != nil || !cut {
			return false, err
		}

		// A cut that came back empty hands the line on to here, and deferring the yank puts it
		// after the paste, where it was measured to belong, on every way out rather than only
		// the one that reaches the end.
		defer p.restoreDraft(ctx, target)
	}

	// Typed characters run as commands in a CLI sitting in a vi keymap, and a newline of its own
	// invocation can be separated from the line it submits.
	if err := p.terminal.PasteAndSubmit(target.Pane, text); err != nil {
		return false, fmt.Errorf("failed to paste at agent %q: %w", target.AgentID, err)
	}

	return true, nil
}

// Interrupt types whatever the agent's own CLI reads as an interrupt into its pane.
func (p *Pane) Interrupt(_ context.Context, target harness.Target) error {
	if target.Pane == "" {
		return fmt.Errorf("agent %q is not in a pane to interrupt", target.AgentID)
	}

	found, err := p.harnesses.GetByID(target.HarnessID)
	if err != nil {
		return fmt.Errorf("failed to find the harness of agent %q: %w", target.AgentID, err)
	}

	keys := found.InterruptKeys()
	if len(keys) == 0 {
		return fmt.Errorf("the %q harness has no keys that interrupt it", target.HarnessID)
	}

	if err := p.terminal.SendKeys(target.Pane, keys...); err != nil {
		return fmt.Errorf("failed to interrupt agent %q: %w", target.AgentID, err)
	}

	return nil
}

// planFor reads the pane for what weave may do to the input box it is showing.
func (p *Pane) planFor(ctx context.Context, target harness.Target) plan {
	if !p.terminal.IsPaneRunningCLI(target.Pane) {
		return planRefuse
	}

	input, err := p.readInput(target)
	if err != nil {
		// No frame of an unknown harness can be recognised, so its mail would otherwise never go
		// anywhere.
		return planPaste
	}

	switch input.Activity {
	case harness.ActivityIdle:
		p.drafts.forget(target.AgentID)

		return planPaste
	case harness.ActivityComposing:
		return p.planForDraft(ctx, target, input)
	case harness.ActivityWorking, harness.ActivityWaiting, harness.ActivityUnknown, harness.ActivityUnrecognized:
		// A CLI still starting up takes the line into the terminal buffer and loses the newline.
		return planRefuse
	}

	return planRefuse
}

// planForDraft is what weave may do to a box already holding text.
func (p *Pane) planForDraft(ctx context.Context, target harness.Target, input harness.Input) plan {
	since := p.drafts.since(target.AgentID, input.Text(), p.now())

	switch {
	case p.now().Sub(since) < draftStaleAfter:
		return planRefuse
	case p.terminal.IsClientAtPane(target.Pane, clientPresenceWindow):
		return planRefuse
	case !input.IsOneLine():
		p.warnAboutDraft(ctx, target)

		return planRefuse
	}

	return planCutAndRestore
}

// warnAboutDraft says once that a box weave will not touch is holding an agent up.
func (p *Pane) warnAboutDraft(ctx context.Context, target harness.Target) {
	if !p.drafts.claimWarning(target.AgentID) {
		return
	}

	// The kill ring holds one entry, so a box of more than one line has no cut weave could put back.
	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "an agent's mail is waiting on a draft of more than one line that nobody is moving",
		slog.String(logging.TargetAgentID, target.AgentID))
}

// restoreDraft hands an agent back the line weave cut out of its input box, reporting rather
// than failing over.
func (p *Pane) restoreDraft(ctx context.Context, target harness.Target) {
	if err := p.terminal.YankLine(target.Pane); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to hand back the draft weave cut out of an agent's input box, which is now only in its CLI's kill ring",
			append(logging.Err(err), slog.String(logging.TargetAgentID, target.AgentID))...)
	}
}

// cutDraft takes the draft out of an agent's input box, reporting whether the box came back
// empty.
func (p *Pane) cutDraft(ctx context.Context, target harness.Target) (cut bool, err error) {
	if err := p.terminal.CutLine(target.Pane); err != nil {
		return false, fmt.Errorf("failed to cut the draft of agent %q: %w", target.AgentID, err)
	}

	// The line is out of the box from here on, so every way out that does not hand it to the
	// caller puts it back.
	defer func() {
		if !cut {
			p.restoreDraft(ctx, target)
		}
	}()

	empty, err := p.waitForAnEmptyBox(target)
	if err != nil {
		return false, err
	}
	if !empty {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "cutting an agent's input box left text behind, so nothing was delivered",
			slog.String(logging.TargetAgentID, target.AgentID))

		return false, nil
	}

	return true, nil
}

// waitForAnEmptyBox reads the frame until it shows the cut.
func (p *Pane) waitForAnEmptyBox(target harness.Target) (bool, error) {
	for read := range cutSettleReads {
		if read > 0 {
			time.Sleep(cutSettleInterval)
		}

		// Codex draws a placeholder in an empty composer, so the box is read through the harness
		// rather than off the capture.
		input, err := p.readInput(target)
		if err != nil {
			return false, fmt.Errorf("failed to read the input box of agent %q: %w", target.AgentID, err)
		}

		if input.Activity == harness.ActivityIdle {
			return true, nil
		}
	}

	return false, nil
}
