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

	var problems []error
	for _, r := range resource.All() {
		table := r.Option.GetTable()
		for _, query := range []string{"List" + r.Plural() + "ByKey", "Snapshot" + r.Plural()} {
			if !queries[query] {
				problems = append(problems, fmt.Errorf("%s has no %s under %s", table, query, queriesDir))
			}
		}

		if !renderers[r.Name()] {
			problems = append(problems, fmt.Errorf("%s has no %s under %s", table, r.Name(), renderDir))
		}
	}

	if len(problems) > 0 {
		return errors.Join(problems...)
	}

	_, err = fmt.Fprintln(out, "every resource has its queries and its renderer")

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
