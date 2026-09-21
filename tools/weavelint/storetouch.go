package weavelint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const storeTouchDoc = "Checks that a store function which writes a table the panel's listings are drawn from " +
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

	watched := make(map[string]struct{}, len(settings.Tables))
	for _, table := range settings.Tables {
		watched[table] = struct{}{}
	}

	queries := &queryWrites{dir: settings.Queries}

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

				writes, err := queries.load()
				if err != nil {
					return nil, err
				}

				reportUntouched(pass, file, writes, watched)
			}

			return nil, nil
		},
	}, nil
}

// reportUntouched reports every function that calls a query writing a watched table with no
// touch after it.
func reportUntouched(pass *analysis.Pass, file *ast.File, writes map[string][]string, watched map[string]struct{}) {
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}

		var lastTouch token.Pos
		var firstWrite *ast.CallExpr
		var firstTable string
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			if isTouch(call) {
				lastTouch = call.Pos()

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

		if firstWrite == nil || lastTouch > firstWrite.Pos() {
			continue
		}

		pass.Reportf(function.Pos(),
			"%s writes %s and never touches the watch afterwards: the rows it changed stay stale on the panel",
			function.Name.Name, firstTable)
	}
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

// queryWrites is the table each sqlc query writes, read from the query files once.
type queryWrites struct {
	dir    string
	once   sync.Once
	byName map[string][]string
	err    error
}

// load reads the query files the first time it is asked, and answers the same thereafter.
func (q *queryWrites) load() (map[string][]string, error) {
	q.once.Do(func() {
		q.byName, q.err = readQueryWrites(q.dir)
	})

	return q.byName, q.err
}

// readQueryWrites maps each named query in the directory's sql files to the tables it writes.
func readQueryWrites(dir string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, fmt.Errorf("failed to list the query files under %q: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("failed to find any sqlc query files under %q", dir)
	}

	writes := map[string][]string{}
	for _, file := range files {
		text, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read the query file %q: %w", file, err)
		}

		for name, body := range splitQueries(string(text)) {
			writes[name] = writtenTables(body)
		}
	}

	return writes, nil
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

// isUpdateQualifier says whether the word before UPDATE makes it a lock or a conflict arm
// rather than a statement.
func isUpdateQualifier(word string) bool {
	upper := strings.ToUpper(word)

	return upper == "FOR" || upper == "DO"
}

// tableName is a table as the statement names it, without the column list that may follow.
func tableName(word string) string {
	if cut := strings.IndexAny(word, "(;"); cut >= 0 {
		word = word[:cut]
	}

	return strings.ToLower(word)
}
