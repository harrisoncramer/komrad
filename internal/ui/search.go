package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"komrad/config"
	"komrad/internal/client"
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/searchsyntax"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	paginationv1 "komrad/proto/gen/pagination/v1"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// searchScreen is the search screen: the query being typed, the hits the server found for it,
// the cursor in them, and the transcript drawn beside them.
type searchScreen struct {
	rowCursor
	typing bool
	query  string
	// turns are the hits the server found in the conversations, one per turn that said the query.
	turns []searchHit
	// drawn is the rows built once for the frame being drawn, and nil between frames.
	drawn   []searchRow
	next    *string
	paging  bool
	problem searchProblem
	// preview is the transcript of the agent the cursor is standing on, drawn beside the list.
	preview searchPreview

	previewFocused bool
}

func (w *searchScreen) list() screenList[searchRow] {
	return screenList[searchRow]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.rows,
		moved:  func(h host, _ panelShared) tea.Cmd { return h.waitToStage() },
		quit:   func(config.Keybindings) []string { return []string{"ctrl+c"} },
	}
}

func (w *searchScreen) landOnRow(_ host, s panelShared) tea.Cmd {
	row, ok := w.hovered(s)
	if !ok {
		return nil
	}

	var cmds []tea.Cmd
	if w.preview.agentID == row.agent.id && w.preview.conversation != "" {
		w.openPreviewOn(s, row.open(w.preview.conversation))
	} else {
		cmds = append(cmds, w.previewCmd(s, row.agent))
	}

	if w.cursor == len(w.rows(s))-1 && w.next != nil && !w.paging {
		w.paging = true
		cmds = append(cmds, w.searchCmd(s, w.query, w.next))
	}

	return tea.Batch(cmds...)
}

// actions is the keys of whichever of the list and the transcript holds them.
func (w *searchScreen) actions(s panelShared) screenActionSpec {
	if w.isBlockFocused(s) {
		return w.previewActions()
	}

	return w.listActions()
}

// listActions is the search screen as one table once the query line has given the keys up.
func (w *searchScreen) listActions() screenActionSpec {
	list := w.list()
	shared := slices.DeleteFunc(list.actions(), func(a screenAction) bool { return a.id == actionColumnScroll })

	return screenActionSpec{
		table: append(shared,
			screenAction{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				run:  func(_ host, s panelShared, pressed string) tea.Cmd { return w.scrollPreview(s, pressed) },
			},
			screenAction{
				id:   actionSearchEdit,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run: func(host, panelShared, string) tea.Cmd {
					w.typing = true

					return nil
				},
			},
			screenAction{
				id:   actionSearchBack,
				keys: func(config.Keybindings) []string { return []string{"esc"} },
				run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.pickView() },
			},
			screenAction{
				id:   actionSearchOpen,
				keys: func(config.Keybindings) []string { return []string{"enter"} },
				run:  func(h host, s panelShared, _ string) tea.Cmd { return w.jumpToAgent(h, s) },
			},
		),
		legend: slices.Concat([]screenLegendRow{
			{
				of:    []actionID{actionSearchOpen},
				desc:  func(panelShared) string { return "go to it" },
				strip: true,
			},
			{
				of:    []actionID{actionSearchEdit},
				desc:  func(panelShared) string { return "edit the query" },
				strip: true,
			},
		}, screenNavLegend(), []screenLegendRow{
			{
				of:   []actionID{actionSearchBack},
				desc: func(panelShared) string { return "back" },
			},
		}),
	}
}

const searchLimit = 10

const searchDebounce = 120 * time.Millisecond

type searchHit struct {
	agentID  string
	name     string
	archived bool
	seq      int32
	role     string
	offset   int64
	length   int64
	snippet  string
}

func (h searchHit) lead() string {
	speaker := speakerOf(h.role)
	if speaker == "" {
		return fmt.Sprintf("#%d", h.seq)
	}

	return fmt.Sprintf("%s #%d", speaker, h.seq)
}

type searchRow struct {
	agent searchAgent
	hit   *searchHit
}

func (r searchRow) key() string {
	if r.hit == nil {
		return r.agent.id
	}

	return fmt.Sprintf("%s/%d", r.agent.id, r.hit.seq)
}

func (r searchRow) open(content string) int {
	if r.hit == nil {
		return 0
	}

	own := r.hit.spans(content)
	if len(own) == 0 {
		return 0
	}

	return max(slices.IndexFunc(r.agent.spans(content), func(span paint.Span) bool {
		return span.Start == own[0].Start
	}), 0)
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
	query   string
	hits    []searchHit
	next    *string
	paged   bool
	problem searchProblem
}

type searchProblem struct {
	reason  string
	refused bool
}

type previewResult struct {
	agentID string
	query   string
	content string
}

