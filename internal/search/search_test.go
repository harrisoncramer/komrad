package search

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTranscript writes a minimal transcript file for cid under a project dir
// so the locator finds it by filename stem.
func writeTranscript(t *testing.T, projectsDir, project, cid string, lines []string) {
	t.Helper()

	dir := filepath.Join(projectsDir, project)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	var body string
	for _, line := range lines {
		body += line + "\n"
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, cid+".jsonl"), []byte(body), 0o644))
}

func TestSearch(t *testing.T) {
	projectsDir := t.TempDir()

	writeTranscript(t, projectsDir, "proj-a", "cid-hooks", []string{
		`{"type":"ai-title","title":"Refine hooks"}`,
		`{"type":"user","message":{"role":"user","content":"help me refine the hooks concept"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"the hook fires on stop"}]}}`,
	})
	writeTranscript(t, projectsDir, "proj-b", "cid-sql", []string{
		`{"type":"user","message":{"role":"user","content":"add a fulltext index to postgres"}}`,
	})

	chats := []Chat{
		{
			Name:            "hooks",
			ClaudeSessionID: "cid-hooks",
		},
		{
			Name:            "db-work",
			ClaudeSessionID: "cid-sql",
			Deleted:         true,
		},
		{
			Name:            "no-transcript",
			ClaudeSessionID: "cid-missing",
		},
	}

	idx := BuildIndex(chats, projectsDir)

	// Matching requires every term as a substring, case-insensitively, so counts
	// are exact and the best content match ranks first.
	tests := []struct {
		name      string
		query     string
		wantFirst string
		wantLen   int
	}{
		{
			name:      "all terms must appear, matching one chat by its body",
			query:     "hook fires",
			wantFirst: "hooks",
			wantLen:   1,
		},
		{
			name:      "matches a soft-deleted chat by its transcript",
			query:     "postgres",
			wantFirst: "db-work",
			wantLen:   1,
		},
		{
			name:      "matches the ai-title even without transcript body",
			query:     "refine hooks",
			wantFirst: "hooks",
			wantLen:   1,
		},
		{
			name:      "thinking blocks are not indexed",
			query:     "secret",
			wantFirst: "",
			wantLen:   0,
		},
		{
			name:      "a term present in no chat matches nothing",
			query:     "hook zzzznope",
			wantFirst: "",
			wantLen:   0,
		},
		{
			name:      "blank query returns every chat",
			query:     "",
			wantFirst: "",
			wantLen:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := idx.Search(tt.query)
			assert.Len(t, results, tt.wantLen)
			if tt.wantFirst != "" {
				require.NotEmpty(t, results)
				assert.Equal(t, tt.wantFirst, results[0].Name)
			}
		})
	}
}

func TestSearchRanksNameOverBody(t *testing.T) {
	projectsDir := t.TempDir()
	// One chat is named "hooks", another only mentions hooks deep in its body.
	writeTranscript(t, projectsDir, "p1", "cid-named", []string{
		`{"type":"user","message":{"role":"user","content":"help me here"}}`,
	})
	writeTranscript(t, projectsDir, "p2", "cid-body", []string{
		`{"type":"user","message":{"role":"user","content":"unrelated preamble then hooks appear later"}}`,
	})

	idx := BuildIndex([]Chat{
		{
			Name:            "body-mention",
			ClaudeSessionID: "cid-body",
		},
		{
			Name:            "hooks",
			ClaudeSessionID: "cid-named",
		},
	}, projectsDir)

	results := idx.Search("hooks")
	require.Len(t, results, 2)
	assert.Equal(t, "hooks", results[0].Name)
}

func TestSearchSnippetShowsContext(t *testing.T) {
	projectsDir := t.TempDir()
	writeTranscript(t, projectsDir, "proj", "cid", []string{
		`{"type":"user","message":{"role":"user","content":"the quick brown fox jumps over the lazy dog"}}`,
	})

	idx := BuildIndex([]Chat{{Name: "c", ClaudeSessionID: "cid"}}, projectsDir)
	results := idx.Search("brown fox")

	require.Len(t, results, 1)
	assert.Contains(t, results[0].Snippet, "brown fox")
}

func TestDeletedFlagCarriesThrough(t *testing.T) {
	projectsDir := t.TempDir()
	writeTranscript(t, projectsDir, "proj", "cid", []string{
		`{"type":"user","message":{"role":"user","content":"anything"}}`,
	})

	idx := BuildIndex([]Chat{{Name: "c", ClaudeSessionID: "cid", Deleted: true}}, projectsDir)
	results := idx.Search("anything")

	require.Len(t, results, 1)
	assert.True(t, results[0].Deleted)
}
