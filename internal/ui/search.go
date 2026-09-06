package ui

import (
	"context"
	"strings"
	"time"

	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/proto"
)

type searchState struct {
	typing bool
	query  string
	hits   []searchHit
	cursor int
	// preview is the transcript of the hit the cursor is standing on, drawn beside the list.
	preview searchPreview
}

const searchLimit = 10

const searchDebounce = 120 * time.Millisecond

type searchHit struct {
	agentID  string
	name     string
	archived bool
	offset   int64
	length   int64
	snippet  string
	near     bool
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
	content string
}

func (m model) enterSearch() (tea.Model, tea.Cmd) {
	m.enterScreen(screenSearch)
	m.search = searchState{typing: true}
	// The search draws the transcript and the hits itself, so it takes the window off the panes
	// it would otherwise be sharing with.
	m.frame.Solo(true)

	// The archived agents a hit can name are only listed while this screen is up, so the first
	// pass is asked for here rather than waited on.
	return m, m.refreshCmd()
}

// leaveSearch lets go of the search and hands the window back, since the screen it steps to
// draws in a pane of the frame's rather than in a column of its own.
func (m *model) leaveSearch() {
	m.search = searchState{}
	m.frame.Solo(false)
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

		hits, err := call(agentsTimeout, func(ctx context.Context) ([]searchHit, error) {
			resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
				Query:      query,
				Pagination: &paginationv1.Pagination{Limit: proto.Int32(searchLimit)},
			}))
			if err != nil {
				return nil, err
			}

			found := resp.Msg.GetMatches()
			near := resp.Msg.GetNear()

			hits := make([]searchHit, 0, len(found)+len(near))
			for _, h := range found {
				hits = append(hits, renderHit(h, false))
			}
			for _, h := range near {
				hits = append(hits, renderHit(h, true))
			}

			return hits, nil
		})
		if err != nil {
			return searchResult{query: query}
		}

		return searchResult{query: query, hits: hits}
	}
}

func renderHit(h *agentv1.SearchHit, near bool) searchHit {
	return searchHit{
		agentID:  h.GetAgentId(),
		name:     h.GetName(),
		archived: h.GetArchived(),
		offset:   h.GetContentOffset(),
		length:   h.GetContentLength(),
		snippet:  h.GetSnippet(),
		near:     near,
	}
}

func (m *model) applySearch(result searchResult) tea.Cmd {
	if result.query != m.search.query {
		return nil
	}

	m.search.hits = groupSections(result.hits)
	m.search.cursor = clampCursor(m.search.cursor, len(m.search.hits))
	if len(m.search.hits) == 0 {
		m.search.preview = searchPreview{}

		return nil
	}

	return m.previewCmd(m.search.hits[m.search.cursor])
}

func (m model) previewCmd(hit searchHit) tea.Cmd {
	client := m.api.agents
	query := m.search.query

	return func() tea.Msg {
		content, err := call(agentsTimeout, func(ctx context.Context) (string, error) {
			resp, err := client.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{
				AgentId: hit.agentID,
			}))
			if err != nil {
				return "", err
			}

			return scrubMarks(resp.Msg.GetContent()), nil
		})
		if err != nil {
			return previewResult{agentID: hit.agentID, query: query}
		}

		return previewResult{
			agentID: hit.agentID,
			query:   query,
			content: content,
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

	m.holdPreview(result.content, hit.spans(result.content))
}

// groupSections gathers each agent's hits within its own section, since an agent with a word
// match and a near match has one of each, and grouping across the two would carry a near hit
// up among the matches and leave the heading in the wrong place.
func groupSections(hits []searchHit) []searchHit {
	var matches, near []searchHit

	for _, hit := range hits {
		if hit.near {
			near = append(near, hit)
			continue
		}

		matches = append(matches, hit)
	}

	return append(groupByAgent(matches), groupByAgent(near)...)
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

func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.search.typing {
		return m.updateSearchInput(key)
	}

	return m.updateSearchHits(key)
}

