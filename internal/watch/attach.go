package watch

import (
	"bytes"
	"fmt"
)

// attachHeader says the migration below is output rather than something to edit.
const attachHeader = `-- Attaches the watch triggers to every table a resource is the rows of, and to every table
-- projected onto one.
--
-- Generated from the resources in internal/watch. Do not edit by hand; change that list and run
-- 'mise run gen:triggers'. The trigger functions this calls are hand-written, one migration
-- earlier.
`

// RenderAttachments is the migration that puts a trigger on each resource's table and on each
// table projected onto one.
func RenderAttachments() []byte {
	var out bytes.Buffer
	out.WriteString(attachHeader)

	for _, r := range Resources() {
		fmt.Fprintf(&out, "\nSELECT\n    attach_watch_trigger(%s, %s);\n", quote(r.Table), quote(r.Key))

		for _, p := range r.Projects {
			if p.Column != "" {
				fmt.Fprintf(&out, "\nSELECT\n    attach_projection_trigger(%s, %s, %s);\n", quote(p.Table), quote(r.Table), quote(p.Column))

				continue
			}

			fmt.Fprintf(&out, "\nSELECT\n    attach_projection_lookup_trigger(%s, %s, %s, %s, %s, %s);\n",
				quote(p.Table), quote(r.Table), quote(r.Key), quote(p.Lookup), quote(p.Via), narrowing(p.Where))
		}
	}

	return out.Bytes()
}

// quote is a name as a SQL string literal.
func quote(name string) string {
	return "'" + name + "'"
}

// narrowing is a lookup's extra condition, NULL when it has none.
func narrowing(where string) string {
	if where == "" {
		return "NULL"
	}

	return quote(where)
}
