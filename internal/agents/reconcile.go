package agents

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"claude-agents/internal/store"
)

// workingLine matches Claude Code's live status line, shown only while it is
// busy generating or running a tool. It is a spinner glyph, a space, a verb, and
// an ellipsis: "✽ Beboppin'…", "✢ Composing… (32s · thinking more)", "· Cooking…
// (1m 50s · ↓ 5.4k tokens)". The trailing timer only appears once tokens start
// streaming, so early in a turn there is none at all; keying on the timer (the
// earlier mistake) missed every no-timer frame and left working agents
// undetected. The stable shape is the leading "<glyph> <phrase>…". The verb can
// be several words ("✢ Building schema, migration, sqlc…"), so anything up to
// the ellipsis is allowed after the first letter. The glyph is a rotating
// spinner symbol, never a letter, so requiring a non-letter lead excludes prose
// that merely ends a word with an ellipsis, and excluding the tool-result "⎿",
// the "❯" prompt, and box rules keeps that chrome from matching. workingTimer
// additionally catches the parenthesised elapsed timer on its own. Detection runs
// only on the bottom live region, so the transcript above cannot trip either.
var (
	workingLine  = regexp.MustCompile(`(?m)^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+\p{L}.*…`)
	workingTimer = regexp.MustCompile(`…\s*\(\s*\d+m?\s*\d*\s*s`)
)

// legacyWorkingMarker is the wording older builds put on that same line instead
// of a token count. Accept it too so the detector spans releases.
const legacyWorkingMarker = "esc to interrupt"

// reconcileMisses is how many consecutive idle observations a thinking agent
// must show before it is settled to done. Requiring several guards against a
// capture landing on a redraw frame between tool calls, where the line can be
// momentarily absent even though work is ongoing. Erring high matters because
// wrongly demoting a live agent is far worse than a stale state lingering a few
// extra seconds.
const reconcileMisses = 3

// liveRegionLines is how many lines from the bottom of a captured frame count as
// the live UI, where Claude's status line, prompt footer, and input box sit. The
// conversation transcript scrolls above it. Detection is confined to this region
// so that prose in the transcript, which can happen to contain any marker text,
// never trips a detector; only the chrome Claude actually renders at the bottom
// does. It must be wide enough to include the working status line even when a
// live tool block sits below it: a plan/todo list renders under the status line
// and above the input box, pushing the status line well up from the bottom, so a
// tight window would miss a genuinely-working agent.
const liveRegionLines = 30

// waitingRegionLines is a much tighter window for the waiting check. The
// selection footer replaces the input box and is the bottom-most line while a
// prompt is up, so it needs only the last few lines. Keeping it tight matters:
// the footer's "esc to cancel" / "enter to select" phrases can appear in
// ordinary transcript text, and the wide working window would otherwise read
// that prose as a live prompt.
const waitingRegionLines = 6

// ansiSeq matches an ANSI escape sequence. The pane is captured with colors (-e),
// so the status line comes back shredded by SGR codes — the verb split across
// color runs, codes wedged between the ellipsis and the timer — which breaks any
// text match. Detection strips these first so it sees the plain glyphs and words.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// tailLines returns the last n lines of a frame with ANSI escapes stripped, the
// region where the live UI sits. The conversation transcript scrolls above it,
// so confining detection here keeps prose in the transcript, which can contain
// any marker text, from tripping a detector.
func tailLines(frame string, n int) string {
	frame = ansiSeq.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

// frameShowsWork reports whether the frame's live region shows Claude actively
// working, as opposed to sitting idle at its input prompt. It uses the wide
// window because a live tool block can push the status line well up from the
// bottom.
func frameShowsWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)
	return workingLine.MatchString(region) || workingTimer.MatchString(region) || strings.Contains(region, legacyWorkingMarker)
}

// waitingFooter matches the footer chrome Claude renders under any interactive
// selection it is blocked on: a permission dialog, a multiple-choice question, a
// menu. That footer reads "Enter to select · ↑/↓ to navigate · Esc to cancel",
// and "esc to cancel" is a reliable, wording-independent signal, distinct from
// the working line's "esc to interrupt" and absent from the plain input box a
// finished agent sits at. Keying on this footer rather than the prompt's prose
// (a question like "do you want …") is what keeps transcript text from being
// mistaken for a live prompt.
var waitingFooter = regexp.MustCompile(`(?i)esc to cancel|enter to select`)

