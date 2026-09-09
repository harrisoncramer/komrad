package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/harness"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
)

type Delivery int

const (
	DeliveryDone Delivery = iota
	DeliverySoon
	DeliveryLater
)

// deliveryPlan is what a pane's frame will take, which is nothing at all unless the input box is
// empty or holds a draft weave can put back.
type deliveryPlan int

const (
	planRefuse deliveryPlan = iota

	// planPaste is an empty box, which takes the nudge on its own.
	planPaste

	// planCutAndRestore is a box holding a draft nobody is moving, which is cut, nudged, and
	// handed what it held back.
	planCutAndRestore
)

// draftStaleAfter is how long an input box has to hold the same text before what it holds is
// taken for abandoned.
const draftStaleAfter = 30 * time.Second

// cutSettleReads is how many times the frame is read for a cut tmux has already written to the
// pane.
const cutSettleReads = 15

// cutSettleInterval is how long is left between those reads.
const cutSettleInterval = 20 * time.Millisecond

// clientPresenceWindow is how recently somebody has to have pressed a key at an agent's window
// to count as being there.
const clientPresenceWindow = time.Minute

// nudgeRetryAfter is how long a nudge stands before unread mail is taken as evidence it went
// nowhere.
const nudgeRetryAfter = time.Minute

type paneReader interface {
	IsAgentRunning(agent agentstore.Agent) bool
}

type paneReadWriter interface {
	IsPaneInMode(pane string) bool
	CapturePane(pane string) string
	IsClientAtPane(pane string, within time.Duration) bool
	PasteAndSubmit(target, text string) error
	CutLine(target string) error
	YankLine(target string) error
}

type nudgeClaimer interface {
	agentGetter
	ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error)
}

type mailboxReader interface {
	ListPendingMailboxes(ctx context.Context) ([]string, error)
	CountPending(ctx context.Context, toAgentID string) (int64, error)
	ListExpired(ctx context.Context, toAgentID string) ([]mailstore.Mail, error)
}

type DeliveryService struct {
	mailboxReader  mailboxReader
	nudgeClaimer   nudgeClaimer
	paneReader     paneReader
	paneReadWriter paneReadWriter
	harnesses      harnessGetter
	now            func() time.Time
	drafts         drafts
}

type NewDeliveryServiceParams struct {
	MailboxReader mailboxReader
	NudgeClaimer  nudgeClaimer
	PaneReader    paneReader
	PaneWriter    paneReadWriter
	Harnesses     harnessGetter

	// Now is the clock a draft's stillness is measured on, defaulting to time.Now.
	Now func() time.Time
}

// NewDeliveryService builds the service from its dependencies.
func NewDeliveryService(params NewDeliveryServiceParams) *DeliveryService {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &DeliveryService{
		mailboxReader:  params.MailboxReader,
		nudgeClaimer:   params.NudgeClaimer,
		paneReader:     params.PaneReader,
		paneReadWriter: params.PaneWriter,
		harnesses:      params.Harnesses,
		now:            now,
	}
}

// DeliverAll is one pass over every mailbox holding mail, working from the rows each time.
func (s *DeliveryService) DeliverAll(ctx context.Context) error {
	mailboxes, err := s.mailboxReader.ListPendingMailboxes(ctx)
	if err != nil {
		return err
	}

	for _, agentID := range mailboxes {
		if _, err := s.Deliver(ctx, agentID); err != nil {
			return err
		}
	}

	return nil
}

