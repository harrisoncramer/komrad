// check-resources holds the queries and the renderers to the resource option: every resource
// has a List<T>ByKey and a Snapshot<T> query under db/queries and a render.<T> under
// internal/rpc/render.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"weave/internal/resource"
)

const (
	queriesDir = "db/queries"
	renderDir  = "internal/rpc/render"
)

var (
	queryName = regexp.MustCompile(`(?m)^-- name: (\w+)`)
	funcName  = regexp.MustCompile(`(?m)^func (\w+)\(`)
)

// standIn is the pair of queries answering for a resource until they are renamed.
type standIn struct {
	byKey    string
	snapshot string
}

// standIns is each resource whose queries are still under their old names, dropped from here
// as each is renamed.
var standIns = map[string]standIn{
	"agents":    {byKey: "ListAgentRows", snapshot: "ListAgentRows"},
	"workflows": {byKey: "ListLiveWorkflowsByID", snapshot: "ListWorkflowsPage"},
	"schedules": {byKey: "ListLiveSchedulesByID", snapshot: "ListSchedulesPage"},
}

func main() {
	if err := run(".", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "check-resources:", err)
		os.Exit(1)
	}
}

func run(root string, out io.Writer) error {
	queries, err := names(filepath.Join(root, queriesDir), "*.sql", queryName)
	if err != nil {
		return err
	}

	renderers, err := names(filepath.Join(root, renderDir), "*.go", funcName)
	if err != nil {
		return err
	}

	var (
		problems []error
		notes    []string
		declared = map[string]bool{}
	)
	for _, r := range resource.All() {
		table := r.Option.GetTable()
		declared[table] = true
		byKey, snapshot := "List"+r.Plural()+"ByKey", "Snapshot"+r.Plural()

		stand, standing := standIns[table]
		if standing && (queries[byKey] || queries[snapshot]) {
			problems = append(problems, fmt.Errorf("%s has %s or %s now, so its stand-in is done: drop it from standIns in cmd/check-resources", table, byKey, snapshot))
		}

		for _, query := range [][2]string{{byKey, stand.byKey}, {snapshot, stand.snapshot}} {
			want, have := query[0], query[1]
			switch {
			case queries[want]:
			case standing && queries[have]:
				notes = append(notes, fmt.Sprintf("%s reads through %s until it is renamed %s", table, have, want))
			case standing:
				problems = append(problems, fmt.Errorf("%s has no %s under %s, and %s, which stands in until it is renamed, is gone too", table, want, queriesDir, have))
			default:
				problems = append(problems, fmt.Errorf("%s has no %s under %s", table, want, queriesDir))
			}
		}

		if !renderers[r.Name()] {
			problems = append(problems, fmt.Errorf("%s has no %s under %s", table, r.Name(), renderDir))
		}
	}

	for table := range standIns {
		if !declared[table] {
			problems = append(problems, fmt.Errorf("standIns in cmd/check-resources names %s, which no message declares as its table", table))
		}
	}

	if len(problems) > 0 {
		return errors.Join(problems...)
	}

	notes = append(notes, "every resource has its queries and its renderer")
	_, err = fmt.Fprintln(out, strings.Join(notes, "\n"))

	return err
}

// names is every name the pattern's first group matches across the files in dir.
func names(dir, glob string, pattern *regexp.Regexp) (map[string]bool, error) {
	paths, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no %s under %s", glob, dir)
	}

	found := map[string]bool{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		text, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		for _, match := range pattern.FindAllStringSubmatch(string(text), -1) {
			found[match[1]] = true
		}
	}

	return found, nil
}
