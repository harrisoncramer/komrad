package db_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/resource"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trigger is one watch trigger as a migrated database holds it: the table it is on, the
// function it calls and the arguments it calls it with.
type trigger struct {
	Table    string
	Function string
	Args     []string
}

// TestWatchTriggersAreTheResources pins the migration to the resource option: the watch
// triggers in a migrated database, arguments included, are exactly the ones the annotated
// resources and their projections declare.
func TestWatchTriggersAreTheResources(t *testing.T) {
	ctx := t.Context()

	conn, err := pgx.Connect(ctx, dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })

	rows, err := conn.Query(ctx, `
		SELECT c.relname, p.proname, t.tgargs
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_proc p ON p.oid = t.tgfoid
		WHERE NOT t.tgisinternal AND p.proname LIKE 'watch\_%'`)
	require.NoError(t, err)

	attached, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (trigger, error) {
		var (
			tg   trigger
			args []byte
		)
		err := row.Scan(&tg.Table, &tg.Function, &args)
		tg.Args = splitArgs(args)

		return tg, err
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, declared(), attached)
}

// declared is every trigger the resource option calls for.
func declared() []trigger {
	var triggers []trigger
	for _, r := range resource.All() {
		table := r.Option.GetTable()
		triggers = append(triggers, trigger{Table: table, Function: "watch_notify", Args: []string{r.Key(), array(r.Option.GetIgnores())}})

		for _, p := range r.Option.GetProjects() {
			if p.GetColumn() != "" {
				triggers = append(triggers, trigger{Table: p.GetTable(), Function: "watch_project", Args: []string{table, p.GetColumn(), array(p.GetIgnores())}})

				continue
			}

			triggers = append(triggers, trigger{Table: p.GetTable(), Function: "watch_project_lookup", Args: []string{table, r.Key(), p.GetLookup(), p.GetVia(), array(p.GetIgnores()), where(p.GetWhere())}})
		}
	}

	return triggers
}

// where is a lookup's narrowing as the attach function spells it, true when there is none.
func where(narrowing string) string {
	if narrowing == "" {
		return "true"
	}

	return narrowing
}

// array is a list of columns as a text array literal spells it.
func array(columns []string) string {
	return "{" + strings.Join(columns, ",") + "}"
}

// splitArgs reads tgargs, which holds each argument followed by a NUL.
func splitArgs(args []byte) []string {
	parts := strings.Split(string(bytes.TrimSuffix(args, []byte{0})), "\x00")

	return slices.DeleteFunc(parts, func(part string) bool { return part == "" })
}
