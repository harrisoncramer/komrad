package ui

import (
	"context"
	"strings"
	"time"

	"weave/internal/ui/layout"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

type searchState struct {
	typing bool

	query  string
	hits   []searchHit
	cursor int

	preview previewResult
}

const searchLimit = 10

const searchDebounce = 120 * time.Millisecond

type searchHit struct {
	agentID  string
	name     string
	archived bool
	offset   int64
	snippet  string
}

type searchSettledMsg struct {
	query string
}

type searchResult struct {
	query string
	hits  []searchHit
}

type previewResult struct {
	agentID string
	query   string
	offset  int64
	content string
}

func (m model) enterSearch() (tea.Model, tea.Cmd) {
	m.enterScreen(screenSearch)
	m.search = searchState{typing: true}
	m.frame.Show(layout.RegionStage, newBlankContent())

	return m, nil
}

func (m model) openSearchInput() (tea.Model, tea.Cmd) {
	m.search.typing = true

	return m, nil
}

func (m model) debounceSearchCmd(query string) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		return func() tea.Msg { return searchResult{query: query} }
	}

	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return searchSettledMsg{query: query}
	})
}

func (m model) runSettledSearch(msg searchSettledMsg) tea.Cmd {
	if msg.query != m.search.query {
		return nil
	}

	return m.searchCmd(msg.query)
}

func (m model) searchCmd(query string) tea.Cmd {
	client := m.api.agents

	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResult{query: query}
		}

		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query:      query,
			Pagination: &paginationv1.Pagination{Limit: proto.Int32(searchLimit)},
		}))
		if err != nil {
			return searchResult{query: query}
		}

		hits := make([]searchHit, 0, len(resp.Msg.GetHits()))
		for _, h := range resp.Msg.GetHits() {
			hits = append(hits, searchHit{
				agentID:  h.GetAgentId(),
				name:     h.GetName(),
				archived: h.GetArchived(),
				offset:   h.GetContentOffset(),
				snippet:  h.GetSnippet(),
			})
		}

		return searchResult{query: query, hits: hits}
	}
}

func (m *model) applySearch(result searchResult) tea.Cmd {
	if result.query != m.search.query {
		return nil
	}

	m.search.hits = groupByAgent(result.hits)
	m.search.cursor = clampCursor(m.search.cursor, len(m.search.hits))
	if len(m.search.hits) == 0 {
		m.frame.Show(layout.RegionStage, newBlankContent())
		return nil
	}

	return m.previewCmd(m.search.hits[m.search.cursor])
}

func (m model) previewCmd(hit searchHit) tea.Cmd {
	client := m.api.agents
	query := m.search.query

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		resp, err := client.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{
			AgentId: hit.agentID,
		}))
		if err != nil {
			return previewResult{
				agentID: hit.agentID,
				query:   query,
				offset:  hit.offset,
			}
		}

		return previewResult{
			agentID: hit.agentID,
			query:   query,
			offset:  hit.offset,
			content: resp.Msg.GetContent(),
		}
	}
}

func (m *model) applyPreview(result previewResult) {
	if result.query != m.search.query {
		return
	}

	hit, ok := m.hoveredHit()
	if !ok || hit.agentID != result.agentID {
		return
	}

	m.search.preview = result
	m.stageTranscript(result.content, lineAtOffset(result.content, result.offset))
}

// groupByAgent gathers each agent's hits under the first of them, since the ranking interleaves
// agents and a header is only worth drawing over the rows it stands for.
func groupByAgent(hits []searchHit) []searchHit {
	grouped := make([]searchHit, 0, len(hits))
	gathered := make(map[string]bool, len(hits))

	for _, hit := range hits {
		if gathered[hit.agentID] {
			continue
		}
		gathered[hit.agentID] = true

		grouped = append(grouped, xslice.Filter(hits, func(h searchHit) bool {
			return h.agentID == hit.agentID
		})...)
	}

	return grouped
}

const noTranscriptMarkdown = "_no transcript yet_"

func (m *model) stageTranscript(conversation string, line int) {
	if strings.TrimSpace(conversation) == "" {
		m.frame.Show(layout.RegionStage, newTextContent(noTranscriptMarkdown))

		return
	}

	m.frame.Show(layout.RegionStage, newTranscriptContent(conversation, line))
}

func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.search.typing {
		return m.updateSearchInput(key)
	}

	return m.updateSearchHits(key)
}

func (m model) updateSearchInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := key.String(); {
	case k == "esc":
		m.search = searchState{}

		return m.returnToAgents()
	case k == "enter":
		m.search.typing = false

		return m, nil
	case k == "ctrl+c":
		return m, m.hide()
	case k == "backspace":
		if m.search.query != "" {
			m.search.query = trimLastRune(m.search.query)
		}

		return m, m.debounceSearchCmd(m.search.query)
	case key.Type == tea.KeySpace:
		m.search.query += " "

		return m, m.debounceSearchCmd(m.search.query)
	case key.Type == tea.KeyRunes:
		m.search.query += string(key.Runes)

		return m, m.debounceSearchCmd(m.search.query)
	}

	return m, nil
}

