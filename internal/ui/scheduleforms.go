package ui

import (
	"fmt"
	"slices"

	"weave/config"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/lib/xslice"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

func scheduleForm(values *panel.ScheduleConfig, workflows []panel.Workflow, keys config.FormKeys) (*huh.Form, []string) {
	options := xslice.Map(workflows, func(w panel.Workflow) huh.Option[string] {
		return huh.NewOption(w.Name+"  "+label.OrDash(w.Cwd), w.ID)
	})

	// A select whose value names none of its options selects the first one and writes it back,
	// which would move the schedule to a workflow nobody chose.
	if values.WorkflowID != "" && !slices.ContainsFunc(options, func(o huh.Option[string]) bool { return o.Value == values.WorkflowID }) {
		options = append(options, huh.NewOption(values.WorkflowID+"  gone", values.WorkflowID))
	}

	return formOf(keys,
		huh.NewInput().Key("name").Title("Name").Description("also names the agents it launches").Value(&values.Name),
		huh.NewInput().Key("cron").Title("Cron").Placeholder("0 9 * * 1-5").Description("minute hour day month weekday").Value(&values.Cron),
		huh.NewInput().Key("timezone").Title("Timezone").Placeholder("America/New_York").Description("blank = this machine's zone").Value(&values.Timezone),
		textField("prompt").Title("Prompt").Description("what each launched agent is asked to do").Value(&values.Prompt),
		// Options comes before Value so huh leaves the list scrolled to the top.
		huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Options(options...).Value(&values.WorkflowID),

		huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&values.Enabled).WithButtonAlignment(lipgloss.Left),
		huh.NewConfirm().Key("hide_agents").Title("Hide its runs?").Description("they still run, they just take no row").Affirmative("Hidden").Negative("Listed").Value(&values.HideAgents).WithButtonAlignment(lipgloss.Left),
	)
}

func archiveScheduleForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive schedule %q?", name),
		"The agents it already launched are kept, and nothing brings the schedule back.",
		"Archive",
		keys,
	)
}
