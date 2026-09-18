package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/internal/client"
	"weave/internal/ui/paint"
	"weave/internal/view"
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

// searchSection is which run of hits one hit belongs to.
type searchSection int

const (
	turnSection searchSection = iota
	nearSection
	nameSection
)

// snippetIndent is how far a snippet is drawn in from the row naming the hit it came from,
// which is the column that row opens its name at.
func snippetIndent() int {
	return view.GutterColumns(view.Columns{})
}

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

// searchHitKey names one hit across a refresh, which is the turn it came from within its own
// section.
func searchHitKey(hit searchHit) string {
	return fmt.Sprintf("%d:%s:%d", hit.section, hit.agentID, hit.offset)
}

// hoveredHitKey names the hit the cursor is standing on, read before a refresh moves the rows
// under it.
func (m model) hoveredHitKey() string {
	hit, ok := m.hoveredHit()
	if !ok {
		return ""
	}

	return searchHitKey(hit)
}

// placeSearchHit puts the cursor back on the hit it was standing on, the names among the hits
// being read off listings a refresh changes, and fetches a preview where that hit has gone.
func (m *model) placeSearchHit(was string) tea.Cmd {
	hits := m.searchHits()
	if len(hits) == 0 {
		m.search.cursor = 0
		m.search.preview = searchPreview{}

		return nil
	}

	at := slices.IndexFunc(hits, func(hit searchHit) bool { return searchHitKey(hit) == was })
	m.search.cursor = placeCursor(at, m.search.cursor, len(hits))
	if at >= 0 {
		return nil
	}

	return m.previewCmd(hits[m.search.cursor])
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
			hits = append(hits, searchHit{
				agentID: item.agent.ID,
				name:    item.agent.Name,
				section: nameSection,
			})
		}
	}
	for _, agent := range m.archive.agents {
		if nameSays(agent.Name, query) {
			hits = append(hits, searchHit{
				agentID:  agent.ID,
				name:     agent.Name,
				archived: true,
				section:  nameSection,
			})
		}
	}

	return hits
}

func nameSays(name, query string) bool {
	return strings.Contains(paint.FoldASCII(name), paint.FoldASCII(query))
}

