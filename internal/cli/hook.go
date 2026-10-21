package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/internal/harness/claude"
	"komrad/internal/harness/codex"
	"komrad/internal/rpcapi"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/spf13/cobra"
)

const hookTimeout = 5 * time.Second

const logTool = "log"

type hookOutput struct {
	HookSpecificOutput hookContext `json:"hookSpecificOutput"`
}

type hookContext struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

func hookCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "hook",
		Annotations: map[string]string{survivesBadConfig: ""},
		Short:       "Run one of the agent CLI hooks komrad installs",
		Long: `Run one of the agent CLI hooks komrad installs.

An agent CLI runs these itself at a point in its own loop. They read the hook
payload on stdin and write whatever that CLI expects on stdout. Nothing here is
meant to be run by hand.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(stopHookCmd(state))
	cmd.AddCommand(turnEndHookCmd(state))
	cmd.AddCommand(compactedHookCmd(state))
	cmd.AddCommand(preToolUseHookCmd(state))
	cmd.AddCommand(payloadHookCmd(state, payloadHook{
		use:   "post-tool-use",
		short: "Record that an agent's tool call returned",
		long: `Record that an agent's tool call returned.

Claude runs this once a tool call has finished, which takes the tool back off
the agent's row.`,
		event: claude.EventPostToolUse,
	}))
	cmd.AddCommand(payloadHookCmd(state, payloadHook{
		use:   "post-tool-use-failure",
		short: "Record that an agent's tool call failed",
		long: `Record that an agent's tool call failed.

Claude runs this in place of its PostToolUse hook when a tool call fails, which
takes the tool back off the agent's row.`,
		event: claude.EventPostToolUseFailure,
	}))
	cmd.AddCommand(payloadHookCmd(state, payloadHook{
		use:   "permission-request",
		short: "Record the tool an agent is waiting to be allowed to run",
		long: `Record the tool an agent is waiting to be allowed to run.

Claude runs this when a tool call puts a dialog up for a person to answer.
Komrad keeps the name of the tool against the agent, which is what tells a row
blocked on an approval from one blocked on a question, and reads nothing else:
a tool's input can carry a secret.`,
		event: claude.EventPermissionRequest,
	}))
	cmd.AddCommand(payloadHookCmd(state, payloadHook{
		use:   "stop-failure",
		short: "Record that an API error ended an agent's turn",
		long: `Record that an API error ended an agent's turn.

Claude runs this in place of its Stop hook when a rate limit, an outage or any
other API error ends the turn. Komrad keeps the error against the agent so the
panel can say the turn failed rather than finished.`,
		event: claude.EventStopFailure,
	}))

	return cmd
}

// payloadHook is one event whose whole answer is what the harness reads out of its payload.
type payloadHook struct {
	use   string
	short string
	long  string
	event harness.Event
}

func payloadHookCmd(state *app, hook payloadHook) *cobra.Command {
	cmd := &cobra.Command{
		Use:           hook.use,
		Short:         hook.short,
		Long:          hook.long,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			reportHookProblem(cmd, state, "komrad read no hook payload, so nothing was recorded", err)

			return nil
		}
		if state.env.AgentID == "" {
			return nil
		}

		reportHookEvent(cmd, state, reportHookEventParams{
			harnessID: claude.HarnessID,
			event:     hook.event,
			payload:   body,
		})

		return nil
	}

	return cmd
}

func preToolUseHookCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pre-tool-use",
		Short: "Record the tool an agent is about to run, and refuse a rebase on a stacked task",
		Long: `Record the tool an agent is about to run, and refuse a rebase on a stacked task.

Claude runs this before every tool call, handing it the call on stdin. Komrad
keeps the name of the tool against the agent so the panel can say what it is
doing, and sends nothing else: a tool's input can carry a secret.

A shell call that starts a git rebase, or a git pull that rebases, is refused
on a task stacked on another, since komrad rebases the stack itself. The command
is read here and never leaves the hook. A rebase komrad asked for after its own
one conflicted is let through.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			reportHookProblem(cmd, state, "komrad read no hook payload, so nothing was recorded", err)

			return nil
		}
		if state.env.AgentID == "" {
			return nil
		}

		if refusesRebase(cmd, state, body) {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(rebaseDenial)
		}

		reportHookEvent(cmd, state, reportHookEventParams{
			harnessID: claude.HarnessID,
			event:     claude.EventPreToolUse,
			payload:   body,
		})

		return nil
	}

	return cmd
}

func turnEndHookCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "turn-end",
		Short: "Record that an agent's turn ended",
		Long: `Record that an agent's turn ended.

Codex runs this as its notify program, handing it one JSON argument. Komrad keeps
the moment against the agent so the panel can say it has finished something
nobody has read. Anything but a completed turn is ignored.`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if state.env.AgentID == "" {
			return nil
		}
		var payload []byte
		if len(args) > 0 {
			payload = []byte(args[0])
		}

		reportHookEvent(cmd, state, reportHookEventParams{
			harnessID: codex.HarnessID,
			event:     codex.EventNotify,
			payload:   payload,
		})

		return nil
	}

	return cmd
}

func stopHookCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Record that an agent's turn ended and tell it about waiting mail",
		Long: `Record that an agent's turn ended and tell it about waiting mail.

