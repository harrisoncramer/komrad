package watch

import watchv1 "komrad/proto/gen/watch/v1"

// Resource is one table the watch keeps track of.
type Resource struct {
	// Table is the Postgres table the rows are read from.
	Table string
	// Enum is the value the wire sends the row under.
	Enum watchv1.Resource
	// KeyColumn is the column of Table a row is keyed by.
	KeyColumn string
	// IgnoredColumns are the columns of Table whose changes alone notify nothing.
	IgnoredColumns []string
	// Feeders are the other tables whose changes can move the resource's rows.
	Feeders []Feeder
}

// Feeder is one table whose changes can move the rows of a resource, e.g. agent_runs into agents.
type Feeder struct {
	// Table is the feeding table.
	Table string
	// Column is the column on the feeding row holding the resource's key.
	Column string
	// Lookup finds the resources a feeding row moves when the row does not hold their key.
	Lookup *Lookup
}

// Lookup matches a feeding row to its resources by a column on each side.
type Lookup struct {
	// ParentColumn is the column on the resource's table.
	ParentColumn string
	// ChildColumn is the column on the feeding row that ParentColumn is matched against.
	ChildColumn string
	// SkipParentsWith is a column of the resource's table that has to be null for the resource to be notified.
	SkipParentsWith string
	// OnChangeOf is the one column of the feeding row whose change on an update notifies, or every change when empty.
	OnChangeOf string
}

// buildTables maps each resource's table to the resource.
func buildTables(resources []Resource) map[string]watchv1.Resource {
	tables := make(map[string]watchv1.Resource, len(resources))
	for _, resource := range resources {
		tables[resource.Table] = resource.Enum
	}

	return tables
}
