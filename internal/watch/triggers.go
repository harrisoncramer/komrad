package watch

// Trigger is one row trigger the watch needs attached for its resources to notify.
type Trigger struct {
	Name     string
	Table    string
	Function string
	Args     []string
}

// TriggerFunctions is every function a watch trigger runs.
var TriggerFunctions = []string{"watch_notify", "watch_subresource", "watch_subresource_lookup"}

// Triggers is every trigger the resources declare.
func Triggers() []Trigger {
	var triggers []Trigger
	for _, resource := range resources {
		triggers = append(triggers, Trigger{
			Name:     "watch_notify",
			Table:    resource.Table,
			Function: "watch_notify",
		})

		for _, subresource := range resource.Subresources {
			trigger := Trigger{Name: "watch_subresource_" + resource.Table, Table: subresource.Table}
			if lookup := subresource.Lookup; lookup != nil {
				trigger.Function = "watch_subresource_lookup"
				trigger.Args = []string{resource.Table, lookup.ParentColumn, lookup.ChildColumn, lookup.SkipParentsWith}
			} else {
				trigger.Function = "watch_subresource"
				trigger.Args = []string{resource.Table, subresource.Key}
			}
			triggers = append(triggers, trigger)
		}
	}

	return triggers
}
