package ui

import (
	"context"

	"komrad/internal/client"
	"komrad/internal/ui/api"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
)

// saidLeadIn is the rows of conversation rendered above the first match when the column opens on it.
const saidLeadIn = 12

// saidTranscript is the conversation of the agent the column reads beside a says: qualifier.
type saidTranscript struct {
	agentID  string
	text     string
	answered bool
	content  string
	err      error
	width    int
	rows     []string
	line     int
	opened   bool
}

type saidTranscriptMsg struct {
	agentID string
	text    string
	content string
	err     error
}

// saidAgentID is the hovered agent the server found saying the text of a says: qualifier.
func (m *model) saidAgentID() (string, bool) {
	var agentID string
	switch screen := m.active().(type) {
	case *agentsScreen:
		agentID = screen.hoveredAgentID(m.inputs())
	case *archivedAgents:
		agentID, _ = screen.screenedAgentID(m.inputs())
	default:
		return "", false
	}

	return agentID, agentID != "" && m.filter.pattern().HasSaid(agentID)
}

// isSaidRead reports whether the column holds the conversation of the agent and text it was read for.
func (s saidTranscript) isSaidRead(agentID, text string) bool {
	return s.agentID == agentID && s.text == text
}

func (m *model) readSaidCmd() tea.Cmd {
	agentID, ok := m.saidAgentID()
	text := m.filter.qualifiers.said.text
	if !ok || m.said.isSaidRead(agentID, text) {
		return nil
	}

	m.said = saidTranscript{agentID: agentID, text: text}
	agents := m.api.Agents

	return func() tea.Msg {
		content, err := api.Call(saidTimeout, func(ctx context.Context) (string, error) {
			return client.GetFullTranscript(ctx, agents, agentID)
		})

		return saidTranscriptMsg{
			agentID: agentID,
			text:    text,
			content: paint.ScrubMarks(content),
			err:     err,
		}
	}
}

func (m *model) applySaidTranscript(msg saidTranscriptMsg) {
	if !m.said.isSaidRead(msg.agentID, msg.text) {
		return
	}

	m.said = saidTranscript{
		agentID:  msg.agentID,
		text:     msg.text,
		answered: true,
		content:  msg.content,
		err:      msg.err,
	}
}

// openSaid puts the reader on the first match once the conversation it was read for is rendered,
// with lead-in above it.
func (m *model) openSaid() {
	if m.said.opened {
		return
	}

	m.reading.Top = max(m.said.line-1-saidLeadIn, 0)
	m.reading.tail = false
	m.said.opened = true
}

// layoutSaid renders the conversation for the column when it has not been rendered at the column's width, and reports whether it is rendered.
func (m *model) layoutSaid() bool {
	agentID, ok := m.saidAgentID()
	if !ok || !m.said.answered || !m.said.isSaidRead(agentID, m.filter.qualifiers.said.text) {
		return false
	}

	split := m.inputs().Room.Column
	if !split.On {
		return false
	}

	if m.said.rows != nil && m.said.width == split.Column {
		return true
	}

	m.said.rows, m.said.line = saidRows(saidRowsParams{
		said:  m.said,
		hits:  m.filter.pattern().SaidHits(agentID),
		width: split.Column,
	})
	m.said.width = split.Column

	return true
}
