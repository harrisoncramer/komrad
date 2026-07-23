// Package search indexes the transcripts of tool-tracked chats and fuzzy-matches
// a query against their content, so a past chat can be found by what was said in
// it and then restored. It reads Claude Code's own transcript files (keyed by a
// chat's claude_session_id) rather than storing conversation text itself.
package search

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// bodyCap bounds how much transcript text is distilled per chat. The whole
// conversation is never held in memory. This much is plenty to match on and
// keeps building the index over every tracked chat fast.
const bodyCap = 6000

// snippetRadius is how many bytes of context to show on each side of a match.
const snippetRadius = 60

// DefaultProjectsDir is where Claude Code keeps its per-directory transcripts.
func DefaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".claude", "projects")
}

// Chat is the identity of one tracked chat the caller wants indexed. Name is the
// stable key the caller maps a result back to. ClaudeSessionID locates the
// transcript on disk.
type Chat struct {
	Name            string
	ClaudeSessionID string
	Deleted         bool
}

// Doc is one chat distilled into a searchable form: its identity plus enough
// transcript text to match against. text holds the name and title (up to
// headLen) followed by the transcript body, all lowercased for matching.
type Doc struct {
	Name            string
	ClaudeSessionID string
	Deleted         bool
	Title           string
	text            string // lowercased name+title+body, for matching
	display         string // same text in original case, for the snippet
	headLen         int
}

// Result is a matched Doc with its relevance score and a snippet of the
// transcript around the first matched term, so the user sees why it matched.
type Result struct {
	Doc
	Score   int
	Snippet string
}

// Index is a built, searchable set of distilled chats.
type Index struct {
	docs []Doc
}

// BuildIndex distills every chat whose transcript can be located under
// projectsDir into a searchable index. Chats without a locatable transcript are
// kept with only their title/name text, so they still match and restore.
func BuildIndex(chats []Chat, projectsDir string) Index {
	paths := locate(projectsDir)

	docs := make([]Doc, 0, len(chats))
	for _, chat := range chats {
		doc := Doc{Name: chat.Name, ClaudeSessionID: chat.ClaudeSessionID, Deleted: chat.Deleted}
		if path := paths[chat.ClaudeSessionID]; path != "" {
			distill(path, &doc)
		}

		// The name and title lead the match text so a term found there outranks the
		// same term buried in the body. headLen marks where that head ends.
		head := strings.TrimSpace(doc.Name + " " + doc.Title)
		doc.display = strings.TrimSpace(head + " " + doc.text)
		doc.text = strings.ToLower(doc.display)
		doc.headLen = len(head)
		docs = append(docs, doc)
	}

	return Index{docs: docs}
}

// headHitScore is added when a term is found in a chat's name or title. It is far
// larger than any body score so a name/title match always outranks a chat that
// only mentions the term in its transcript.
const headHitScore = 1_000_000

// Search returns the chats whose distilled text contains every whitespace-
// separated term of the query, case-insensitively, best match first. A blank
// query returns every indexed chat in the caller's original order, so the search
// screen can list them before anything is typed.
func (idx Index) Search(query string) []Result {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		results := make([]Result, len(idx.docs))
		for i, doc := range idx.docs {
			results[i] = Result{Doc: doc}
		}

		return results
	}

	results := make([]Result, 0, len(idx.docs))
	for order, doc := range idx.docs {
		score, first, ok := scoreDoc(doc, terms)
		if !ok {
			continue
		}

		// The input order is recency, so fold it in as a small tiebreaker that
		// keeps recent chats ahead when their content relevance is equal.
		results = append(results, Result{
			Doc:     doc,
			Score:   score - order,
			Snippet: snippet(doc.display, first),
		})
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	return results
}

// scoreDoc reports whether a doc contains every term and, if so, its relevance
// score and the byte offset of the earliest matched term (for the snippet). A
// term found in the name/title head is worth more, and an earlier match in the
// body scores slightly higher than a later one.
func scoreDoc(doc Doc, terms []string) (int, int, bool) {
	score := 0
	first := -1

	for _, term := range terms {
		at := strings.Index(doc.text, term)
		if at < 0 {
			return 0, 0, false
		}

		if at < doc.headLen {
			score += headHitScore
		} else {
			// A body hit scores a modest base plus an earlier-is-better bonus that
			// stays well below a head hit.
			score += 1000 + (len(doc.text) - at)
		}

		if first < 0 || at < first {
			first = at
		}
	}

	return score, first, true
}

