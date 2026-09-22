package watch

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheMigratedTriggersAreTheDeclaredOnes pins that the schema carries exactly the triggers the
// resources declare, so a resource changed without a watch migration fails here.
func TestTheMigratedTriggersAreTheDeclaredOnes(t *testing.T) {
	l := newPooled(t)

	rows, err := l.pool.Query(t.Context(), `
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
			AND t.tgfoid::regproc::text = ANY ($1)`, TriggerFunctions)
	require.NoError(t, err)
	defer rows.Close()

	var migrated []string
	for rows.Next() {
		var trigger Trigger
		var args []byte
		require.NoError(t, rows.Scan(&trigger.Name, &trigger.Table, &trigger.Function, &args))
		for arg := range bytes.SplitSeq(bytes.TrimSuffix(args, []byte{0}), []byte{0}) {
			trigger.Args = append(trigger.Args, string(arg))
		}
		migrated = append(migrated, spell(trigger))
	}
	require.NoError(t, rows.Err())

	declared := xslice.Map(Triggers(), spell)
	require.NotEmpty(t, declared)

	slices.Sort(migrated)
	slices.Sort(declared)
	assert.Equal(t, declared, migrated, "the resources changed without a migration; run mise run db:watch-migration <name>")
}

func spell(trigger Trigger) string {
	return trigger.Table + "." + trigger.Name + " " + trigger.Function + "(" + strings.Join(trigger.Args, ", ") + ")"
}
