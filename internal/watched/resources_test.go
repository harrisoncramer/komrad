package watched

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"komrade/internal/dbtest"
	"komrade/internal/store"
	"komrade/internal/watch"
	"komrade/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryDeclaredColumnIsInTheSchema pins that every column a resource names exists on its table.
func TestEveryDeclaredColumnIsInTheSchema(t *testing.T) {
	st, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	rows, err := st.GetRawPool().Query(t.Context(), `SELECT table_name::text || '.' || column_name::text FROM information_schema.columns WHERE table_schema = 'public'`)
	require.NoError(t, err)
	defer rows.Close()

	migrated := map[string]bool{}
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		migrated[column] = true
	}
	require.NoError(t, rows.Err())

	for _, column := range listDeclaredColumns(All()) {
		assert.True(t, migrated[column], "%s is declared but not in the schema", column)
	}
}

// TestTheMigratedTriggersAreTheDeclaredOnes pins that the schema carries exactly the triggers the
// resources declare.
func TestTheMigratedTriggersAreTheDeclaredOnes(t *testing.T) {
	st, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	functions := xslice.Map(watch.TriggerFunctions, func(function watch.TriggerFunction) string { return function.Name })
	rows, err := st.GetRawPool().Query(t.Context(), `
		SELECT
			t.tgname,
			c.relname,
			t.tgfoid::regproc::text,
			t.tgargs
		FROM
			pg_trigger t
			JOIN pg_class c ON c.oid = t.tgrelid
		WHERE
			NOT t.tgisinternal
			AND t.tgfoid::regproc::text = ANY ($1)`, functions)
	require.NoError(t, err)
	defer rows.Close()

	var migrated []string
	for rows.Next() {
		var trigger watch.Trigger
		var args []byte
		require.NoError(t, rows.Scan(&trigger.Name, &trigger.Table, &trigger.Function, &args))
		for arg := range bytes.SplitSeq(bytes.TrimSuffix(args, []byte{0}), []byte{0}) {
			trigger.Args = append(trigger.Args, string(arg))
		}
		migrated = append(migrated, spellTrigger(trigger))
	}
	require.NoError(t, rows.Err())

	declared := xslice.Map(watch.BuildTriggers(All()), spellTrigger)
	require.NotEmpty(t, declared)

	slices.Sort(migrated)
	slices.Sort(declared)
	assert.Equal(t, declared, migrated, "the resources changed without a migration; run mise run db:watch-migration <name>")
}

func spellTrigger(trigger watch.Trigger) string {
	return trigger.Table + "." + trigger.Name + " " + trigger.Function + "(" + strings.Join(trigger.Args, ", ") + ")"
}

func listDeclaredColumns(resources []watch.Resource) []string {
	var columns []string
	named := func(table, column string) {
		if column != "" {
			columns = append(columns, table+"."+column)
		}
	}
	for _, resource := range resources {
		columns = append(columns, resource.Table+"."+resource.KeyColumn)
		for _, ignored := range resource.IgnoredColumns {
			named(resource.Table, ignored)
		}
		for _, feeder := range resource.Feeders {
			named(feeder.Table, feeder.Column)
			if lookup := feeder.Lookup; lookup != nil {
				named(resource.Table, lookup.ParentColumn)
				named(resource.Table, lookup.SkipParentsWith)
				named(feeder.Table, lookup.ChildColumn)
				named(feeder.Table, lookup.OnChangeOf)
			}
		}
	}

	return columns
}
