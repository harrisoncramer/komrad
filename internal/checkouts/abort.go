package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
)

// ErrAbortNothing says nothing is part-way through the checkout.
var ErrAbortNothing = fail.Precondition("nothing is part-way through in this worktree, so there is nothing to abort")

// AbortResult is what an abort did.
type AbortResult struct {
	Sequence git.Sequence `json:"sequence"`
	// Branch is where the checkout stands once the sequence is undone, empty when detached.
	Branch string `json:"branch"`
	// ParkedRef holds the checkout as it stood mid-sequence, conflicts and all.
	ParkedRef string `json:"parked_ref"`
	// Ours says the sequence was komrad's own rebase, stopped on conflicts.
	Ours bool `json:"ours"`
	// Interrupted says a turn was cut so the abort could run.
	Interrupted bool `json:"interrupted"`
}

// Abort undoes whatever rebase, am, merge or pick is part-way through the agent's worktree,
// parking the checkout first so nothing the agent had resolved is lost.
func (s *Service) Abort(ctx context.Context, agentID string) (*AbortResult, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	unlock, err := s.lockTree(agent.Cwd)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if _, err := s.readSequence(ctx, *agent); err != nil {
		return nil, err
	}

	interrupted, err := s.settle(ctx, *agent)
	if err != nil {
		return nil, err
	}

	result, err := s.abort(ctx, *agent)
	if err != nil {
		if interrupted {
			s.queueEvent(ctx, agent.ID, Aborted{AgentID: agent.ID, Failure: err.Error()})
		}

		return nil, err
	}
	result.Interrupted = interrupted

	s.queueEvent(ctx, agent.ID, Aborted{AgentID: agent.ID, Result: result})

	return result, nil
}

// abort reads the sequence again, parks the checkout and undoes it.
func (s *Service) abort(ctx context.Context, agent agentstore.Agent) (*AbortResult, error) {
	if _, err := s.read(ctx, agent.ID); err != nil {
		return nil, err
	}

	sequence, err := s.readSequence(ctx, agent)
	if err != nil {
		return nil, err
	}

	ours, err := s.isOwnRebase(ctx, agent, sequence)
	if err != nil {
		return nil, err
	}

	result := &AbortResult{
		Sequence:  sequence,
		ParkedRef: buildParkRef(parkAbort, agent.ID),
		Ours:      ours,
	}
	if _, err := s.aborter.Park(ctx, agent.Cwd, result.ParkedRef); err != nil {
		return nil, err
	}
	defer s.checkoutSync.Wake()

	err = s.aborter.AbortSequence(ctx, agent.Cwd, sequence)
	if errors.Is(err, git.ErrIndexLocked) {
		return nil, fmt.Errorf("%w: %w", ErrBusy, err)
	}
	if err != nil {
		return nil, err
	}

	s.recordAborted(ctx, agent)

	state, err := s.aborter.ReadTreeState(ctx, agent.Cwd)
	if err != nil {
		return nil, err
	}
	result.Branch = state.Branch

	return result, nil
}

// readSequence is what is part-way through the checkout, refusing one with nothing.
func (s *Service) readSequence(ctx context.Context, agent agentstore.Agent) (git.Sequence, error) {
	sequence, err := s.aborter.ReadSequence(ctx, agent.Cwd)
	if err != nil {
		return git.SequenceNone, err
	}
	if !sequence.IsInProgress() {
		return git.SequenceNone, ErrAbortNothing
	}

	return sequence, nil
}

// isOwnRebase reports whether the sequence is the rebase komrad started and left stopped on
// conflicts, which is the one the agent was told to wait on.
func (s *Service) isOwnRebase(ctx context.Context, agent agentstore.Agent, sequence git.Sequence) (bool, error) {
	if sequence != git.SequenceRebase {
		return false, nil
	}

	raw, err := s.agents.GetLastRebaseByID(ctx, agent.ID)
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}

	var outcome rebaseOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return false, fmt.Errorf("failed to read the last rebase of agent %q: %w", agent.Name, err)
	}
	if outcome.Result == nil || !outcome.Result.Conflicted {
		return false, nil
	}

	onto, err := s.aborter.ReadRebaseOnto(ctx, agent.Cwd)
	if err != nil {
		return false, err
	}

	return onto != "" && onto == outcome.Result.BaseSHA, nil
}

// recordAborted clears the sequence on the agent's row ahead of the checkout sync.
func (s *Service) recordAborted(ctx context.Context, agent agentstore.Agent) {
	if err := s.agents.SetSequenceByID(ctx, agent.ID, agentstore.SequenceNone); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record that an agent's sequence was aborted",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// abortedMail is what an agent is told once the panel aborted what its worktree had part-way
// through, which turns on whether komrad or the agent started it.
func abortedMail(aborted Aborted) string {
	if aborted.Result == nil {
		return fmt.Sprintf("Your turn was interrupted so that what was part-way through your worktree could be aborted, "+
			"but the abort failed: %s\n\n"+
			"Check git status before carrying on, then pick up where you were.", aborted.Failure)
	}

	result := *aborted.Result
	var body strings.Builder

	if result.Interrupted {
		fmt.Fprintf(&body, "Your turn was interrupted so that the %s in your worktree could be aborted.\n\n", sequenceName(result.Sequence))
	}

	if result.Ours {
		fmt.Fprintf(&body, "The rebase of your worktree that stopped on conflicts was aborted from the panel. "+
			"The person chose not to carry it forward. %s is back where it stood before the rebase began, with all your commits.\n\n"+
			"Do not start the rebase again.", standing(result))
	} else {
		fmt.Fprintf(&body, "The %s you had part-way through your worktree was aborted from the panel. "+
			"%s is back where it stood before it began.\n\n"+
			"Do not start it again without asking.", sequenceName(result.Sequence), standing(result))
	}

	body.WriteString(" Read your files again rather than trusting what you remember, then carry on from here.")
	body.WriteString("\n\nA copy of the worktree as it stood before the abort, conflicts and any resolution you had made included, is kept at " +
		result.ParkedRef + ".")

	return body.String()
}

// standing names what was put back.
func standing(result AbortResult) string {
	if result.Branch == "" {
		return "Your checkout"
	}

	return "Your branch " + result.Branch
}

// sequenceName is a sequence as git's own command names it.
func sequenceName(sequence git.Sequence) string {
	if sequence == git.SequenceCherryPick {
		return "cherry-pick"
	}

	return string(sequence)
}
