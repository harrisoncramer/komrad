package ui

import (
	"context"
	"slices"
	"strings"

	"komrad/internal/client"
	"komrad/internal/errui"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
)

// saidLeadIn is the rows of conversation drawn above the first match when the column opens on it.
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
		agentID = screen.hoveredAgentID(m)
	case *archivedAgents:
		agentID, _ = screen.screenedAgentID(m)
	default:
		return "", false
	}

	return agentID, agentID != "" && m.filter.hasSaid(agentID)
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
	agents := m.api.reads().agents

	return func() tea.Msg {
		content, err := call(agentsTimeout, func(ctx context.Context) (string, error) {
			return client.GetFullTranscript(ctx, agents, agentID)
		})

		return saidTranscriptMsg{agentID: agentID, text: text, content: paint.ScrubMarks(content), err: err}
	}
}

func (m *model) applySaidTranscript(msg saidTranscriptMsg) {
	if !m.said.isSaidRead(msg.agentID, msg.text) {
		return
	}

	m.said = saidTranscript{agentID: msg.agentID, text: msg.text, answered: true, content: msg.content, err: msg.err}
}

// openSaid puts the reader on the first match once the conversation it was read for is drawn,
// with lead-in above it.
func (m *model) openSaid() {
	agentID, ok := m.saidAgentID()
	if !ok || m.reading.opened || !m.said.answered || !m.said.isSaidRead(agentID, m.filter.qualifiers.said.text) {
		return
	}

	split, ok := m.columnSplit()
	if !ok {
		return
	}

	m.saidColumnRows(agentID, split.column)
	m.reading.Top = max(m.said.line-1-saidLeadIn, 0)
	m.reading.tail = false
	m.reading.opened = true
}

// saidColumnRows is the conversation of an agent the server found saying the text of a says:
// qualifier, drawn for the column, and none when it was found saying nothing.
func (m *model) saidColumnRows(agentID string, width int) []string {
	hits := m.filter.saidHits(agentID)
	if len(hits) == 0 {
		return nil
	}

	if !m.said.isSaidRead(agentID, m.filter.qualifiers.said.text) || !m.said.answered {
		return paint.DocStyledRows("_reading the conversation…_", max(width, 1), columnStyle())
	}

	if m.said.rows == nil || m.said.width != width {
		m.said.rows, m.said.line = saidRows(saidRowsParams{said: m.said, hits: hits, width: width})
		m.said.width = width
	}

	return m.said.rows
}

type saidRowsParams struct {
	said  saidTranscript
	hits  []saidHit
	width int
}

// saidRows is a conversation drawn for the column with every word the server matched in it
// marked, and the line the first of them is on, or one line saying why there is none.
func saidRows(params saidRowsParams) ([]string, int) {
	width := max(params.width, 1)
	switch {
	case params.said.err != nil:
		return paint.DocStyledRows("_could not read the conversation: "+errui.GetReason(params.said.err)+"_", width, columnStyle()), 0
	case strings.TrimSpace(params.said.content) == "":
		return paint.DocStyledRows("_no transcript yet_", width, columnStyle()), 0
	}

	content := params.said.content
	spans := []paint.Span{}
	for _, hit := range params.hits {
		spans = append(spans, paint.MatchSpans(content, hit.offset, hit.length, paint.MarkedTerms(hit.snippet))...)
	}
	slices.SortFunc(spans, func(x, y paint.Span) int { return x.Start - y.Start })

	text, line := paint.Panel.RenderTranscript(paint.RenderTranscriptParams{
		Conversation: content,
		Spans:        spans,
		Width:        width,
	})

	return strings.Split(strings.TrimRight(text, "\n"), "\n"), line
}