// nameMatches is where in a name the query was found, in bytes of that name, which the fold
// keeps where a Unicode one would move them.
func nameMatches(name, query string) []view.Span {
	folded := paint.FoldASCII(strings.TrimSpace(query))
	if folded == "" {
		return nil
	}

	at := strings.Index(paint.FoldASCII(name), folded)
	if at < 0 {
		return nil
	}

	return []view.Span{{Start: at, End: at + len(folded)}}
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

			return paint.ScrubMarks(transcript), nil
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
			m.search.query = paint.TrimLastRune(m.search.query)
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
func (h searchHit) spans(content string) []paint.Span {
	return paint.MatchSpans(content, h.offset, h.length, paint.MarkedTerms(h.snippet))
}

// searchDropped are the three columns the search screen gives up: a hit is a pointer into a
// conversation rather than a piece of work to triage.
var searchDropped = view.Columns{Repo: true, Review: true, Forge: true}

// searchRow reads one hit into the row the agents list draws, keyed only where the cursor can
// stand on it so a snippet does not light the header above it.
func (m model) searchRow(hit searchHit, key string) view.Row {
	row := view.Row{
		Key:     key,
		Kind:    view.KindResult,
		Name:    hit.name,
		Matches: nameMatches(hit.name, m.search.query),
		Mark:    m.listing.marks[agentRowKey(hit.agentID)],
		Process: view.ProcessNone,
	}

	if hit.archived {
		return m.archivedSearchRow(row, hit.agentID)
	}

	item, ok := m.liveAgent(hit.agentID)
	if !ok {
		return row
	}

	return agentFacts(row, item)
}

// archivedSearchRow is a hit on an agent nobody can reach any more, read the same way the archive
// screen reads it so the two cannot say different things about one agent.
func (m model) archivedSearchRow(row view.Row, id string) view.Row {
	// Nothing is known about a hit the archive listing has not landed yet, beyond its being over.
	row.Process = view.ProcessStopped

	if agent, ok := m.archivedAgent(id); ok {
		row = agentFacts(row, agentItem{agent: agent})
	}

	// The server said this hit is archived, which holds whether or not the listing carrying the
	// stamp has arrived.
	row.Archived = true

	return row
}

// searchLine is one line of the hits: either a row of the agents list, drawn with the rest so
// every one of them fits the same gutter, or text already drawn.
type searchLine struct {
	header bool
	row    view.Row
	text   string
}

// searchLines lays the hits out, reporting where the cursor's own line landed so the window can
// be placed on it.
func (m model) searchLines() ([]searchLine, int) {
	hits := m.searchHits()
	lines := make([]searchLine, 0, len(hits)*2)
	cursor := 0

	labelled, section := "", searchSection(-1)
	for i, hit := range hits {
		if hit.section != section {
			if len(lines) > 0 {
				lines = append(lines, searchLine{})
			}

			lines = append(lines, searchLine{text: dimStyle.Render(hit.section.label())})
			section, labelled = hit.section, ""
		}

		hovered := i == m.search.cursor
		if hovered {
			cursor = len(lines)
		}

		// A name is its own row, so the cursor stands on the header rather than under it.
		if hit.section == nameSection {
			lines = append(lines, searchLine{header: true, row: m.searchRow(hit, searchHitKey(hit))})
			continue
		}

		if hit.agentID != labelled {
			lines = append(lines, searchLine{header: true, row: m.searchRow(hit, "")})
			labelled = hit.agentID
			if hovered {
				cursor = len(lines)
			}
		}

		lines = append(lines, searchLine{text: m.snippetRow(hit, hovered)})
	}

	return lines, cursor
}

// snippetRow is one matched turn drawn under the row naming the agent it came from.
func (m model) snippetRow(hit searchHit, hovered bool) string {
	indent := strings.Repeat(" ", max(snippetIndent()-len(blankCursor), 0))
	body := paint.Panel.RenderSnippet(hit.snippet, m.paneWidth()-snippetIndent())

	return indent + m.cursorGap(hovered) + dimStyle.Render(body)
}

// searchRows draws the hits as the lines the screen holds, reporting where the cursor's own line
// landed so the window can be placed on it.
func (m model) searchRows() ([]string, int) {
	lines, cursor := m.searchLines()

	headers := make([]view.Row, 0, len(lines))
	for _, line := range lines {
		if line.header {
			headers = append(headers, line.row)
		}
	}

	palette := paint.Panel
	drawn := view.Rows(view.Prepare(view.Snapshot{
		Rows:        headers,
		Dropped:     searchDropped,
		CaretHidden: m.cursorBlinkedOut(m.isListFocused()),
		Focused:     m.hoveredHitKey(),
		Pane:        m.paneWidth(),
	}), palette, m.paneWidth())

	rows := make([]string, 0, len(lines))
	at := 0
	for _, line := range lines {
		if !line.header {
			rows = append(rows, line.text)

			continue
		}

		rows = append(rows, drawn[at])
		at++
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
	palette := paint.Panel

	query := palette.Cursor().Render("/") + " " + m.search.query
	if m.search.typing {
		query += palette.Cursor().Render("▏")
	}
	header := palette.Title("Search") + "\n\n" + query + "\n\n"
	help := m.renderHelp(m.searchHelp())

	switch {
	case strings.TrimSpace(m.search.query) == "":
		return header + dimStyle.Render("type to search transcripts") + "\n" + help
	case len(m.searchHits()) == 0:
		return header + dimStyle.Render("no matches") + "\n" + help
	}

	rows, cursor := m.searchRows()

	return header + paint.RenderList(rows, cursor, m.listLines(header, help), dimStyle) + help
}
