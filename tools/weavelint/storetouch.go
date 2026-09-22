package weavelint

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const storeTouchDoc = "Checks that a store function which writes a table the panel's listings read " +
	"touches the watch afterwards, so a row cannot change without the panel being told."

// modelsPackage is where sqlc writes the query methods.
const modelsPackage = "weave/db/models"

// queriesType is the type every sqlc query is a method of.
const queriesType = "Queries"

// touchPrefix opens the name of every call that says a row moved, the three touches on the
// handle and a store's own helper that works out which rows to name.
const touchPrefix = "touch"

// queryHeader is the line sqlc reads a query's name off.
var queryHeader = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)\s+:\w+`)

// newStoreTouch builds the rule that a write to a watched table is followed by a touch.
func newStoreTouch(settings config.StoreTouchConfig) (*analysis.Analyzer, error) {
	stores, err := compile(settings.Stores)
	if err != nil {
		return nil, err
	}

	queries := &queryFiles{dir: settings.Queries, listings: settings.Listings}

	return &analysis.Analyzer{
		Name: "store_touch",
		Doc:  storeTouchDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || !isUnder(pass, file, stores) {
					continue
				}

				loaded, err := queries.load()
				if err != nil {
					return nil, err
				}

				reportUntouched(pass, file, loaded.writes, loaded.watched)
			}

			return nil, nil
		},
	}, nil
}

// reportUntouched reports every function that calls a query writing a table a listing reads
// with no touch of a listing that reads it after the write.
func reportUntouched(pass *analysis.Pass, file *ast.File, writes map[string][]string, watched map[string][]string) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}

		var touches []touch
		var firstWrite *ast.CallExpr
		var firstTable string
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			if isTouch(call) {
				touches = append(touches, touch{pos: call.Pos(), listing: touchedListing(pass, call)})

				return true
			}

			if firstWrite != nil {
				return true
			}

			query, ok := queryName(pass, call)
			if !ok {
				return true
			}

			for _, table := range writes[query] {
				if _, ok := watched[table]; ok {
					firstWrite, firstTable = call, table

					return true
				}
			}

			return true
		})

		if firstWrite == nil || isTouchedAfter(touches, firstWrite.Pos(), watched[firstTable]) {
			continue
		}

		pass.Reportf(function.Pos(),
			"%s writes %s and never touches the %s listing afterwards: the rows it changed stay stale on the panel",
			function.Name.Name, firstTable, strings.Join(watched[firstTable], " or "))
	}
}

// touch is one call named as a touch and the listing it names, empty for a helper that names
// none.
type touch struct {
	pos     token.Pos
	listing string
}

// isTouchedAfter says whether a touch after the position names one of the listings, or names
// none.
func isTouchedAfter(touches []touch, after token.Pos, listings []string) bool {
	return slices.ContainsFunc(touches, func(t touch) bool {
		return t.pos > after && (t.listing == "" || slices.Contains(listings, t.listing))
	})
}

// touchedListing is the listing a touch names in its first argument, when that is a constant
// string, and empty otherwise.
func touchedListing(pass *analysis.Pass, call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}

	value, ok := pass.TypesInfo.Types[call.Args[0]]
	if !ok || value.Value == nil || value.Value.Kind() != constant.String {
		return ""
	}

	return constant.StringVal(value.Value)
}

// isTouch says whether a call is named as a touch, on whatever holds it.
func isTouch(call *ast.CallExpr) bool {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	case *ast.Ident:
		name = fun.Name
	default:
		return false
	}

	return strings.HasPrefix(strings.ToLower(name), touchPrefix)
}

// queryName is the sqlc query a call runs, when the call is a method of the models' Queries.
func queryName(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}

	method, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Func)
	if !ok || method.Pkg() == nil || method.Pkg().Path() != modelsPackage {
		return "", false
	}

	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return "", false
	}

	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}

	named, ok := types.Unalias(receiver).(*types.Named)
	if !ok || named.Obj().Name() != queriesType {
		return "", false
	}

	return method.Name(), true
}

// queryFiles is what the rule learns from the sqlc query files, read once: the tables each
// query writes, and the tables the listings read.
type queryFiles struct {
	dir      string
	listings map[string][]string
	once     sync.Once
	loaded   loadedQueries
	err      error
}

// loadedQueries is the tables each query writes and, for each table a listing reads, which
// listings read it.
type loadedQueries struct {
	writes  map[string][]string
	watched map[string][]string
}

// load reads the query files the first time it is asked, and answers the same thereafter.
func (q *queryFiles) load() (loadedQueries, error) {
	q.once.Do(func() {
		q.loaded, q.err = readQueries(q.dir, q.listings)
	})

	return q.loaded, q.err
}

// readQueries reads every named query under the directory, noting the tables each writes, and
// works out from the listing queries which tables the listings read.
func readQueries(dir string, listings map[string][]string) (loadedQueries, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return loadedQueries{}, fmt.Errorf("failed to list the query files under %q: %w", dir, err)
	}
	if len(files) == 0 {
		return loadedQueries{}, fmt.Errorf("failed to find any sqlc query files under %q", dir)
	}

	bodies := map[string]string{}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			return loadedQueries{}, fmt.Errorf("failed to read the query file %q: %w", file, err)
		}

		maps.Copy(bodies, splitQueries(string(text)))
	}

	loaded := loadedQueries{writes: make(map[string][]string, len(bodies)), watched: map[string][]string{}}
	for name, body := range bodies {
		loaded.writes[name] = writtenTables(body)
	}

	for _, listing := range slices.Sorted(maps.Keys(listings)) {
		for _, name := range listings[listing] {
			body, ok := bodies[name]
			if !ok {
				return loadedQueries{}, fmt.Errorf("listing %q names query %q, which no file under %q holds", listing, name, dir)
			}

			for _, table := range readTables(body) {
				if !slices.Contains(loaded.watched[table], listing) {
					loaded.watched[table] = append(loaded.watched[table], listing)
				}
			}
		}
	}

	return loaded, nil
}

// splitQueries cuts a query file into its named statements.
func splitQueries(text string) map[string]string {
	headers := queryHeader.FindAllStringSubmatchIndex(text, -1)
	queries := make(map[string]string, len(headers))
	for i, header := range headers {
		end := len(text)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}

		queries[text[header[2]:header[3]]] = text[header[1]:end]
	}

	return queries
}

// writtenTables is every table a statement inserts into, updates or deletes from.
func writtenTables(statement string) []string {
	words := strings.Fields(statement)
	var tables []string
	for i, word := range words {
		if i+1 >= len(words) {
			break
		}

		switch strings.ToUpper(word) {
		case "INSERT":
			if strings.EqualFold(words[i+1], "INTO") && i+2 < len(words) {
				tables = append(tables, tableName(words[i+2]))
			}
		case "DELETE":
			if strings.EqualFold(words[i+1], "FROM") && i+2 < len(words) {
				tables = append(tables, tableName(words[i+2]))
			}
		case "UPDATE":
			if i > 0 && isUpdateQualifier(words[i-1]) {
				continue
			}

			tables = append(tables, tableName(words[i+1]))
		}
	}

	return tables
}

// readTables is every table a statement selects FROM or JOINs, the statement's own CTEs aside.
func readTables(statement string) []string {
	words := strings.Fields(statement)

	ctes := map[string]bool{}
	for i := 1; i+1 < len(words); i++ {
		if strings.EqualFold(words[i], "AS") && strings.HasPrefix(words[i+1], "(") {
			ctes[tableName(strings.TrimPrefix(words[i-1], ","))] = true
		}
	}

	var tables []string
	for i, word := range words {
		if i+1 >= len(words) {
			break
		}

		upper := strings.ToUpper(word)
		if upper != "FROM" && upper != "JOIN" {
			continue
		}

		next := words[i+1]
		if strings.HasPrefix(next, "(") {
			continue
		}

		table := tableName(next)
		if ctes[table] || slices.Contains(tables, table) {
			continue
		}

		tables = append(tables, table)
	}

	return tables
}

// isUpdateQualifier says whether the word before UPDATE makes it a lock or a conflict arm
// rather than a statement.
func isUpdateQualifier(word string) bool {
	upper := strings.ToUpper(word)

	return upper == "FOR" || upper == "DO"
}

// tableName is a table as the statement names it, without the column list that may follow.
func tableName(word string) string {
	if cut := strings.IndexAny(word, "();"); cut >= 0 {
		word = word[:cut]
	}

	return strings.ToLower(word)
}
