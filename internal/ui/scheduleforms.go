package ui

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	schedulev1 "komrad/proto/gen/schedule/v1"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// scheduleFormParams is what the schedule form offers to pick from.
type scheduleFormParams struct {
	values        *panel.ScheduleConfig
	workflows     []panel.Workflow
	orchestrators []panel.Agent
	keys          config.FormKeys
}

func scheduleForm(params scheduleFormParams) (*huh.Form, []string) {
	values := params.values

	fields := []huh.Field{
		kindField(values, params.orchestrators),
		huh.NewInput().Key("name").Title("Name").Description(scheduleNameDescription(values.Kind)).Value(&values.Name),
		huh.NewInput().Key("cron").Title("Cron").Placeholder("0 9 * * 1-5").Description("minute hour day month weekday").Value(&values.Cron),
		huh.NewInput().Key("timezone").Title("Timezone").Placeholder("America/New_York").Description("blank = this machine's zone").Value(&values.Timezone),
		textField("prompt").Title("Prompt").Description(schedulePromptDescription(values.Kind)).Value(&values.Prompt),
	}

	if values.Kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		fields = append(fields, orchestratorField(values, params.orchestrators, params.workflows))
	} else {
		fields = append(fields, workflowField(values, params.workflows))
	}

	fields = append(fields,
		huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&values.Enabled).WithButtonAlignment(lipgloss.Left),
	)
	if values.Kind != schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		fields = append(fields,
			huh.NewConfirm().Key("hide_agents").Title("Hide its runs?").Description("they still run, they just take no row").Affirmative("Hidden").Negative("Listed").Value(&values.HideAgents).WithButtonAlignment(lipgloss.Left),
		)
	}

	return formOf(params.keys, fields...)
}

func kindField(values *panel.ScheduleConfig, orchestrators []panel.Agent) huh.Field {
	options := []huh.Option[schedulev1.ScheduleKind]{
		huh.NewOption("Implementer  launch a new agent", schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER),
	}
	description := "what each firing does"
	if len(orchestrators) > 0 || values.Kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		options = append(options, huh.NewOption("Orchestrator  mail one already running", schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR))
	} else {
		description = "start an orchestrator to mail one instead"
	}

	return huh.NewSelect[schedulev1.ScheduleKind]().Key("kind").Title("Kind").Description(description).Options(options...).Value(&values.Kind)
}

func scheduleNameDescription(kind schedulev1.ScheduleKind) string {
	if kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		return "also signs the mail it sends"
	}

	return "also names the agents it launches"
}

func schedulePromptDescription(kind schedulev1.ScheduleKind) string {
	if kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		return "the mail the orchestrator is sent"
	}

	return "what each launched agent is asked to do"
}

func workflowField(values *panel.ScheduleConfig, workflows []panel.Workflow) huh.Field {
	options := xslice.Map(workflows, func(w panel.Workflow) huh.Option[string] {
		return huh.NewOption(w.Name+"  "+label.OrDash(w.Cwd), w.ID)
	})

	// A select whose value names none of its options selects the first one and writes it back,
	// which would move the schedule to a workflow nobody chose.
	if values.WorkflowID != "" && !slices.ContainsFunc(options, func(o huh.Option[string]) bool { return o.Value == values.WorkflowID }) {
		options = append(options, huh.NewOption(values.WorkflowID+"  gone", values.WorkflowID))
	}

	// Options comes before Value so huh leaves the list scrolled to the top.
	return huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Options(options...).Value(&values.WorkflowID)
}

func orchestratorField(values *panel.ScheduleConfig, orchestrators []panel.Agent, workflows []panel.Workflow) huh.Field {
	options := xslice.Map(orchestrators, func(a panel.Agent) huh.Option[string] {
		workflow := "—"
		if w := workflowByID(workflows, a.WorkflowID); w != nil {
			workflow = w.Name
		}

		return huh.NewOption(a.Name+"  "+workflow, a.ID)
	})

	if values.OrchestratorID != "" && !slices.ContainsFunc(options, func(o huh.Option[string]) bool { return o.Value == values.OrchestratorID }) {
		options = append(options, huh.NewOption(values.OrchestratorID+"  gone", values.OrchestratorID))
	}

	return huh.NewSelect[string]().Key("orchestrator").Title("Orchestrator").Description("receives the prompt as mail").Options(options...).Value(&values.OrchestratorID)
}

func archiveScheduleForm(name string, keys config.FormKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Archive schedule %q?", name),
		"The agents it already launched are kept, and nothing brings the schedule back.",
		"Archive",
		keys,
	)
}