// Deliver makes one attempt at getting an agent to read what is waiting for it, and reports
// what it managed, which is what the tests assert on.
func (s *DeliveryService) Deliver(ctx context.Context, agentID string) (Delivery, error) {
	agent, err := s.nudgeClaimer.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		s.drafts.forget(agentID)

		return DeliveryDone, nil
	}
	if err != nil {
		return DeliveryDone, err
	}
	if agent.ArchivedAt != nil {
		s.drafts.forget(agentID)

		return DeliveryDone, nil
	}

	pending, err := s.mailboxReader.CountPending(ctx, agent.ID)
	if err != nil {
		return DeliveryDone, err
	}
	if pending == 0 {
		s.reportExpired(ctx, *agent)

		return DeliveryDone, nil
	}

	switch {
	case agent.State == agentstore.StateThinking && !isStateUnconfirmed(*agent):
		return DeliverySoon, nil
	case agent.State == agentstore.StateWaiting:
		return DeliveryLater, nil
	case !s.paneReader.IsAgentRunning(*agent):
		return DeliveryLater, nil
	case s.paneReadWriter.IsPaneInMode(agent.Pane):
		return DeliveryLater, nil
	}

	plan := s.planFor(ctx, *agent)
	if plan == planRefuse {
		return DeliveryLater, nil
	}

	claimed, err := s.nudgeClaimer.ClaimNudge(ctx, agent.ID, nudgeRetryAfter)
	if err != nil {
		return DeliveryLater, err
	}
	if !claimed {
		return DeliveryLater, nil
	}

	// The claim is spent whether or not the nudge landed, since every way it can fail has
	// already touched the pane, and a minute of quiet is what keeps a refused cut from becoming
	// a two-second loop of cutting and yanking at a box a person owns.
	delivered, err := s.nudge(ctx, *agent, pending, plan)
	if err != nil {
		return DeliveryLater, err
	}
	if !delivered {
		return DeliveryLater, nil
	}

	return DeliverySoon, nil
}

// planFor reads the pane for what weave may do to the input box it is showing.
func (s *DeliveryService) planFor(ctx context.Context, agent agentstore.Agent) deliveryPlan {
	input, err := s.readInput(agent)
	if err != nil {
		// No frame of an unknown harness can be recognised, so its mail would otherwise never go
		// anywhere.
		return planPaste
	}

	switch input.Activity {
	case harness.ActivityIdle:
		s.drafts.forget(agent.ID)

		return planPaste
	case harness.ActivityComposing:
		return s.planForDraft(ctx, agent, input)
	case harness.ActivityWorking, harness.ActivityWaiting, harness.ActivityUnknown, harness.ActivityUnrecognized:
		// A CLI still starting up takes the line into the terminal buffer and loses the newline.
		return planRefuse
	}

	return planRefuse
}

// planForDraft is what weave may do to a box already holding text, which is nothing at all until
// the text has stopped moving and nobody is at the pane.
func (s *DeliveryService) planForDraft(ctx context.Context, agent agentstore.Agent, input harness.Input) deliveryPlan {
	since := s.drafts.since(agent.ID, input.Text(), s.now())

	switch {
	case s.now().Sub(since) < draftStaleAfter:
		return planRefuse
	case s.paneReadWriter.IsClientAtPane(agent.Pane, clientPresenceWindow):
		return planRefuse
	case !input.IsOneLine():
		s.warnAboutDraft(ctx, agent)

		return planRefuse
	}

	return planCutAndRestore
}

// warnAboutDraft says once that a box weave will not touch is holding an agent's mail up.
func (s *DeliveryService) warnAboutDraft(ctx context.Context, agent agentstore.Agent) {
	if !s.drafts.claimWarning(agent.ID) {
		return
	}

	// The kill ring holds one entry, so a box of more than one line has no cut weave could put back.
	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "an agent's mail is waiting on a draft of more than one line that nobody is moving",
		slog.String(logging.TargetAgentID, agent.ID))
}

// readInput is what the harness of an agent's pane reads out of the frame it is showing.
func (s *DeliveryService) readInput(agent agentstore.Agent) (harness.Input, error) {
	hn, err := s.harnesses.GetByID(agent.Harness)
	if err != nil {
		return harness.Input{}, err
	}

	return hn.ReadInput(s.paneReadWriter.CapturePane(agent.Pane)), nil
}