// enter opens the search fresh, with the query line waiting for the filter key.
func (w *searchScreen) enter(h host, _ panelShared, target screenTarget) tea.Cmd {
	if target.resume {
		return nil
	}

	*w = searchScreen{}

	return nil
}

func debounceSearchCmd(query string) tea.Cmd {
	if strings.TrimSpace(query) == "" {
		return func() tea.Msg { return searchResult{query: query} }
	}

	return startTimer(searchDebounce, func(time.Time) tea.Msg {
		return searchSettledMsg{query: query}
	})
}

func (w *searchScreen) runSettled(s panelShared, msg searchSettledMsg) tea.Cmd {
	if msg.query != w.query {
		return nil
	}

	return w.searchCmd(s, msg.query, nil)
}

func (w *searchScreen) searchCmd(s panelShared, query string, cursor *string) tea.Cmd {
	client := s.api.agents

	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return searchResult{query: query}
		}

		page, err := call(agentsTimeout, func(ctx context.Context) (searchResult, error) {
			resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
				Query: query,
				Pagination: &paginationv1.Pagination{
					Limit:  xptr.Ptr[int32](searchLimit),
					Cursor: cursor,
				},
			}))
			if err != nil {
				return searchResult{}, err
			}

			// The turns that say the words stand before the ones that only carry the letters.
			found := slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear())

			return searchResult{
				query: query,
				hits:  xslice.Map(found, searchHitFrom),
				next:  resp.Msg.NextPageToken,
				paged: cursor != nil,
			}, nil
		})
		if err != nil {
			return searchResult{query: query, paged: cursor != nil, problem: searchProblem{
				reason:  errui.GetReason(err),
				refused: connect.CodeOf(err) == connect.CodeInvalidArgument,
			}}
		}

		return page
	}
}

func searchHitFrom(h *agentv1.SearchHit) searchHit {
	return searchHit{
		agentID:  h.GetAgentId(),
		name:     h.GetName(),
		archived: h.GetArchived(),
		seq:      h.GetSeq(),
		role:     h.GetRole(),
		offset:   h.GetContentOffset(),
		length:   h.GetContentLength(),
		snippet:  h.GetSnippet(),
	}
}

// place puts the cursor back on the row it was standing on, and fetches a preview where that
// row has gone.
func (w *searchScreen) place(s panelShared, was string) tea.Cmd {
	rows := w.rows(s)
	if len(rows) == 0 {
		w.cursor = 0
		w.preview = searchPreview{}

		return nil
	}

	at := rowIndexOfKey(rows, was)
	w.cursor = cursor.Place(at, w.cursor, len(rows))
	if at >= 0 {
		return nil
	}

	return w.previewCmd(s, rows[w.cursor].agent)
}

// rows is every agent the search found, once each, the named ones first, with the turns of its
// conversation that said the query under it.
func (w *searchScreen) rows(s panelShared) []searchRow {
	if w.drawn != nil {
		return w.drawn
	}

	return w.buildRows(s)
}

func (w *searchScreen) buildRows(s panelShared) []searchRow {
	var rows []searchRow
	for _, agent := range w.buildAgents(s) {
		rows = append(rows, searchRow{agent: agent})
		for i := range agent.turns {
			rows = append(rows, searchRow{agent: agent, hit: &agent.turns[i]})
		}
	}

	return rows
}

