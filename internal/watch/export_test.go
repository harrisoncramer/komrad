package watch

import watchv1 "weave/proto/gen/watch/v1"

// NewEmptyRows builds a Rows over stores holding nothing.
func NewEmptyRows() *Rows {
	return newRows(newStores())
}

// ListLoadedResources lists the resources rows has a loader for.
func ListLoadedResources(rows *Rows) []watchv1.Resource {
	resources := make([]watchv1.Resource, 0, len(rows.loaders))
	for resource := range rows.loaders {
		resources = append(resources, resource)
	}

	return resources
}