// locate maps every transcript's claude_session_id (its filename stem) to its
// path in one cheap directory walk, so distillation is a direct lookup.
func locate(projectsDir string) map[string]string {
	paths := map[string]string{}
	if projectsDir == "" {
		return paths
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return paths
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		files, err := os.ReadDir(filepath.Join(projectsDir, entry.Name()))
		if err != nil {
			continue
		}

		for _, file := range files {
			name := file.Name()
			if !strings.HasSuffix(name, ".jsonl") {
				continue
			}

			id := strings.TrimSuffix(name, ".jsonl")
			paths[id] = filepath.Join(projectsDir, entry.Name(), name)
		}
	}

	return paths
}

// transcriptLine is the subset of a transcript record we read. message holds the
// user/assistant payload, decoded lazily so tool and metadata records are cheap
// to skip.
type transcriptLine struct {
	Type    string          `json:"type"`
	Title   string          `json:"title"`
	Prompt  string          `json:"prompt"`
	Content string          `json:"content"`
	Message json.RawMessage `json:"message"`
}

// distill fills a doc's title and text from one transcript, reading only until
// the body cap is reached. It keeps the AI-generated title, the first user
// prompt, and the running message text, which is what a content search matches.
func distill(path string, doc *Doc) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	var body strings.Builder
	var firstPrompt string

	for {
		raw, err := reader.ReadBytes('\n')
		if len(raw) > 0 {
			var line transcriptLine
			if json.Unmarshal(raw, &line) == nil {
				switch line.Type {
				case "ai-title":
					if line.Title != "" {
						doc.Title = line.Title
					}
				case "user", "assistant":
					text := messageText(line.Message)
					if text != "" {
						if line.Type == "user" && firstPrompt == "" {
							firstPrompt = text
						}

						body.WriteString(text)
						body.WriteByte(' ')
					}
				}
			}
		}

		if body.Len() >= bodyCap || err != nil {
			break
		}
	}

	text := firstPrompt + " " + body.String()
	if len(text) > bodyCap {
		text = text[:bodyCap]
	}

	doc.text = text
}

// messageText pulls the plain text out of a user/assistant record, whose content
// is either a bare string or a list of typed blocks. Only text blocks contribute.
// Thinking, tool calls, and tool results are ignored.
func messageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var envelope struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Content) == 0 {
		return ""
	}

	var str string
	if json.Unmarshal(envelope.Content, &str) == nil {
		return str
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(envelope.Content, &blocks) != nil {
		return ""
	}

	var out strings.Builder
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			out.WriteString(block.Text)
			out.WriteByte(' ')
		}
	}

	return out.String()
}

// snippet returns a one-line window of text around the matched byte offset,
// collapsed to single spaces so it fits on a result row. A negative offset
// yields the start of the text, for a blank-query listing.
func snippet(text string, at int) string {
	if at < 0 {
		return oneLine(text, 0, min(len(text), 2*snippetRadius))
	}

	start := at - snippetRadius
	if start < 0 {
		start = 0
	}

	end := at + snippetRadius
	if end > len(text) {
		end = len(text)
	}

	prefix := ""
	if start > 0 {
		prefix = "…"
	}

	suffix := ""
	if end < len(text) {
		suffix = "…"
	}

	return prefix + oneLine(text, start, end) + suffix
}

// oneLine trims text[start:end] to a single spaced line, stepping the bounds to
// valid rune starts so a byte window never splits a character.
func oneLine(text string, start, end int) string {
	for start < end && !utf8Start(text[start]) {
		start++
	}
	for end < len(text) && !utf8Start(text[end]) {
		end++
	}

	return strings.Join(strings.Fields(text[start:end]), " ")
}

// utf8Start reports whether b begins a UTF-8 rune (not a continuation byte).
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