func (m model) updateSearchInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := key.String(); {
	case k == "esc":
		m.leaveSearch()

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
	case isBound(k, m.keys.Search.ScrollDown):
		return m.scrollPreview(searchScrollStep)
	case isBound(k, m.keys.Search.ScrollUp):
		return m.scrollPreview(-searchScrollStep)
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

	m.leaveSearch()

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

// spans locates this hit's own matched words in the transcript, measured from the turn the hit
// names rather than from whatever the preview last fetched, since an agent's hits sit next to
// each other in the listing and the transcript on hand can belong to the one before it.
func (h searchHit) spans(content string) []span {
	return matchSpans(content, h.offset, h.length, markedTerms(h.snippet))
}

// hitHeader names the agent a run of hits came from, wearing the same marks its row wears on
// the screen it lives on, since a snippet says nothing about the agent that wrote it.
func (m model) hitHeader(hit searchHit) string {
	name := clipName(hit.name)

	if hit.archived {
		row := "  " + noStateMark() + " " + name + dimStyle.Render(" (archived)")

		agent, ok := m.archivedAgent(hit.agentID)
		if !ok {
			return row
		}

		return row + failedMark(agent) + scheduledMark(agent)
	}

	item, ok := m.liveAgent(hit.agentID)
	if !ok {
		return "  " + noStateMark() + " " + name
	}

	return markGutter(m.listing.marks[agentRowKey(item.agent.ID)]) + stateMark(displayState(item)) + " " + name +
		worktreeRemovedMark(item.agent) + scheduledMark(item.agent) + hiddenMark(item.agent.Hidden) +
		unreadMailMark(item.unreadMail) + unviewedMark(item.unviewed) +
		failedRunMark(item.failedRun)
}

// searchRows lays the hits out as the lines the screen draws, reporting where the cursor's own
// line landed so the window can be placed on it.
func (m model) searchRows() ([]string, int) {
	rows := make([]string, 0, len(m.search.hits)*2)
	cursor := 0

	labelled, sectioned := "", false
	for i, hit := range m.search.hits {
		if hit.near && !sectioned {
			rows = append(rows, "", dimStyle.Render("near matches"))
			sectioned, labelled = true, ""
		}
		if hit.agentID != labelled {
			rows = append(rows, m.hitHeader(hit))
			labelled = hit.agentID
		}

		prefix := "    "
		if i == m.search.cursor {
			prefix = "  " + m.rowCursorStyle().Render("› ")
			cursor = len(rows)
		}

		rows = append(rows, prefix+dimStyle.Render(renderSnippet(hit.snippet, m.paneWidth()-snippetIndent)))
	}

	return rows, cursor
}

// searchView draws the hits and, beside them, the transcript of the one the cursor is standing
// on, as one frame rather than pushing a screenful of it into a pane of its own.
func (m model) searchView() string {
	sidebar := m.searchSidebarView()

	split, ok := m.searchColumns()
	if !ok {
		return sidebar
	}

	sidebar = fitColumn(strings.TrimRight(sidebar, "\n"), split.sidebar)
	preview := m.searchPreviewView(split)

	if m.frame.IsSidebarOnRight() {
		return lipgloss.JoinHorizontal(lipgloss.Top, preview, sidebar)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, preview)
}

// searchSidebarView is the panel's own column: the query and the hits it found.
func (m model) searchSidebarView() string {
	query := cursorStyle.Render("/") + " " + m.search.query
	if m.search.typing {
		query += cursorStyle.Render("▏")
	}
	header := m.titleStyle().Render("Search") + "\n\n" + query + "\n\n"
	help := "\n" + m.renderHelp(m.searchHelp())

	switch {
	case strings.TrimSpace(m.search.query) == "":
		return header + dimStyle.Render("type to search transcripts") + "\n" + help
	case len(m.search.hits) == 0:
		return header + dimStyle.Render("no matches") + "\n" + help
	}

	rows, cursor := m.searchRows()

	return header + renderList(rows, cursor, m.listLines(header, help)) + help
}
