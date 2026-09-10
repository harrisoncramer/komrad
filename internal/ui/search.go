package ui

import (
	"context"
	"strings"
	"time"

	"weave/internal/client"
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
	// turns are the hits the server found in the conversations, held apart from the agents whose
	// name says the query, which are read off the listings the panel already holds.
	turns  []searchHit
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
	section  searchSection
}

// searchSection is which run of hits one hit belongs to, drawn in this order.
type searchSection int

const (
	turnSection searchSection = iota
	nearSection
	nameSection
)

func (s searchSection) label() string {
	switch s {
	case nameSection:
		return "agents"
	case turnSection:
		return "conversations"
	case nearSection:
		return "near matches"
	default:
		return ""
	}
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

	// The archived agents a hit can name are only listed while this screen is up, so the first
	// pass is asked for here rather than waited on.
	return m, m.refreshCmd()
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
				hits = append(hits, renderHit(h, turnSection))
			}
			for _, h := range near {
				hits = append(hits, renderHit(h, nearSection))
			}

			return hits, nil
		})
		if err != nil {
			return searchResult{query: query}
		}

		return searchResult{query: query, hits: hits}
	}
}

func renderHit(h *agentv1.SearchHit, section searchSection) searchHit {
	return searchHit{
		agentID:  h.GetAgentId(),
		name:     h.GetName(),
		archived: h.GetArchived(),
		offset:   h.GetContentOffset(),
		length:   h.GetContentLength(),
		snippet:  h.GetSnippet(),
		section:  section,
	}
}

// searchHits is every hit the screen stands on, the agents whose name says the query first.
func (m model) searchHits() []searchHit {
	return append(m.nameHits(), m.search.turns...)
}

// nameHits are the agents whose own name says the query, found in the listings rather than
// asked of the server.
func (m model) nameHits() []searchHit {
	query := strings.TrimSpace(m.search.query)
	if query == "" {
		return nil
	}

	var hits []searchHit
	for _, item := range m.listing.agents {
		if nameSays(item.agent.Name, query) {
			hits = append(hits, searchHit{agentID: item.agent.ID, name: item.agent.Name, section: nameSection})
		}
	}
	for _, agent := range m.archive.agents {
		if nameSays(agent.Name, query) {
			hits = append(hits, searchHit{agentID: agent.ID, name: agent.Name, archived: true, section: nameSection})
		}
	}

	return hits
}

func nameSays(name, query string) bool {
	return strings.Contains(foldASCII(name), foldASCII(query))
}

// markName marks where in a name the query was found.
func markName(name, query string) string {
	folded := foldASCII(strings.TrimSpace(query))
	if folded == "" {
		return name
	}

	at := strings.Index(foldASCII(name), folded)
	if at < 0 {
		return name
	}

	return name[:at] + matchStyle.Render(name[at:at+len(folded)]) + name[at+len(folded):]
}

func (m *model) applySearch(result searchResult) tea.Cmd {
	if result.query != m.search.query {
		return nil
	}

	m.search.turns = groupSections(result.hits)

	hits := m.searchHits()
	m.search.cursor = clampCursor(m.search.cursor, len(hits))
	if len(hits) == 0 {
		m.search.preview = searchPreview{}

		return nil
	}

	return m.previewCmd(hits[m.search.cursor])
}

