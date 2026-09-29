package ui

import (
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

const (
	conversationPreviewTurns     = 6
	conversationPreviewTurnBytes = 1200
)

const conversationPreviewHeading = "## Conversation Preview"

type conversationPreview struct {
	agentID  string
	answered bool
	turns    []*agentv1.Turn
	err      error
}

type conversationPreviewMsg struct {
	agentID string
	turns   []*agentv1.Turn
	err     error
}

func (m *model) previewedAgentID() (string, bool) {
	s := m.shared()

	switch m.screen {
	case screenArchive:
		if m.archive.kind != archiveAgents {
			return "", false
		}

		agent, ok := m.archive.hoveredAgent(s)

		return agent.ID, ok
	case screenWorktrees:
		row, ok := m.worktrees.hovered(s)
		if !ok || row.item.agent == nil {
			return "", false
		}

		return row.item.agent.id, true
	case screenAgents, screenSchedules, screenSearch, screenReview, screenDocuments, screenLogs:
	}

	return "", false
}

func (m *model) previewConversationCmd() tea.Cmd {
	agentID, ok := m.previewedAgentID()
	if !ok {
		return nil
	}

	m.conversation = conversationPreview{agentID: agentID}
	clients := m.api

	return func() tea.Msg {
		turns, err := clients.getLastTurns(agentID)

		return conversationPreviewMsg{agentID: agentID, turns: turns, err: err}
	}
}

func (m *model) previewMovedConversationCmd() tea.Cmd {
	agentID, ok := m.previewedAgentID()
	if !ok || agentID == m.conversation.agentID {
		return nil
	}

	return m.previewConversationCmd()
}

func (m *model) applyConversationPreview(msg conversationPreviewMsg) {
	if msg.agentID != m.conversation.agentID {
		return
	}

	m.conversation = conversationPreview{
		agentID:  msg.agentID,
		answered: true,
		turns:    msg.turns,
		err:      msg.err,
	}
}

func conversationPreviewMarkdown(preview conversationPreview, agentID string) string {
	var b strings.Builder
	b.WriteString(conversationPreviewHeading + "\n\n")

	switch {
	case preview.agentID != agentID || !preview.answered:
		b.WriteString(searchLoadingMarkdown + "\n")
	case preview.err != nil:
		b.WriteString("_could not read: " + errui.GetReason(preview.err) + "_\n")
	case len(preview.turns) == 0:
		b.WriteString(noTranscriptMarkdown + "\n")
	default:
		for _, turn := range preview.turns {
			b.WriteString("_" + speakerOf(turn.GetRole()) + "_\n\n")
			b.WriteString(turnMarkdown(turn) + "\n\n")
		}
	}

	return b.String()
}

func speakerOf(role string) string {
	switch role {
	case "user":
		return "you"
	case "assistant":
		return "agent"
	}

	return role
}

func turnMarkdown(turn *agentv1.Turn) string {
	content := strings.TrimSpace(paint.ScrubMarks(turn.GetContent()))
	if turn.GetTruncated() && countFences(content)%2 == 1 {
		return "```\n" + content
	}

	return content
}

func countFences(content string) int {
	count := 0
	for line := range strings.Lines(content) {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			count++
		}
	}

	return count
}
