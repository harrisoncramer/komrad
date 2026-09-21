package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"weave/config"
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
	rowCursor
	typing bool
	query  string
	// turns are the hits the server found in the conversations, one per turn that said the query.
	turns []searchHit
	rows  []searchAgent
	// preview is the transcript of the agent the cursor is standing on, drawn beside the list.
	preview searchPreview
}

func searchList() listSpec[searchAgent] {
	return listSpec[searchAgent]{
		cursor: func(m *model) *rowCursor { return &m.search.rowCursor },
		rows:   model.searchAgents,
		moved: func(m model) (model, tea.Cmd) {
			agent, ok := m.hoveredSearchAgent()
			if !ok {
				return m, nil
			}

			return m, m.previewCmd(agent)
		},
		quit: func(config.Keybindings) []string { return []string{"ctrl+c"} },
	}
}

// searchActions is the search screen as one table once the query line has given the keys up.
func searchActions() actionSpec {
	list := searchList()

	return actionSpec{
		table: append(list.actions(),
			action{
				id:   actionSearchEdit,
				keys: func(config.Keybindings) []string { return []string{"esc", filterOpenKey} },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openSearchInput() },
			},
			action{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Column.ScrollDown, k.Column.ScrollUp) },
				run:  model.scrollPreviewPressed,
			},
			action{
				id:   actionSearchOpen,
				keys: func(config.Keybindings) []string { return []string{"enter"} },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.jumpToSearchAgent() },
			},
		),
		legend: slices.Concat([]legendRow{
			{
				of:    []actionID{actionSearchOpen},
				desc:  func(model) string { return "go to it" },
				strip: true,
			},
			{
				of:    []actionID{actionSearchEdit},
				desc:  func(model) string { return "edit the query" },
				strip: true,
			},
		}, navLegend(), []legendRow{
			{
				of:   []actionID{actionColumnScroll},
				desc: func(model) string { return "scroll the transcript" },
				label: func(k config.Keybindings) string {
					return keyLabel(k.Column.ScrollDown) + "/" + keyLabel(k.Column.ScrollUp)
				},
			},
		}),
	}
}

func (m model) scrollPreviewPressed(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Column.ScrollUp) {
		return m.scrollPreview(-docScrollStep)
	}

	return m.scrollPreview(docScrollStep)
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
}

// searchAgent is one row of the search: an agent, with the turns of its conversation that said
// the query.
type searchAgent struct {
	id       string
	name     string
	archived bool
	turns    []searchHit
}

func (a searchAgent) key() string {
	return a.id
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

	return m, m.refreshCmd()
}

func (m model) openSearchInput() (tea.Model, tea.Cmd) {
	m.search.typing = true

	return m, nil
}

func debounceSearchCmd(query string) tea.Cmd {
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

			// The turns that say the words stand before the ones that only carry the letters.
			found := slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear())

			return xslice.Map(found, searchHitFrom), nil
		})
		if err != nil {
			return searchResult{query: query}
		}

		return searchResult{query: query, hits: hits}
	}
}

func searchHitFrom(h *agentv1.SearchHit) searchHit {
	return searchHit{
		agentID:  h.GetAgentId(),
		name:     h.GetName(),
		archived: h.GetArchived(),
		offset:   h.GetContentOffset(),
		length:   h.GetContentLength(),
		snippet:  h.GetSnippet(),
	}
}

// hoveredSearchID names the agent the cursor is standing on.
func (m model) hoveredSearchID() string {
	return searchList().hovered(m)
}

// placeSearchAgent puts the cursor back on the agent it was standing on, and fetches a preview
// where that agent has gone.
func (m *model) placeSearchAgent(was string) tea.Cmd {
	agents := m.searchAgents()
	if len(agents) == 0 {
		m.search.cursor = 0
		m.search.preview = searchPreview{}

		return nil
	}

	at := rowIndexOfKey(agents, was)
	m.search.cursor = placeCursor(at, m.search.cursor, len(agents))
	if at >= 0 {
		return nil
	}

	return m.previewCmd(agents[m.search.cursor])
}

// searchAgents is every agent the search found, once each, the named ones first.
func (m model) searchAgents() []searchAgent {
	if m.search.rows != nil {
		return m.search.rows
	}

	return m.buildSearchAgents()
}

