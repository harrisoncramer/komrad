package watched

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	"weave/internal/watch"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryResourceNamesItsKeyColumn pins that no resource leaves its trigger without a key to notify under.
func TestEveryResourceNamesItsKeyColumn(t *testing.T) {
	for _, resource := range All() {
		assert.NotEmpty(t, resource.KeyColumn, "%s names no key column", resource.Table)
	}
}

// TestTheMigratedTriggersAreTheDeclaredOnes pins that the schema carries exactly the triggers the
// resources declare, so a resource changed without a watch migration fails here.
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
