package watch

// Trigger is one row trigger the watch needs attached for its resources to notify.
type Trigger struct {
	Name     string
	Table    string
	Function string
	Args     []string
}

// TriggerFunctions is every function a watch trigger runs.
var TriggerFunctions = []string{"watch_notify", "watch_project", "watch_project_lookup"}

// Triggers is every trigger the resources declare.
func Triggers() []Trigger {
	var triggers []Trigger
	for _, resource := range Resources() {
		triggers = append(triggers, Trigger{
			Name:     "watch_notify",
			Table:    resource.Table,
			Function: "watch_notify",
			Args:     []string{resource.Key},
		})

		for _, projection := range resource.Projects {
			trigger := Trigger{Name: "watch_project_" + resource.Table, Table: projection.Table}
			if projection.Column != "" {
				trigger.Function = "watch_project"
				trigger.Args = []string{resource.Table, projection.Column}
			} else {
				trigger.Function = "watch_project_lookup"
				trigger.Args = []string{resource.Table, resource.Key, projection.ParentMatch, projection.Match, projection.ParentMustBeNull}
			}
			triggers = append(triggers, trigger)
		}
	}

	return triggers
}