func (m model) buildSearchAgents() []searchAgent {
	query := strings.TrimSpace(m.search.query)
	if query == "" {
		return nil
	}

	var agents []searchAgent
	at := make(map[string]int)
	add := func(agent searchAgent) int {
		if i, ok := at[agent.id]; ok {
			return i
		}
		at[agent.id] = len(agents)
		agents = append(agents, agent)

		return len(agents) - 1
	}

	for _, item := range m.listing.agents {
		if doesNameSay(item.agent.Name, query) {
			add(searchAgent{id: item.agent.ID, name: item.agent.Name})
		}
	}
	for _, item := range m.listing.archivedAgents {
		if doesNameSay(item.agent.Name, query) {
			add(searchAgent{
				id:       item.agent.ID,
				name:     item.agent.Name,
				archived: true,
			})
		}
	}
	for _, hit := range m.search.turns {
		i := add(searchAgent{
			id:       hit.agentID,
			name:     hit.name,
			archived: hit.archived,
		})
		agents[i].turns = append(agents[i].turns, hit)
	}

	return agents
}

func doesNameSay(name, query string) bool {
	return strings.Contains(paint.FoldASCII(name), paint.FoldASCII(query))
}

// nameMatches is where in a name the query was found, in bytes of that name.
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

	m.search.turns = result.hits

	agents := m.searchAgents()
	m.search.cursor = clampCursor(m.search.cursor, len(agents))
	if len(agents) == 0 {
		m.search.preview = searchPreview{}

		return nil
	}

	return m.previewCmd(agents[m.search.cursor])
}

func (m model) previewCmd(agent searchAgent) tea.Cmd {
	agents := m.api.agents
	query := m.search.query

	return func() tea.Msg {
		content, err := call(agentsTimeout, func(ctx context.Context) (string, error) {
			transcript, err := client.GetFullTranscript(ctx, agents, agent.id)
			if err != nil {
				return "", err
			}

			return paint.ScrubMarks(transcript), nil
		})
		if err != nil {
			return previewResult{agentID: agent.id, query: query}
		}

		return previewResult{
			agentID: agent.id,
			query:   query,
			content: content,
		}
	}
}

func (m *model) applyPreview(result previewResult) {
	if result.query != m.search.query {
		return
	}

	agent, ok := m.hoveredSearchAgent()
	if !ok || agent.id != result.agentID {
		return
	}

	m.holdPreview(result.content, agent.spans(result.content))
}

const noTranscriptMarkdown = "_no transcript yet_"

// The three things the column beside the list says before it has a transcript to draw.
const (
	searchWaitingMarkdown = "_the conversation the cursor is standing on is read here_"
	noSearchHitsMarkdown  = "_nothing said that_"
	searchLoadingMarkdown = "_reading the conversation…_"
)

func (m model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.search.typing {
		return m.updateSearchInput(key)
	}

	return m.updateSearchList(key)
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

		return m, debounceSearchCmd(m.search.query)
	case key.Type == tea.KeySpace:
		m.search.query += " "

		return m, debounceSearchCmd(m.search.query)
	case key.Type == tea.KeyRunes:
		m.search.query += string(key.Runes)

		return m, debounceSearchCmd(m.search.query)
	}

	return m, nil
}

func (m model) updateSearchList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return searchList().update(m, key, searchActions())
}

func (m model) hoveredSearchAgent() (searchAgent, bool) {
	return rowAt(m.searchAgents(), m.search.cursor)
}

func (m model) jumpToSearchAgent() (tea.Model, tea.Cmd) {
	agent, ok := m.hoveredSearchAgent()
	if !ok {
		return m, nil
	}

	m.search = searchState{}

	if agent.archived {
		m.archive.focus = agent.id
		// The stage is drawn before the listing the focus is spent on comes back, so the cursor
		// is placed here too when the archive is already held.
		if idx := rowIndexOfAgentID(m.archiveRows(), agent.id); idx >= 0 {
			m.archive.cursor = idx
		}

		return m.enterArchive()
	}

	m.enterScreen(screenAgents)
	m.revealAgent(agent.id)
	m.stageSelection()

	return m, nil
}