func (w *searchScreen) buildAgents(s panelShared) []searchAgent {
	query := strings.TrimSpace(w.query)
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

	for _, item := range s.listing.agents {
		if doesNameSay(item.Agent.Name, query) {
			add(searchAgent{id: item.Agent.ID, name: item.Agent.Name})
		}
	}
	for _, item := range s.listing.archivedAgents {
		if doesNameSay(item.Agent.Name, query) {
			add(searchAgent{
				id:       item.Agent.ID,
				name:     item.Agent.Name,
				archived: true,
			})
		}
	}
	for _, hit := range w.turns {
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

func (w *searchScreen) apply(h host, s panelShared, result searchResult) tea.Cmd {
	if result.query != w.query {
		return nil
	}

	w.problem = result.problem
	if result.problem.reason != "" {
		w.paging = false

		return nil
	}

	if result.paged {
		was := w.list().hovered(s)
		w.paging = false
		w.turns = append(w.turns, result.hits...)
		w.next = result.next
		w.drawn = nil

		rows := w.rows(s)
		w.cursor = cursor.Place(rowIndexOfKey(rows, was), w.cursor, len(rows))

		return w.landOnRow(h, s)
	}

	w.turns = result.hits
	w.next = result.next
	w.paging = false
	w.drawn = nil

	rows := w.rows(s)
	w.cursor = cursor.Clamp(w.cursor, len(rows))
	if len(rows) == 0 {
		w.preview = searchPreview{}

		return nil
	}

	return w.previewCmd(s, rows[w.cursor].agent)
}

func (w *searchScreen) previewCmd(s panelShared, agent searchAgent) tea.Cmd {
	agents := s.api.agents
	query := w.query

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

func (w *searchScreen) applyPreview(s panelShared, result previewResult) {
	if result.query != w.query {
		return
	}

	row, ok := w.hovered(s)
	if !ok || row.agent.id != result.agentID {
		return
	}

	w.holdPreview(s, holdPreviewParams{
		agentID:      row.agent.id,
		conversation: result.content,
		spans:        row.agent.spans(result.content),
		open:         row.open(result.content),
	})
}

const noTranscriptMarkdown = "_no transcript yet_"

// The three things the column beside the list says before it has a transcript to draw.
const (
	searchWaitingMarkdown = "_the conversation the cursor is standing on is read here_"
	noSearchHitsMarkdown  = "_nothing said that_"
	searchLoadingMarkdown = "_reading the conversation…_"
)

// update takes the search's own results and, once the query line has given them up, the keys.
func (w *searchScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case searchSettledMsg:
		return w.runSettled(s, msg)
	case searchResult:
		return w.apply(h, s, msg)
	case previewResult:
		w.applyPreview(s, msg)

		return nil
	case tea.PasteMsg:
		if !w.typing {
			return nil
		}

		w.query += pastedLine(msg)

		return debounceSearchCmd(w.query)
	case tea.KeyPressMsg:
		if w.typing {
			return w.updateInput(h, s, msg)
		}

		if w.isBlockFocused(s) {
			return w.previewActions().dispatch(h, s, msg.String())
		}

		return w.list().update(h, s, msg, w.listActions())
	}

	return nil
}

func (w *searchScreen) updateInput(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	passes := slices.Concat(s.keys.Nav.PageUp, s.keys.Nav.PageDown, s.keys.Column.ScrollDown, s.keys.Column.ScrollUp)
	if typedText(key) == "" && isBound(key.String(), passes) {
		return w.listActions().dispatch(h, s, key.String())
	}

	switch k := key.String(); {
	case k == "esc":
		w.typing = false

		return nil
	case k == "enter":
		w.typing = false

		return nil
	case k == "ctrl+c":
		return h.hide()
	case k == "backspace":
		if w.query != "" {
			w.query = paint.TrimLastRune(w.query)
		}

		return debounceSearchCmd(w.query)
	case typedText(key) != "":
		w.query += typedText(key)

		return debounceSearchCmd(w.query)
	}

	return nil
}

func (w *searchScreen) hovered(s panelShared) (searchRow, bool) {
	return rowAt(w.rows(s), w.cursor)
}

func (w *searchScreen) hoveredName(s panelShared) string {
	if w.typing {
		return ""
	}

	agent, ok := w.hoveredAgent(s)
	if !ok {
		return ""
	}

	return agent.name
}

func (w *searchScreen) hoveredAgent(s panelShared) (searchAgent, bool) {
	row, ok := w.hovered(s)

	return row.agent, ok
}

// jumpToAgent lands the agents list or the archive on the hovered row's agent.
func (w *searchScreen) jumpToAgent(h host, s panelShared) tea.Cmd {
	agent, ok := w.hoveredAgent(s)
	if !ok {
		return nil
	}

	*w = searchScreen{}

	target := screenTarget{agentID: agent.id}
	if agent.archived {
		return h.enter(screenArchive, target)
	}

	return h.enter(screenAgents, target)
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

// agentRow reads one agent the search found into the row the agents list draws.
func (w *searchScreen) agentRow(s panelShared, agent searchAgent, archived map[string]panel.AgentRow) view.Row {
	row := view.Row{
		Key:     agent.id,
		Kind:    view.KindResult,
		Name:    agent.name,
		Matches: nameMatches(agent.name, w.query),
		Process: view.ProcessNone,
	}

	if agent.archived {
		return archivedSearchRow(row, agent.id, archived)
	}

	item, ok := s.listing.liveAgent(agent.id)
	if !ok {
		return row
	}

	return agentFacts(row, item)
}

func searchHitRow(row searchRow) view.Row {
	text, matches := flattenSnippet(row.hit.snippet)

	return view.Row{
		Key:      row.key(),
		Kind:     view.KindHit,
		Lead:     row.hit.lead(),
		Name:     text,
		Matches:  matches,
		Process:  view.ProcessNone,
		Archived: row.agent.archived,
	}
}

func flattenSnippet(snippet string) (string, []view.Span) {
	var (
		text    strings.Builder
		matches []view.Span
		open    = -1
		space   = true
	)
	for _, r := range snippet {
		switch {
		case r == rune(paint.MarkOpen[0]):
			open = text.Len()
		case r == rune(paint.MarkClose[0]):
			if open >= 0 && text.Len() > open {
				matches = append(matches, view.Span{Start: open, End: text.Len()})
			}
			open = -1
		case unicode.IsSpace(r):
			if !space {
				text.WriteRune(' ')
			}
			space = true
		default:
			text.WriteRune(r)
			space = false
		}
	}

	return strings.TrimSpace(text.String()), matches
}

// archivedSearchRow is a result on an archived agent.
func archivedSearchRow(row view.Row, id string, archived map[string]panel.AgentRow) view.Row {
	row.Process = view.ProcessStopped

	if item, ok := archived[id]; ok {
		row = agentFacts(row, item)
	}

	row.Archived = true

	return row
}

// snapshot is the rows the search found, as the list draws them.
func (w *searchScreen) snapshot(s panelShared, rows []searchRow, pane listPane) view.Snapshot {
	archived := archivedByID(s.listing.archivedAgents)
	caret := pane.caret
	if w.isBlockFocused(s) {
		caret = view.CaretDrawn
	}

	return view.Snapshot{
		Rows: xslice.Map(rows, func(row searchRow) view.Row {
			if row.hit != nil {
				return searchHitRow(row)
			}

			return w.agentRow(s, row.agent, archived)
		}),
		Columns:    view.NoColumns(),
		Caret:      caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, w.cursor),
		Pane:       pane.width,
	}
}

// view draws the agents the search found beside the transcript of the hovered one.
func (w *searchScreen) view(s panelShared) string {
	w.drawn = w.buildRows(s)
	defer func() { w.drawn = nil }()

	sidebar := w.sidebarView(s)

	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return sidebar
	}

	return s.draw.joinColumns(split, sidebar, w.previewView(s, split))
}

// sidebarView is the panel's own column: the query and the agents it found.
func (w *searchScreen) sidebarView(s panelShared) string {
	palette := paint.Panel

	if w.typing {
		palette = s.draw.focusPalette()
	}

	query := palette.Cursor().Render("/") + " " + w.query
	if w.typing {
		query += palette.Cursor().Render(glyph.TextCursor)
	}
	header := s.draw.viewsBar() + query + "\n\n"

	blank := strings.TrimSpace(w.query) == ""
	var qualifiers string
	if blank || s.chrome.helpExpanded {
		qualifiers = qualifierLegend(s.pane)
	}
	help := s.draw.renderHelp(w.help(s), header, qualifiers)
	if !blank {
		help = qualifiers + help
	}
	below := help
	if qualifiers != "" {
		below = "\n" + help
	}

	if w.problem.reason != "" {
		tone := paint.ToneDanger
		if w.problem.refused {
			tone = paint.ToneWarning
		}
		header += palette.Style(tone).Width(s.pane).Render(w.problem.reason) + "\n\n"
	}

	rows := w.rows(s)
	switch {
	case blank:
		return header + qualifiers + help
	case len(rows) == 0 && w.problem.reason != "":
		return header + help
	case len(rows) == 0:
		return header + dimStyle().Render("no matches") + "\n" + below
	}

	window := view.Window(view.WindowParams{
		List:    prepareList(s.glyphs, w.snapshot(s, rows, s.draw.listPane(s.pane))),
		Palette: palette,
		Width:   s.pane,
		Lines:   s.draw.listLines(header, below),
	})

	return header + window + below
}

// qualifierLegend lays out the qualifiers a search takes the way the legend lays out keys.
func qualifierLegend(width int) string {
	rows := xslice.Map(searchsyntax.Qualifiers, func(qualifier searchsyntax.Qualifier) key.Binding {
		return key.NewBinding(key.WithKeys(qualifier.Syntax), key.WithHelp(qualifier.Syntax, qualifier.Meaning))
	})

	return fitLegend(helpView(0).FullHelpView([][]key.Binding{rows}), width) + "\n\n"
}

// help is the search's legend: the query line's two keys while it is typed at, and the list's
// own once it has given the keys up.
func (w *searchScreen) help(s panelShared) keyMap {
	if w.typing {
		done := binding([]string{"enter"}, "keep it, then move through the hits")
		leave := binding([]string{"esc"}, "leave the query line")

		return keyMap{short: []key.Binding{done, leave}, full: []key.Binding{done, leave}}
	}

	own := w.actions(s).help(s)

	return keyMap{
		short: slices.Concat(own.short, s.draw.globalHelp()),
		full:  slices.Concat(own.full, s.draw.fullGlobalHelp()),
	}
}

var (
	_ columned = (*searchScreen)(nil)
	_ blocked  = (*searchScreen)(nil)
)

func (w *searchScreen) doesDrawFromListing() bool {
	return false
}

func (w *searchScreen) takesEveryKey() bool {
	return w.typing
}

func (w *searchScreen) column() columnSpec {
	return searchColumn()
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