Claude runs this on its Stop event. Komrad keeps the moment against the agent so
the panel can say it has finished something nobody has read. When mail is waiting,
it prints the same one-line notice komrad would type into the pane, so the agent
reads its mail without waiting for the next pass.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		body, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			reportHookProblem(cmd, state, "komrad read no hook payload, so nothing was recorded", err)

			return nil
		}
		if state.env.AgentID == "" {
			return nil
		}

		reportHookEvent(cmd, state, reportHookEventParams{
			harnessID: claude.HarnessID,
			event:     claude.EventStop,
			payload:   body,
		})

		var claimed agentv1.ClaimMailNudgeResponse
		if err := callHookRPC(cmd.Context(), state, hookRPC{
			path:    agentv1connect.AgentServiceClaimMailNudgeProcedure,
			request: map[string]any{},
			into:    &claimed,
		}); err != nil {
			reportHookProblem(cmd, state, "komrad could not be reached, so any mail waits for the next pass", err)

			return nil
		}
		if claimed.GetNudge() == "" {
			return nil
		}

		return json.NewEncoder(cmd.OutOrStdout()).Encode(hookOutput{
			HookSpecificOutput: hookContext{
				HookEventName:     string(claude.EventStop),
				AdditionalContext: claimed.GetNudge(),
			},
		})
	}

	return cmd
}

// reportHookProblem says on stderr what a hook could not do, and logs it under the agent when komrad can be reached.
func reportHookProblem(cmd *cobra.Command, state *app, what string, err error) {
	line := fmt.Sprintf("%s: %v", what, err)
	cmd.PrintErrln(line)

	if state.env.AgentID == "" || isUnreachable(err) {
		return
	}

	if err := logToKomrad(cmd.Context(), state, line); err != nil {
		cmd.PrintErrf("komrad did not log the problem: %v\n", err)
	}
}

// isUnreachable reports whether a call failed before komrad answered it.
func isUnreachable(err error) bool {
	var urlErr *url.Error

	return errors.As(err, &urlErr)
}

type reportHookEventParams struct {
	harnessID string
	event     harness.Event
	payload   []byte
}

// reportHookEvent hands one hook payload to the harness that fired it and records what it read.
func reportHookEvent(cmd *cobra.Command, state *app, params reportHookEventParams) {
	hn, err := state.harnesses.GetByID(params.harnessID)
	if err != nil {
		reportHookProblem(cmd, state, "komrad has no harness to read the hook payload with, so nothing was recorded", err)

		return
	}

	report, err := hn.ReadReport(params.event, params.payload)
	if errors.Is(err, harness.ErrNoReport) {
		return
	}
	if err != nil {
		reportHookProblem(cmd, state, "komrad could not read the hook payload, so nothing was recorded", err)

		return
	}

	if err := recordReport(cmd.Context(), state, params.event, report); err != nil {
		reportHookProblem(cmd, state, "komrad did not record the hook, so the agent's row may be out of date", err)
	}
}

func recordReport(ctx context.Context, state *app, hook harness.Event, report harness.Report) error {
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	_, err := callRPC(ctx, callRPCParams{
		Base: formatBase(state.env.Addr),
		Path: agentv1connect.AgentServiceRecordReportProcedure,
		From: caller.Caller{AgentID: state.env.AgentID, SessionID: state.env.SessionID},
		Request: map[string]any{
			"event":     reportEvents[report.Event].String(),
			"hook":      string(hook),
			"tool":      clampPrintable(report.Tool, reportLimit),
			"turnError": clampPrintable(report.TurnError, reportLimit),
		},
	})

	return err
}

var reportEvents = map[harness.ReportEvent]agentv1.ReportEvent{
	harness.ReportTurnEnded:      agentv1.ReportEvent_REPORT_EVENT_TURN_ENDED,
	harness.ReportTurnFailed:     agentv1.ReportEvent_REPORT_EVENT_TURN_FAILED,
	harness.ReportToolStarted:    agentv1.ReportEvent_REPORT_EVENT_TOOL_STARTED,
	harness.ReportToolFinished:   agentv1.ReportEvent_REPORT_EVENT_TOOL_FINISHED,
	harness.ReportApprovalWanted: agentv1.ReportEvent_REPORT_EVENT_APPROVAL_WANTED,
	harness.ReportCompacted:      agentv1.ReportEvent_REPORT_EVENT_COMPACTED,
}

// reportLimit is the longest tool or turn error RecordReportRequest takes.
const reportLimit = 200

const logLimit = 2048

// clampPrintable drops the control characters an endpoint refuses and cuts what is left to the
// length it takes, on a rune boundary since half a rune is refused too.
func clampPrintable(text string, limit int) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}

		return r
	}, text)

	runes := []rune(cleaned)
	if len(runes) <= limit {
		return cleaned
	}

	return string(runes[:limit])
}

func logToKomrad(ctx context.Context, state *app, message string) error {
	path, err := toolPath(logTool)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	_, err = callRPC(ctx, callRPCParams{
		Base:    formatBase(state.env.Addr),
		Path:    path,
		From:    caller.Caller{AgentID: state.env.AgentID, SessionID: state.env.SessionID},
		Request: map[string]any{"message": clampPrintable(message, logLimit)},
	})

	return err
}

func toolPath(tool string) (string, error) {
	for exposed := range rpcapi.GetMethods() {
		if exposed.Surfaces.GetName() == tool {
			return exposed.GetPath(), nil
		}
	}

	return "", fmt.Errorf("no rpc is exposed as %q", tool)
}