// spans locates every matched word in the transcript, in the order they are read.
func (a searchAgent) spans(content string) []paint.Span {
	var spans []paint.Span
	for _, hit := range a.turns {
		spans = append(spans, hit.spans(content)...)
	}

	slices.SortFunc(spans, func(x, y paint.Span) int { return x.Start - y.Start })

	return spans
}

// spans locates this hit's own matched words in the turn the hit names.
func (h searchHit) spans(content string) []paint.Span {
	return paint.MatchSpans(content, h.offset, h.length, paint.MarkedTerms(h.snippet))
}

// searchDropped are the three columns the search screen gives up.
var searchDropped = view.Columns{
	Repo:   true,
	Review: true,
	Forge:  true,
}

// searchRow reads one agent the search found into the row the agents list draws.
func (m model) searchRow(agent searchAgent, archived map[string]agentItem) view.Row {
	row := view.Row{
		Key:     agent.id,
		Kind:    view.KindResult,
		Name:    agent.name,
		Matches: nameMatches(agent.name, m.search.query),
		Mark:    m.listing.marks[agentRowKey(agent.id)],
		Process: view.ProcessNone,
	}

	if agent.archived {
		return archivedSearchRow(row, agent.id, archived)
	}

	item, ok := m.liveAgent(agent.id)
	if !ok {
		return row
	}

	return agentFacts(row, item)
}

// archivedSearchRow is a result on an archived agent.
func archivedSearchRow(row view.Row, id string, archived map[string]agentItem) view.Row {
	row.Process = view.ProcessStopped

	if item, ok := archived[id]; ok {
		row = agentFacts(row, item)
	}

	row.Archived = true

	return row
}

// searchSnapshot is the agents the search found, as the list draws them.
func (m model) searchSnapshot(agents []searchAgent, pane int) view.Snapshot {
	archived := archivedByID(m.listing.archivedAgents)

	return view.Snapshot{
		Rows: xslice.Map(agents, func(agent searchAgent) view.Row {
			return m.searchRow(agent, archived)
		}),
		Dropped:       searchDropped,
		CaretHidden:   m.isCursorBlinkedOut(m.isListFocused()),
		BarBlinkedOut: m.rowCursor.Blink,
		Focused:       rowKeyAt(agents, m.search.cursor),
		Blurred:       !m.isSectionLit(sectionList),
		Pane:          pane,
	}
}

// searchView draws the agents the search found beside the transcript of the hovered one.
func (m model) searchView() string {
	m.search.rows = m.buildSearchAgents()

	sidebar := m.searchSidebarView()

	split, ok := m.screenColumnSplit()
	if !ok {
		return sidebar
	}

	return m.joinColumns(split, sidebar, m.searchPreviewView(split))
}

// searchSidebarView is the panel's own column: the query and the agents it found.
func (m model) searchSidebarView() string {
	palette := paint.Panel

	query := palette.Cursor().Render("/") + " " + m.search.query
	if m.search.typing {
		query += palette.Cursor().Render("▏")
	}
	header := screenTitle(view.GlyphSearch, "Search") + "\n\n " + query + "\n\n"
	help := m.renderHelp(m.searchHelp(), header)

	agents := m.searchAgents()
	switch {
	case strings.TrimSpace(m.search.query) == "":
		return header + dimStyle.Render("type to search transcripts") + "\n" + help
	case len(agents) == 0:
		return header + dimStyle.Render("no matches") + "\n" + help
	}

	pane := m.paneWidth()
	window := view.Window(view.WindowParams{
		List:    view.Prepare(m.searchSnapshot(agents, pane)),
		Palette: palette,
		Width:   pane,
		Lines:   m.listLines(header, help),
	})

	return header + window + help
}

// searchScreen is the search screen behind the panel's one door.
type searchScreen struct{}

var _ columned = searchScreen{}

func (searchScreen) update(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	return onKey(m, msg, model.updateSearch)
}

func (searchScreen) view(m model) string {
	return m.searchView()
}

func (searchScreen) needs() screenNeeds {
	return screenNeeds{archive: true}
}

// reenter is the screen as it stands, nothing being drawn over the search.
func (searchScreen) reenter(m model) (tea.Model, tea.Cmd) {
	return m, nil
}

func (searchScreen) takesEveryKey(m model) bool {
	return m.search.typing
}

func (searchScreen) column() columnSpec {
	return searchColumn()
}
