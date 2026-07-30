package agents

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	agentstore "claude-agents/internal/store/agent"
)

// workingLine matches Claude Code's live status line, shown only while it is busy
// generating or running a tool. The shape is a spinner glyph, a space, a verb, and an
// ellipsis, like "✽ Beboppin'…" or "· Cooking… (1m 50s · ↓ 5.4k tokens)". The
// trailing timer appears only once tokens start streaming, so keying on it would leave
// every no-timer frame undetected. The glyph is never a letter, so requiring a
// non-letter lead excludes prose that merely ends a word with an ellipsis, and
// excluding the tool-result "⎿", the "❯" prompt, and box rules keeps that chrome from
// matching. workingTimer catches the parenthesised elapsed timer on its own.
var (
	workingLine  = regexp.MustCompile(`(?m)^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+\p{L}.*…`)
	workingTimer = regexp.MustCompile(`…\s*\(\s*\d+m?\s*\d*\s*s`)
)

// legacyWorkingMarker is the wording older builds put on that same line instead
// of a token count. Accept it too so the detector spans releases.
const legacyWorkingMarker = "esc to interrupt"

// reconcileMisses is how many consecutive idle observations a thinking agent must
// show before it settles to done. Requiring several guards against a capture landing
// on a redraw frame between tool calls, where the line is momentarily absent even
// though work is ongoing. Erring high matters because wrongly demoting a live agent is
// far worse than a stale state lingering a few seconds.
const reconcileMisses = 3

// liveRegionLines is how many lines from the bottom of a captured frame count as the
// live UI. It has to be wide enough to hold the working status line even when a plan or
// todo list renders under it, pushing it well up from the bottom.
const liveRegionLines = 30

// waitingRegionLines is a much tighter window for the waiting check, since the
// selection footer replaces the input box and is the bottom-most line while a prompt is
// up. Keeping it tight matters because the footer's phrases can appear in ordinary
// transcript text, which the wide working window would read as a live prompt.
const waitingRegionLines = 6

// ansiSeq matches an ANSI escape sequence. The pane is captured with colors via -e, so
// the status line comes back shredded by SGR codes, the verb split across color runs
// and codes wedged between the ellipsis and the timer.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// tailLines returns the last n lines of a frame with ANSI escapes stripped, the region
// where the live UI sits. Transcript prose scrolls above it and can contain any marker
// text, so detection stays inside this window.
func tailLines(frame string, n int) string {
	frame = ansiSeq.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

// frameShowsWork reports whether the frame's live region shows Claude actively
// working, as opposed to sitting idle at its input prompt.
func frameShowsWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)
	return workingLine.MatchString(region) || workingTimer.MatchString(region) || strings.Contains(region, legacyWorkingMarker)
}

// waitingFooter matches the footer chrome Claude renders under any interactive
// selection it is blocked on, which reads "Enter to select · ↑/↓ to navigate · Esc to
// cancel". Those phrases are wording-independent, distinct from the working line's "esc
// to interrupt", and absent from the plain input box a finished agent sits at. Keying on
// the footer rather than the prompt's own prose keeps transcript text from being
// mistaken for a live prompt.
var waitingFooter = regexp.MustCompile(`(?i)esc to cancel|enter to select`)

// frameShowsWaiting reports whether the frame's live region shows Claude blocked on the
// user at an interactive prompt.
func frameShowsWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

// tmuxProbe is the part of the tmux client the reconciler observes through.
// Observation keys on the pane's owner stamp rather than the pane's mere existence, so
// a pane id the tmux server has reissued to somebody else is not read as this agent
// still running.
type tmuxProbe interface {
	PaneOwner(pane string) string
	CapturePane(pane string) string
}

type stateStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
	ClearPane(ctx context.Context, id string) error
}

// Reconciler is the single source of truth for agent state. Rather than react to Claude
// Code lifecycle events, which fire unreliably and miss endings like an escape
// interrupt, it observes each agent directly from tmux on a timer and writes the state
// that observation implies. Because it reads ground truth every pass, a wrong state
// cannot outlive the interval and there are no edges to miss.
type Reconciler struct {
	tmux   tmuxProbe
	store  stateStore
	misses map[string]int // agent id to consecutive idle observations
}

// NewReconciler builds a Reconciler over a tmux probe and the state store.
func NewReconciler(t tmuxProbe, s stateStore) *Reconciler {
	return &Reconciler{tmux: t, store: s, misses: map[string]int{}}
}

// Reconcile observes every tracked agent once and corrects any stored state that
// reality has moved past. An agent already matching reality is left untouched, so
// running it repeatedly is safe and cheap.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	agents, err := r.store.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for reconcile: %w", err)
	}

	listed := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		listed[agent.ID] = struct{}{}
		r.reconcileAgent(ctx, agent)
	}

	// Miss counts for agents that no longer list are forgotten, so the map cannot grow
	// without bound as agents come and go.
	for id := range r.misses {
		if _, ok := listed[id]; !ok {
			delete(r.misses, id)
		}
	}

	return nil
}

// reconcileAgent writes the single agent's state to match what tmux shows.
func (r *Reconciler) reconcileAgent(ctx context.Context, agent agentstore.Agent) {
	// A pane that is not this agent's means the agent is dormant and cannot be working,
	// whether its pane died or the tmux server has since given that id to somebody else.
	if !paneOwnedBy(r.tmux.PaneOwner(agent.Pane), agent.ID) {
		// A pane id kept on a dormant row is the one that collides after a server restart,
		// so forgetting it here is what stops a later pass, a stage preview, or a kill from
		// landing on another agent's pane.
		if agent.Pane != "" {
			_ = r.store.ClearPane(ctx, agent.ID)
		}
		if agent.State == agentstore.StateThinking || agent.State == agentstore.StateWaiting {
			r.apply(ctx, agent, agentstore.StateDone)
		} else {
			delete(r.misses, agent.ID)
		}
		return
	}

	frame := r.tmux.CapturePane(agent.Pane)

	// An empty capture means the pane could not be read, not that Claude went idle. A
	// live agent always shows at least its input box, so this is inconclusive and every
	// uncertain case is a no-op.
	if frame == "" {
		return
	}

	switch {
	case frameShowsWork(frame):
		r.apply(ctx, agent, agentstore.StateThinking)
	case frameShowsWaiting(frame):
		r.apply(ctx, agent, agentstore.StateWaiting)
	case agent.State == agentstore.StateThinking || agent.State == agentstore.StateWaiting:
		// Idle, but was active. Demoting waits for enough consecutive idle observations to
		// rule out a transient redraw between tool calls.
		r.misses[agent.ID]++
		if r.misses[agent.ID] >= reconcileMisses {
			r.apply(ctx, agent, agentstore.StateDone)
		}
	default:
		delete(r.misses, agent.ID)
	}
}

// apply writes a state and clears the agent's miss count, doing nothing when the stored
// state already matches so an unchanged agent never touches the store.
func (r *Reconciler) apply(ctx context.Context, agent agentstore.Agent, state string) {
	delete(r.misses, agent.ID)
	if agent.State == state {
		return
	}

	_ = r.store.SetStateByID(ctx, agent.ID, state)
}
