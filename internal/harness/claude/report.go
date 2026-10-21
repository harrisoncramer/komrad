package claude

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/harness"
)

// The events Claude fires that komrad takes a reading from, named as Claude names them.
const (
	// EventStop is fired as a turn of Claude's own ends.
	EventStop harness.Event = "Stop"

	// EventStopFailure is fired in place of Stop when an API error ends the turn.
	EventStopFailure harness.Event = "StopFailure"

	// EventPreToolUse is fired before a tool call runs, and not again if the call fails.
	EventPreToolUse harness.Event = "PreToolUse"

	// EventPostToolUse is fired once a tool call has returned.
	EventPostToolUse harness.Event = "PostToolUse"

	// EventPostToolUseFailure is fired in place of PostToolUse when a tool call fails.
	EventPostToolUseFailure harness.Event = "PostToolUseFailure"

	// EventPermissionRequest is fired when a tool call puts a dialog up for a person to answer.
	EventPermissionRequest harness.Event = "PermissionRequest"

	// EventSessionStart is fired as a conversation starts, resumes, clears or has just compacted.
	EventSessionStart harness.Event = "SessionStart"
)

// compactMatcher narrows the SessionStart hook to a conversation that has just compacted.
const compactMatcher = "compact"

const apiErrorPrefix = "API Error: "

// toolPayload is a tool event, of which only the name of the tool is read.
type toolPayload struct {
	ToolName string `json:"tool_name"`
}

type stopFailurePayload struct {
	Error                string `json:"error"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// ReadReport reads out of one of Claude's hook payloads only what komrad records about the agent,
// leaving what the hook has to answer Claude with to the subcommand that answers it.
func (h *Harness) ReadReport(event harness.Event, payload []byte) (harness.Report, error) {
	// The event is the caller's rather than the payload's, the subcommand answering an event
	// knowing which one it is.
	switch event {
	// The event says the turn ended on its own, so nothing in the payload decides it, and a
	// turn komrad's own last answer caused is still a turn ending.
	case EventStop:
		return harness.Report{Event: harness.ReportTurnEnded}, nil
	case EventStopFailure:
		var parsed stopFailurePayload
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return harness.Report{}, fmt.Errorf("failed to parse the %s payload: %w", event, err)
		}

		return harness.Report{Event: harness.ReportTurnFailed, TurnError: parsed.describe()}, nil
	case EventPreToolUse:
		return readToolReport(event, harness.ReportToolStarted, payload)
	case EventPostToolUse, EventPostToolUseFailure:
		return readToolReport(event, harness.ReportToolFinished, payload)

	// The dialog names its tool, which is what tells a row blocked on an approval from one
	// blocked on a question.
	case EventPermissionRequest:
		return readToolReport(event, harness.ReportApprovalWanted, payload)

	// The matcher narrows the event to a conversation that has just compacted, so nothing in the
	// payload decides it.
	case EventSessionStart:
		return harness.Report{Event: harness.ReportCompacted}, nil
	default:
		// Refused rather than read as a report of nothing, which names no event komrad records.
		return harness.Report{}, fmt.Errorf("no reading is taken from the %q event", event)
	}
}

func readToolReport(event harness.Event, reported harness.ReportEvent, payload []byte) (harness.Report, error) {
	var parsed toolPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return harness.Report{}, fmt.Errorf("failed to parse the %s payload: %w", event, err)
	}

	return harness.Report{Event: reported, Tool: parsed.ToolName}, nil
}

func (p stopFailurePayload) describe() string {
	if message := strings.TrimSpace(strings.TrimPrefix(p.LastAssistantMessage, apiErrorPrefix)); message != "" {
		return message
	}

	if p.Error != "" {
		return p.Error
	}

	return "unknown"
}

// Hooks is each event komrad answers, with the matcher an event is narrowed by beside it.
func (h *Harness) Hooks() []string {
	hooks := make([]string, 0, len(InstalledHooks))
	for event, install := range InstalledHooks {
		name := string(event)
		if install.Matcher != "" {
			name += " (" + install.Matcher + ")"
		}
		hooks = append(hooks, name)
	}
	slices.Sort(hooks)

	return hooks
}

// toolBash is the name Claude gives its shell tool.
const toolBash = "Bash"

type shellPayload struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// ReadShellCommand is the command line a tool event's shell call runs, empty for any other tool.
func ReadShellCommand(payload []byte) (string, error) {
	var parsed shellPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse the tool payload: %w", err)
	}
	if parsed.ToolName != toolBash {
		return "", nil
	}

	return parsed.ToolInput.Command, nil
}
