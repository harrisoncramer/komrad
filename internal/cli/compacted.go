package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"komrad/internal/caller"
	"komrad/internal/harness/claude"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const promptLimit = 2000

const compactedPreamble = "Your conversation was just compacted, so the summary above may have dropped " +
	"details komrad keeps for you. This is your record as komrad holds it now."

func compactedHookCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compacted",
		Short: "Remind an agent of its own record once its conversation has compacted",
		Long: `Remind an agent of its own record once its conversation has compacted.

Claude runs this on its SessionStart event when a conversation has just been
summarized. It reads the agent's record from komrad, its branch, pull request,
recorded metadata, open review threads, waiting mail and the task it was
launched with, and prints it back as additional context. A panel that cannot be
reached prints nothing.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if _, err := io.Copy(io.Discard, cmd.InOrStdin()); err != nil {
			reportHookProblem(cmd, "komrad read no hook payload", err)
		}
		if state.env.AgentID == "" {
			return nil
		}

		row, err := readOwnRow(cmd.Context(), state)
		if err != nil {
			reportHookProblem(cmd, "komrad could not be reached, so the agent was not reminded of its record", err)

			return nil
		}

		threads, err := readOpenThreads(cmd.Context(), state, threadOwner(row.GetAgent()))
		if err != nil {
			reportHookProblem(cmd, "komrad could not read the review threads, so they are left out", err)
		}

		return json.NewEncoder(cmd.OutOrStdout()).Encode(hookOutput{
			HookSpecificOutput: hookContext{
				HookEventName:     string(claude.EventSessionStart),
				AdditionalContext: renderCompactedRecord(row, threads),
			},
		})
	}

	return cmd
}

func threadOwner(agent *agentv1.Agent) string {
	switch agent.GetRole() {
	case agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER:
		return agent.GetId()
	case agentv1.AgentRole_AGENT_ROLE_REVIEWER:
		return agent.GetParentAgentId()
	case agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR, agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func readOwnRow(ctx context.Context, state *app) (*agentv1.AgentRow, error) {
	var resp agentv1.GetAgentResponse
	if err := callHookRPC(ctx, state, hookRPC{
		path:    agentv1connect.AgentServiceGetAgentProcedure,
		request: map[string]any{"id": state.env.AgentID},
		into:    &resp,
	}); err != nil {
		return nil, err
	}

	return resp.GetAgent(), nil
}

func readOpenThreads(ctx context.Context, state *app, owner string) ([]*agentv1.ReviewThread, error) {
	if owner == "" {
		return nil, nil
	}

	var resp agentv1.ListReviewThreadsResponse
	if err := callHookRPC(ctx, state, hookRPC{
		path:    agentv1connect.AgentServiceListReviewThreadsProcedure,
		request: map[string]any{"agentId": owner},
		into:    &resp,
	}); err != nil {
		return nil, err
	}

	var open []*agentv1.ReviewThread
	for _, thread := range resp.GetThreads() {
		if thread.GetResolvedAt() == "" {
			open = append(open, thread)
		}
	}

	return open, nil
}

type hookRPC struct {
	path    string
	request map[string]any
	into    proto.Message
}

func callHookRPC(ctx context.Context, state *app, call hookRPC) error {
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	payload, err := callRPC(ctx, callRPCParams{
		Addr:    state.env.Addr,
		Path:    call.path,
		From:    caller.Caller{AgentID: state.env.AgentID, SessionID: state.env.SessionID},
		Request: call.request,
	})
	if err != nil {
		return err
	}

	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, call.into); err != nil {
		return fmt.Errorf("failed to parse the response from %s: %w", call.path, err)
	}

	return nil
}

func renderCompactedRecord(row *agentv1.AgentRow, threads []*agentv1.ReviewThread) string {
	agent := row.GetAgent()

	lines := []string{
		compactedPreamble,
		"",
		fmt.Sprintf("You are %s (%s), the %s agent.", agent.GetName(), agent.GetId(), roleName(agent.GetRole())),
	}

	if agent.GetBranch() != "" {
		lines = append(lines, "Branch: "+agent.GetBranch())
	}
	if prs := row.GetPullRequests(); len(prs) > 0 {
		lines = append(lines, fmt.Sprintf("Pull request: %s (%s)", prs[0].GetUrl(), enumWords(prs[0].GetState().String(), "PULL_REQUEST_STATE_")))
	}
	if agent.GetParentAgentId() != "" {
		lines = append(lines, "Parent agent: "+agent.GetParentAgentId())
	}
	lines = append(lines, xslice.Map(agent.GetMetadata(), func(meta *agentv1.AgentMetadata) string {
		return fmt.Sprintf("Recorded %s: %s", meta.GetKey(), meta.GetValue())
	})...)

	if len(threads) > 0 {
		lines = append(lines, threadsHeading(agent.GetRole(), len(threads)))
		lines = append(lines, xslice.Map(threads, func(thread *agentv1.ReviewThread) string {
			return fmt.Sprintf("- %s on %s lines %d-%d", thread.GetId(), thread.GetFilePath(), thread.GetNewStart(), thread.GetNewEnd())
		})...)
	}

	if row.GetUnreadMail() {
		lines = append(lines, "Mail is waiting for you. Call read_mail.")
	}

	if prompt := agent.GetPrompt(); prompt != "" {
		lines = append(lines, "", "The task you were launched with:", cutPrompt(prompt))
	}

	return strings.Join(lines, "\n")
}

func threadsHeading(role agentv1.AgentRole, count int) string {
	if role == agentv1.AgentRole_AGENT_ROLE_REVIEWER {
		return fmt.Sprintf("Your open threads on the work you review (%d). Re-read them with list_review_threads, "+
			"take a fresh round, and resolve each one that is settled or release another review:", count)
	}

	return fmt.Sprintf("Open review threads on your work (%d), answered with add_review_message:", count)
}

func roleName(role agentv1.AgentRole) string {
	return enumWords(role.String(), "AGENT_ROLE_")
}

func enumWords(name, prefix string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(name, prefix), "_", " "))
}

func cutPrompt(prompt string) string {
	runes := []rune(prompt)
	if len(runes) <= promptLimit {
		return prompt
	}

	return string(runes[:promptLimit]) + "\n(cut here; get_agent returns the whole task)"
}
