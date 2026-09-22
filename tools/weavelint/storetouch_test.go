package weavelint

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// testListings is the listings the testdata queries read: agents and marks through the agent
// listing, workflows and their hooks through the workflow one.
var testListings = map[string][]string{
	"agents":    {"ListAgentRows"},
	"workflows": {"ListWorkflowRows"},
}

// TestStoreTouch covers a store over the tables the listings read: a write followed by a touch
// naming a listing that reads the table passes, through the handle or through a helper named
// touch, a write with none, with the touch before it, or with a touch naming a listing that
// does not read the table is a finding, a table two listings read is settled by a touch of
// either, a write through a CTE is read through to the table it changes, a query built from
// models.New is a query all the same, and a read locked FOR UPDATE, a conflict arm's DO UPDATE
// and a write to a table no listing reads are not writes the rule is about.
func TestStoreTouch(t *testing.T) {
	analyzer, err := newStoreTouch(config.StoreTouchConfig{
		Stores:   []string{`internal/store/`},
		Queries:  filepath.Join(analysistest.TestData(), "src", "weave", "db", "queries"),
		Listings: testListings,
	})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/store/agent")
}

// TestStoreTouchOutsideStores covers a package the store patterns leave alone.
func TestStoreTouchOutsideStores(t *testing.T) {
	analyzer, err := newStoreTouch(config.StoreTouchConfig{
		Stores:   []string{`internal/store/agent/`},
		Queries:  filepath.Join(analysistest.TestData(), "src", "weave", "db", "queries"),
		Listings: testListings,
	})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/store/quiet")
}

// TestStoreTouchSkip covers the rule turned off, which reports nothing anywhere.
func TestStoreTouchSkip(t *testing.T) {
	analyzer, err := newStoreTouch(config.StoreTouchConfig{Skip: true, Stores: []string{`internal/store/`}, Listings: testListings})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/store/quiet")
}

// TestReadTables covers the tables a listing query reads: FROM and JOIN name them, a CTE and a
// subquery do not, a table read twice is named once, and one glued to a closing paren is read.
func TestReadTables(t *testing.T) {
	loaded, err := readQueries(filepath.Join(analysistest.TestData(), "src", "weave", "db", "queries"), testListings)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][]string{
		"agents":         {"agents"},
		"marks":          {"agents", "workflows"},
		"schedules":      {"workflows"},
		"workflows":      {"workflows"},
		"workflow_hooks": {"workflows"},
	}
	if !maps.EqualFunc(loaded.watched, want, slices.Equal) {
		t.Fatalf("watched tables: got %v, want %v", loaded.watched, want)
	}
}

// TestListingNamingAMissingQueryIsRefused covers a listing naming a query the files do not
// hold, which is refused rather than watched as nothing.
func TestListingNamingAMissingQueryIsRefused(t *testing.T) {
	_, err := readQueries(filepath.Join(analysistest.TestData(), "src", "weave", "db", "queries"), map[string][]string{"agents": {"ListNothing"}})
	if err == nil || !strings.Contains(err.Error(), `names query "ListNothing"`) {
		t.Fatalf("got %v, want a refusal naming the query", err)
	}
}

// TestWatchedTablesOfTheRepository prints the tables the repository's own listings read, which
// is the set a store write has to touch after, so a change to a listing query shows up here.
func TestWatchedTablesOfTheRepository(t *testing.T) {
	loaded, err := readQueries(filepath.Join("..", "..", "db", "queries"), map[string][]string{
		"agents":    {"ListAgentRows", "ListAgentMetadata"},
		"workflows": {"ListWorkflowsPage", "ListLiveWorkflowsByID", "ListHooksForWorkflows", "ListMarks"},
		"schedules": {"ListSchedulesPage", "ListLiveSchedulesByID"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, table := range slices.Sorted(maps.Keys(loaded.watched)) {
		t.Logf("%-30s read by %v", table, loaded.watched[table])
	}

	want := map[string][]string{
		"agent_metadata":               {"agents"},
		"agent_runs":                   {"agents"},
		"agents":                       {"agents"},
		"mail":                         {"agents"},
		"marks":                        {"agents", "workflows"},
		"pull_request_syncs":           {"agents"},
		"pull_requests":                {"agents"},
		"review_rounds":                {"agents"},
		"reviews":                      {"agents"},
		"schedules":                    {"schedules"},
		"workflow_configs":             {"workflows"},
		"workflow_hooks":               {"workflows"},
		"workflow_provisioner_configs": {"agents", "workflows"},
		"workflows":                    {"agents", "workflows"},
	}
	if !maps.EqualFunc(loaded.watched, want, slices.Equal) {
		t.Fatalf("watched tables: got %v, want %v", loaded.watched, want)
	}
}