// nudge tells an agent about its mail, reporting whether the line was submitted.
func (s *DeliveryService) nudge(ctx context.Context, agent agentstore.Agent, pending int64, plan deliveryPlan) (bool, error) {
	line := fmt.Sprintf("weave: %d piece(s) of unread mail, call read_mail", pending)

	if plan == planCutAndRestore {
		cut, err := s.cutDraft(ctx, agent)
		if err != nil || !cut {
			return false, err
		}

		// A cut that came back empty hands the line on to here, and deferring the yank puts it
		// after the paste, where it was measured to belong, on every way out rather than only
		// the one that reaches the end.
		defer s.restoreDraft(ctx, agent)
	}

	// Typed characters run as commands in a CLI sitting in a vi keymap, and a newline of its own
	// invocation can be separated from the line it submits.
	if err := s.paneReadWriter.PasteAndSubmit(agent.Pane, line); err != nil {
		return false, fmt.Errorf("failed to paste at agent %q: %w", agent.ID, err)
	}

	attrs := []slog.Attr{
		slog.String(logging.TargetAgentID, agent.ID),
		slog.Int64(logging.UnreadMail, pending),
	}

	if didLastNudgeGoNowhere(agent) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "told an agent to read its mail again, so the last time went nowhere", attrs...)
	} else {
		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "told an agent to read its mail", attrs...)
	}

	return true, nil
}

// restoreDraft hands an agent back the line weave cut out of its input box, which is only ever
// reported rather than failed over, since the line is by then in the CLI's own kill ring where
// nobody but the person at the pane can reach it.
func (s *DeliveryService) restoreDraft(ctx context.Context, agent agentstore.Agent) {
	if err := s.paneReadWriter.YankLine(agent.Pane); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to hand back the draft weave cut out of an agent's input box, which is now only in its CLI's kill ring",
			append(logging.Err(err), slog.String(logging.TargetAgentID, agent.ID))...)
	}
}

// cutDraft takes the draft out of an agent's input box, reporting whether the box came back
// empty, which is what hands the line on to the caller.
func (s *DeliveryService) cutDraft(ctx context.Context, agent agentstore.Agent) (cut bool, err error) {
	if err := s.paneReadWriter.CutLine(agent.Pane); err != nil {
		return false, fmt.Errorf("failed to cut the draft of agent %q: %w", agent.ID, err)
	}

	// The line is out of the box from here on, so every way out that does not hand it to the
	// caller puts it back.
	defer func() {
		if !cut {
			s.restoreDraft(ctx, agent)
		}
	}()

	empty, err := s.waitForAnEmptyBox(agent)
	if err != nil {
		return false, err
	}
	if !empty {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "cutting an agent's input box left text behind, so its mail was not delivered",
			slog.String(logging.TargetAgentID, agent.ID))

		return false, nil
	}

	return true, nil
}

// waitForAnEmptyBox reads the frame until it shows the cut, which tmux writes to the pane ahead
// of the CLI repainting it.
func (s *DeliveryService) waitForAnEmptyBox(agent agentstore.Agent) (bool, error) {
	for read := range cutSettleReads {
		if read > 0 {
			time.Sleep(cutSettleInterval)
		}

		// Codex draws a placeholder in an empty composer, so the box is read through the harness
		// rather than off the capture.
		input, err := s.readInput(agent)
		if err != nil {
			return false, fmt.Errorf("failed to read the input box of agent %q: %w", agent.ID, err)
		}

		if input.Activity == harness.ActivityIdle {
			return true, nil
		}
	}

	return false, nil
}

// didLastNudgeGoNowhere reports whether the agent was told about mail before and has ended no
// turn since, which is the only reading under which the keys are known to have been swallowed.
func didLastNudgeGoNowhere(agent agentstore.Agent) bool {
	if agent.NudgedAt == nil {
		return false
	}

	return agent.TurnEndedAt == nil || !agent.TurnEndedAt.After(*agent.NudgedAt)
}

func (s *DeliveryService) reportExpired(ctx context.Context, agent agentstore.Agent) {
	expired, err := s.mailboxReader.ListExpired(ctx, agent.ID)
	if err != nil || len(expired) == 0 {
		return
	}

	for _, item := range expired {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "a piece of mail expired before it was read",
			slog.String(logging.TargetAgentID, agent.ID),
			slog.String(logging.MailID, item.ID))
	}
}
