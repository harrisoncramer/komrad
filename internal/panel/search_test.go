package panel

import (
	"strings"
	"testing"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

func hits(names ...string) []searchHit {
	out := make([]searchHit, 0, len(names))
	for _, n := range names {
		out = append(out, searchHit{sessionID: n, name: n, snippet: n})
	}
	return out
}

// TestApplySearchResultsDiscardsStale confirms a response for a query the input
// has since moved past is ignored, so the list never shows hits for text the
// user already changed.
func TestApplySearchResultsDiscardsStale(t *testing.T) {
	m := model{screen: screenSearch, searchQuery: "current"}
	m.applySearchResults(searchResultsMsg{query: "stale", hits: hits("a", "b")})

	if m.searchHits != nil {
		t.Fatalf("stale results should be discarded, got %d hits", len(m.searchHits))
	}
}

// TestUpdateSearchEditsQuery confirms printable keys, space, and backspace edit
// the query. It avoids the nav and open keys, which stage a preview through the
// manager.
func TestUpdateSearchEditsQuery(t *testing.T) {
	m := model{screen: screenSearch, keys: appconfig.DefaultKeybindings()}

	mi, _ := m.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("po")})
	m = mi.(model)
	mi, _ = m.updateSearch(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(model)
	mi, _ = m.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gr")})
	m = mi.(model)

	if m.searchQuery != "po gr" {
		t.Fatalf("want query %q, got %q", "po gr", m.searchQuery)
	}

	mi, _ = m.updateSearch(tea.KeyMsg{Type: tea.KeyBackspace})
	m = mi.(model)
	if m.searchQuery != "po g" {
		t.Fatalf("backspace: want %q, got %q", "po g", m.searchQuery)
	}
}

func TestTrimLastRune(t *testing.T) {
	if got := trimLastRune("abc"); got != "ab" {
		t.Fatalf("want ab, got %q", got)
	}
	// A multi-byte rune is dropped whole, not left half-deleted.
	if got := trimLastRune("aé"); got != "a" {
		t.Fatalf("want a, got %q", got)
	}
	if got := trimLastRune(""); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRenderSnippetHighlightsMatch(t *testing.T) {
	in := "before " + store.SnippetOpen + "match" + store.SnippetClose + " after\nnext"
	out := renderSnippet(in)

	if strings.Contains(out, store.SnippetOpen) || strings.Contains(out, store.SnippetClose) {
		t.Fatalf("markers should be consumed, got %q", out)
	}
	if !strings.Contains(out, "match") {
		t.Fatalf("matched text should survive, got %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("newlines should be collapsed to keep the snippet on one line, got %q", out)
	}
}

func TestFirstMatchLine(t *testing.T) {
	content := "line one\nline two has postgres in it\nline three"

	if got := firstMatchLine(content, "postgres"); got != 2 {
		t.Fatalf("want match on line 2, got %d", got)
	}
	// Case-insensitive, since the search itself is.
	if got := firstMatchLine(content, "POSTGRES"); got != 2 {
		t.Fatalf("case-insensitive: want 2, got %d", got)
	}
	// No occurrence anchors the preview at the top.
	if got := firstMatchLine(content, "sqlite"); got != 0 {
		t.Fatalf("want 0 for no match, got %d", got)
	}
	if got := firstMatchLine(content, "  "); got != 0 {
		t.Fatalf("want 0 for blank query, got %d", got)
	}
}