func (m model) previewCmd(hit searchHit) tea.Cmd {
	agents := m.api.agents
	query := m.search.query

	return func() tea.Msg {
		content, err := call(agentsTimeout, func(ctx context.Context) (string, error) {
			transcript, err := client.GetFullTranscript(ctx, agents, hit.agentID)
			if err != nil {
				return "", err
			}

			return scrubMarks(transcript), nil
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

// groupSections gathers each agent's hits within its own section, keeping a near hit from being
// carried up among the word matches.
func groupSections(hits []searchHit) []searchHit {
	var matches, near []searchHit

	for _, hit := range hits {
		if hit.section == nearSection {
			near = append(near, hit)
			continue
		}

		matches = append(matches, hit)
	}

	return append(groupByAgent(matches), groupByAgent(near)...)
}

// groupByAgent gathers each agent's hits under the first of them.
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

// The three things the column beside the hits says before it has a transcript to draw.
const (
	searchWaitingMarkdown = "_the conversation the cursor is standing on is read here_"
	noSearchHitsMarkdown  = "_nothing said that_"
	searchLoadingMarkdown = "_reading the conversation…_"
)

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
		return m.placeSearchCursor(len(m.searchHits()) - 1)
	case isBound(k, m.keys.Column.ScrollDown):
		return m.scrollPreview(docScrollStep)
	case isBound(k, m.keys.Column.ScrollUp):
		return m.scrollPreview(-docScrollStep)
	case k == "enter":
		return m.jumpToSearchHit()
	}

	return m, nil
}

func (m model) moveSearchCursor(delta int) (tea.Model, tea.Cmd) {
	hits := m.searchHits()

	next, ok := stepCursor(m.search.cursor, delta, len(hits))
	if !ok {
		return m, nil
	}

	m.search.cursor = next

	return m, m.previewCmd(hits[next])
}

func (m model) placeSearchCursor(index int) (tea.Model, tea.Cmd) {
	hits := m.searchHits()
	if len(hits) == 0 {
		return m, nil
	}

	m.search.cursor = clampCursor(index, len(hits))

	return m, m.previewCmd(hits[m.search.cursor])
}

func (m model) hoveredHit() (searchHit, bool) {
	return rowAt(m.searchHits(), m.search.cursor)
}

func (m model) jumpToSearchHit() (tea.Model, tea.Cmd) {
	hit, ok := m.hoveredHit()
	if !ok {
		return m, nil
	}

	m.search = searchState{}

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
// names and not from whatever the preview last fetched.
func (h searchHit) spans(content string) []span {
	return matchSpans(content, h.offset, h.length, markedTerms(h.snippet))
}

// hitHeader names the agent a run of hits came from, wearing the same marks its row wears on the
// screen it lives on.
func (m model) hitHeader(hit searchHit) string {
	name := clipName(hit.name)
	if hit.section == nameSection {
		name = markName(name, m.search.query)
	}

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
		worktreeRemovedMark(item) + scheduledMark(item.agent) + hiddenMark(item.agent.Hidden) +
		unreadMailMark(item.unreadMail) + unviewedMark(item.unviewed) +
		failedRunMark(item.failedRun) + localReviewRoundsMark(item.localRounds) +
		localReviewMark(item.inLocalReview, item.localVerdict)
}

// searchRows lays the hits out as the lines the screen draws, reporting where the cursor's own
// line landed so the window can be placed on it.
func (m model) searchRows() ([]string, int) {
	hits := m.searchHits()
	rows := make([]string, 0, len(hits)*2)
	cursor := 0

	labelled, section := "", searchSection(-1)
	for i, hit := range hits {
		if hit.section != section {
			if len(rows) > 0 {
				rows = append(rows, "")
			}

			rows = append(rows, dimStyle.Render(hit.section.label()))
			section, labelled = hit.section, ""
		}

		hovered := i == m.search.cursor

		var row string
		if hit.section == nameSection {
			row = m.cursorGap(hovered) + m.hitHeader(hit)
		} else {
			if hit.agentID != labelled {
				rows = append(rows, m.hitHeader(hit))
				labelled = hit.agentID
			}

			row = "  " + m.cursorGap(hovered) + dimStyle.Render(renderSnippet(hit.snippet, m.paneWidth()-snippetIndent))
		}

		if hovered {
			cursor = len(rows)
		}

		rows = append(rows, row)
	}

	return rows, cursor
}

// cursorGap is the room a row leaves the cursor, whether or not the cursor is standing on it.
func (m model) cursorGap(hovered bool) string {
	if !hovered {
		return blankCursor
	}

	return m.rowCursorPrefix()
}

// searchView draws the hits and, beside them, the transcript of the one the cursor is standing
// on, as one frame.
func (m model) searchView() string {
	sidebar := m.searchSidebarView()

	split, ok := m.screenColumnSplit()
	if !ok {
		return sidebar
	}

	return m.joinColumns(split, sidebar, m.searchPreviewView(split))
}

// searchSidebarView is the panel's own column: the query and the hits it found.
func (m model) searchSidebarView() string {
	query := cursorStyle.Render("/") + " " + m.search.query
	if m.search.typing {
		query += cursorStyle.Render("▏")
	}
	header := titleOf(m.isListFocused(), "Search") + "\n\n" + query + "\n\n"
	help := m.renderHelp(m.searchHelp())

	switch {
	case strings.TrimSpace(m.search.query) == "":
		return header + dimStyle.Render("type to search transcripts") + "\n" + help
	case len(m.searchHits()) == 0:
		return header + dimStyle.Render("no matches") + "\n" + help
	}

	rows, cursor := m.searchRows()

	return header + renderList(rows, cursor, m.listLines(header, help)) + help
}