func (m model) updateSearchHits(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return m.placeSearchCursor(0)
	}
	if m.topHeld {
		return m, nil
	}

	switch {
	case k == "esc", isBound(k, m.keys.Filter.Open):
		return m.openSearchInput()
	case k == "ctrl+c":
		return m, m.hide()
	case isBound(k, m.keys.Nav.Up):
		return m.moveSearchCursor(-1)
	case isBound(k, m.keys.Nav.Down):
		return m.moveSearchCursor(1)
	case isBound(k, m.keys.Nav.Bottom):
		return m.placeSearchCursor(len(m.search.hits) - 1)
	case k == "enter":
		return m.jumpToSearchHit()
	}

	return m, nil
}

func (m model) moveSearchCursor(delta int) (tea.Model, tea.Cmd) {
	next, ok := stepCursor(m.search.cursor, delta, len(m.search.hits))
	if !ok {
		return m, nil
	}

	m.search.cursor = next

	return m, m.previewCmd(m.search.hits[next])
}

func (m model) placeSearchCursor(index int) (tea.Model, tea.Cmd) {
	if len(m.search.hits) == 0 {
		return m, nil
	}

	m.search.cursor = clampCursor(index, len(m.search.hits))

	return m, m.previewCmd(m.search.hits[m.search.cursor])
}

func (m model) hoveredHit() (searchHit, bool) {
	return rowAt(m.search.hits, m.search.cursor)
}

func (m model) jumpToSearchHit() (tea.Model, tea.Cmd) {
	hit, ok := m.hoveredHit()
	if !ok {
		return m, nil
	}

	m.match = matchFor(hit, m.search.preview)

	if hit.archived {
		m.archive.focus = hit.agentID
		// The stage is drawn before the listing the focus is spent on comes back, so the cursor
		// is placed here too when the archive is already held.
		if idx := rowIndexOfAgentID(m.archiveRows(), hit.agentID); idx >= 0 {
			m.archive.cursor = idx
		}

		return m.enterArchive()
	}

	m.enterScreen(screenAgents)
	m.revealAgent(hit.agentID)
	m.stageSelection()

	return m, nil
}

// matchFocus is where a search hit sat in its agent's transcript, kept because the screen the
// jump lands on redraws its stage from the row under the cursor every refresh.
type matchFocus struct {
	agentID string
	content string
	line    int
}

func matchFor(hit searchHit, preview previewResult) matchFocus {
	if preview.agentID != hit.agentID || strings.TrimSpace(preview.content) == "" {
		return matchFocus{}
	}

	// An offset the content does not hold reads as line zero, which is the top of the
	// transcript and so nothing worth taking the stage for.
	line := lineAtOffset(preview.content, hit.offset)
	if line == 0 {
		return matchFocus{}
	}

	return matchFocus{
		agentID: hit.agentID,
		content: preview.content,
		line:    line,
	}
}

// stageMatch puts the transcript a search hit came from on the stage at the line it matched on,
// reporting whether it took the stage, and holds it until the cursor moves to another agent.
func (m *model) stageMatch(agentID string) bool {
	if m.match.agentID == "" {
		return false
	}
	if m.match.agentID != agentID {
		m.match = matchFocus{}

		return false
	}

	m.stageTranscript(m.match.content, m.match.line)

	return true
}

func (m model) searchView() string {
	var b strings.Builder
	b.WriteString(m.titleStyle().Render("Search") + "\n\n")
	query := cursorStyle.Render("/") + " " + m.search.query
	if m.search.typing {
		query += cursorStyle.Render("▏")
	}
	b.WriteString(query + "\n\n")

	switch {
	case strings.TrimSpace(m.search.query) == "":
		b.WriteString(dimStyle.Render("type to search transcripts") + "\n")
	case len(m.search.hits) == 0:
		b.WriteString(dimStyle.Render("no matches") + "\n")
	default:

		labelled := ""
		for i, hit := range m.search.hits {
			if hit.agentID != labelled {
				name := clipName(hit.name)
				if hit.archived {
					name += dimStyle.Render(" (archived)")
				}
				b.WriteString("  " + name + "\n")
				labelled = hit.agentID
			}

			prefix := "    "
			if i == m.search.cursor {
				prefix = "  " + m.rowCursorStyle().Render("› ")
			}

			b.WriteString(prefix + dimStyle.Render(renderSnippet(hit.snippet, m.width-snippetIndent)) + "\n")
		}
	}

	b.WriteString("\n" + m.renderHelp(m.searchHelp()))

	return b.String()
}