// frameShowsWaiting reports whether the frame's live region shows Claude blocked
// on the user at an interactive prompt. It uses the tight window because the
// footer is pinned at the very bottom, and a wide window would let transcript
// prose containing the same phrases read as a live prompt.
func frameShowsWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

// tmuxProbe is the slice of the tmux client the reconciler observes through.
// Liveness keys on the pane id, not a session name, so an agent stays observable
// no matter which window or session its pane currently sits in.
type tmuxProbe interface {
	PaneAlive(pane string) bool
	CapturePane(pane string) string
}

// stateStore is the slice of the store the reconciler reads and corrects. Writes
// are keyed by id, not name, so a state decided from a List snapshot always
// lands on the exact row observed even if its name was reused meanwhile.
type stateStore interface {
	List(ctx context.Context) ([]store.Agent, error)
	SetStateByID(ctx context.Context, id, state string) error
}

// Reconciler is the single source of truth for agent state. Rather than react to
// Claude Code lifecycle events, which fire unreliably and miss endings like an
// escape interrupt, it observes each agent directly from tmux on a timer and
// writes the state that observation implies: a working timer on the pane means
// thinking, a confirmation prompt means waiting, an idle prompt means done, and
// a vanished pane means done. Because it reads ground truth every pass,
// a wrong state cannot persist longer than the interval, and there are no edges
// to miss.
type Reconciler struct {
	tmux   tmuxProbe
	store  stateStore
	misses map[string]int // agent id -> consecutive idle observations
}

// NewReconciler builds a Reconciler over a tmux probe and the state store.
func NewReconciler(t tmuxProbe, s stateStore) *Reconciler {
	return &Reconciler{tmux: t, store: s, misses: map[string]int{}}
}

// Reconcile observes every tracked agent once and corrects any stored state that
// reality has moved past. It is idempotent: an agent already matching reality is
// left untouched, so running it repeatedly is safe and cheap.
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

	// Forget miss counts for agents that no longer list, so the map cannot grow
	// without bound as chats come and go.
	for id := range r.misses {
		if _, ok := listed[id]; !ok {
			delete(r.misses, id)
		}
	}

	return nil
}

// reconcileAgent writes the single agent's state to match what tmux shows.
func (r *Reconciler) reconcileAgent(ctx context.Context, agent store.Agent) {
	// The pane is gone: the agent is dormant and cannot be working. An active
	// state settles to done so the counts stop treating it as running; a new agent
	// that never launched, or one already done, is left as is.
	if !r.tmux.PaneAlive(agent.Pane) {
		if agent.State == store.StateThinking || agent.State == store.StateWaiting {
			r.apply(ctx, agent, store.StateDone)
		} else {
			delete(r.misses, agent.ID)
		}
		return
	}

	var frame string
	if agent.Pane != "" {
		frame = r.tmux.CapturePane(agent.Pane)
	}

	// An empty capture means the pane could not be read, not that Claude went
	// idle: a live agent always shows at least its input box. Treat it as
	// inconclusive and leave the state alone, so a flaky read never moves an
	// agent. Wrongly demoting a live agent is the worst outcome, so every
	// uncertain case is a no-op.
	if frame == "" {
		return
	}

	switch {
	case frameShowsWork(frame):
		// Promotion is immediate: the moment work appears, the agent is thinking.
		r.apply(ctx, agent, store.StateThinking)
	case frameShowsWaiting(frame):
		r.apply(ctx, agent, store.StateWaiting)
	case agent.State == store.StateThinking || agent.State == store.StateWaiting:
		// Idle, but was active. Demote to done only after enough consecutive idle
		// observations to rule out a transient redraw between tool calls.
		r.misses[agent.ID]++
		if r.misses[agent.ID] >= reconcileMisses {
			r.apply(ctx, agent, store.StateDone)
		}
	default:
		// Idle and new or already done: nothing to change.
		delete(r.misses, agent.ID)
	}
}

// apply writes a state and clears the agent's miss count. A no-op when the
// stored state already matches, so an unchanged agent never touches the store.
func (r *Reconciler) apply(ctx context.Context, agent store.Agent, state string) {
	delete(r.misses, agent.ID)
	if agent.State == state {
		return
	}

	_ = r.store.SetStateByID(ctx, agent.ID, state)
}
