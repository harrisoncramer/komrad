package agents

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/eventprompts"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"

	"charm.land/huh/v2"
)

const (
	provisionerNone     = "none"
	provisionerWorktree = "worktree"
)

type workflowFormValues struct {
	Name                 string
	Context              string
	Harness              string
	Cwd                  string
	Flags                string
	ReviewerHarness      string
	ReviewerFlags        string
	Provisioner          string
	Hooks                map[eventv1.Moment]*string
	WorktreeBaseBranch   string
	WorktreeCopyPaths    string
	WorktreeSymlinkPaths string
	WorktreeDiffExclude  string
	WorktreeDir          string
	WorktreeSetupCommand string
	// EventPrompts are the workflow's prompts keyed by event.
	EventPrompts map[eventv1.Event]*string
	// EventMutes are whether each event is kept from opening mail, keyed by event.
	EventMutes map[eventv1.Event]*bool
}

func workflowFormValuesFrom(cfg panel.WorkflowConfig) workflowFormValues {
	provisioner := provisionerNone
	if cfg.Worktree.Enabled {
		provisioner = provisionerWorktree
	}

	return workflowFormValues{
		Name:                 cfg.Name,
		Context:              cfg.Context,
		Harness:              cfg.Harness,
		Cwd:                  cfg.Cwd,
		Flags:                cfg.Flags,
		ReviewerHarness:      cfg.ReviewerHarness,
		ReviewerFlags:        cfg.ReviewerFlags,
		Provisioner:          provisioner,
		Hooks:                hooksByMoment(cfg.Hooks),
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    strings.Join(cfg.Worktree.CopyPaths, "\n"),
		WorktreeSymlinkPaths: strings.Join(cfg.Worktree.SymlinkPaths, "\n"),
		WorktreeDiffExclude:  strings.Join(cfg.Worktree.DiffExclude, "\n"),
		WorktreeDir:          cfg.Worktree.Dir,
		WorktreeSetupCommand: cfg.Worktree.SetupCommand,
		EventPrompts:         eventPromptsByEvent(cfg.EventPrompts),
		EventMutes:           eventMutesByEvent(cfg.EventPrompts),
	}
}

func (v workflowFormValues) toConfig() panel.WorkflowConfig {
	return panel.WorkflowConfig{
		Name:            v.Name,
		Context:         v.Context,
		Harness:         v.Harness,
		Cwd:             v.Cwd,
		Flags:           v.Flags,
		ReviewerHarness: v.ReviewerHarness,
		ReviewerFlags:   v.ReviewerFlags,
		Worktree: panel.WorktreeConfig{
			Enabled:      v.Provisioner == provisionerWorktree,
			BaseBranch:   v.WorktreeBaseBranch,
			CopyPaths:    splitLines(v.WorktreeCopyPaths),
			SymlinkPaths: splitLines(v.WorktreeSymlinkPaths),
			DiffExclude:  splitLines(v.WorktreeDiffExclude),
			Dir:          strings.TrimSpace(v.WorktreeDir),
			SetupCommand: strings.TrimSpace(v.WorktreeSetupCommand),
		},
		Hooks:        v.hookList(),
		EventPrompts: v.eventPromptList(),
	}
}

// hooksByMoment is each moment's first hook, which is the one the form shows.
func hooksByMoment(hooks []panel.Hook) map[eventv1.Moment]*string {
	byMoment := make(map[eventv1.Moment]*string, len(hooks))
	for _, hook := range hooks {
		if _, ok := byMoment[hook.Moment]; !ok {
			byMoment[hook.Moment] = &hook.Script
		}
	}

	return byMoment
}

func (v *workflowFormValues) hookFor(moment eventv1.Moment) *string {
	if v.Hooks == nil {
		v.Hooks = make(map[eventv1.Moment]*string)
	}
	if v.Hooks[moment] == nil {
		v.Hooks[moment] = new(string)
	}

	return v.Hooks[moment]
}

func (v workflowFormValues) hookList() []panel.Hook {
	var hooks []panel.Hook
	for _, moment := range config.HookMoments() {
		script, ok := v.Hooks[moment]
		if !ok || *script == "" {
			continue
		}

		hooks = append(hooks, panel.Hook{Moment: moment, Script: *script})
	}

	return hooks
}

// eventPromptsByEvent is every event's prompt, blank for an event the workflow attaches none to.
func eventPromptsByEvent(prompts []panel.EventPrompt) map[eventv1.Event]*string {
	byEvent := make(map[eventv1.Event]*string, len(prompts))
	for _, prompt := range prompts {
		byEvent[prompt.Event] = &prompt.Prompt
	}

	return byEvent
}

// eventMutesByEvent is whether each event is muted, false for an event the workflow does not mute.
func eventMutesByEvent(prompts []panel.EventPrompt) map[eventv1.Event]*bool {
	byEvent := make(map[eventv1.Event]*bool, len(prompts))
	for _, prompt := range prompts {
		byEvent[prompt.Event] = &prompt.Muted
	}

	return byEvent
}

// eventPromptFor is where the form holds one event's prompt.
func (v *workflowFormValues) eventPromptFor(event eventv1.Event) *string {
	if v.EventPrompts == nil {
		v.EventPrompts = make(map[eventv1.Event]*string)
	}
	if v.EventPrompts[event] == nil {
		v.EventPrompts[event] = new(string)
	}

	return v.EventPrompts[event]
}

// eventMuteFor is where the form holds whether one event is muted.
func (v *workflowFormValues) eventMuteFor(event eventv1.Event) *bool {
	if v.EventMutes == nil {
		v.EventMutes = make(map[eventv1.Event]*bool)
	}
	if v.EventMutes[event] == nil {
		v.EventMutes[event] = new(bool)
	}

	return v.EventMutes[event]
}

// eventPromptList is the prompts as the wire takes them, in the order the events are listed and
// leaving out the events that neither prompt nor mute.
func (v workflowFormValues) eventPromptList() []panel.EventPrompt {
	var prompts []panel.EventPrompt
	for _, event := range protoenum.Values[eventv1.Event]() {
		var prompt string
		if text, ok := v.EventPrompts[event]; ok {
			prompt = strings.TrimSpace(*text)
		}
		muted := v.EventMutes[event] != nil && *v.EventMutes[event]
		if prompt == "" && !muted {
			continue
		}

		prompts = append(prompts, panel.EventPrompt{
			Event:  event,
			Prompt: prompt,
			Muted:  muted,
		})
	}

	return prompts
}

func splitLines(text string) []string {
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	return lines
}

const provisionerKey = "provisioner"

func provisionerOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("None: agents keep the directory they are given", provisionerNone),
		huh.NewOption("Worktree: a git worktree per agent", provisionerWorktree),
	}
}

// workflowFormParams is what the workflow form is rendered from and what it does once answered.
type workflowFormParams struct {
	NameTitle string
	Values    *workflowFormValues
	// Editing says the workflow already exists and its base branch is required.
	Editing bool
	// Hooks lists the hooks the hooks subform offers.
	Hooks  func() []string
	Submit func(form.Inputs) screen.Request
}

func workflowForm(params workflowFormParams) form.Form {
	values := params.Values
	if values.Provisioner == "" {
		values.Provisioner = provisionerWorktree
	}

	renderedProvisioner := values.Provisioner
	choice := workflowSubformAgents
	subforms := workflowSubforms(values, params.Editing, params.Hooks)

	return form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			renderedProvisioner = values.Provisioner

			return form.Of(config.Fixed(), append(workflowTopFields(params.NameTitle, values), form.SubformList("Settings", subforms, &choice))...)
		},
		DoesAnswerReshape: func() bool { return values.Provisioner != renderedProvisioner },
		Editable:          []form.Editable{{Key: "context", Value: &values.Context}},
		Subforms:          subforms,
		Submit:            params.Submit,
	}
}

func workflowTopFields(nameTitle string, values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
		form.Text("context").Title("Context").
			Description(fmt.Sprintf("shared notes for this workflow; %s opens $EDITOR", keys.Label(config.Fixed().Editor))).
			Lines(2).Value(&values.Context),
		huh.NewInput().Key("dir").Title("Default Directory").
			Description("agents start here, and worktrees come from this checkout").
			Value(&values.Cwd),
		huh.NewSelect[string]().Key(provisionerKey).Title("Provisioner").
			Options(provisionerOptions()...).Value(&values.Provisioner),
	}
}

const (
	workflowSubformAgents       = "agents"
	workflowSubformWorktree     = "worktree"
	workflowSubformHooks        = "hooks"
	workflowSubformEventPrompts = "event_prompts"
)

func workflowSubforms(values *workflowFormValues, editing bool, hooks func() []string) []form.Subform {
	return []form.Subform{
		{
			Key:   workflowSubformAgents,
			Title: "agents",
			Fields: func(form.Inputs) []huh.Field {
				return workflowAgentFields(values)
			},
		},
		{
			Key:       workflowSubformWorktree,
			Title:     "worktree",
			IsRefused: func() bool { return values.Provisioner != provisionerWorktree },
			Fields: func(form.Inputs) []huh.Field {
				return worktreeDetailFields(values, editing)
			},
			Editable: []form.Editable{
				{Key: "worktree_copy_paths", Value: &values.WorktreeCopyPaths},
				{Key: "worktree_symlink_paths", Value: &values.WorktreeSymlinkPaths},
				{Key: "worktree_diff_exclude", Value: &values.WorktreeDiffExclude},
			},
		},
		{
			Key:   workflowSubformHooks,
			Title: "hooks",
			Fields: func(form.Inputs) []huh.Field {
				return hookFields(values, hooks())
			},
		},
		{
			Key:   workflowSubformEventPrompts,
			Title: "event prompts",
			Fields: func(form.Inputs) []huh.Field {
				return eventPromptFields(values, config.Fixed())
			},
			Editable: eventPromptEditables(values),
		},
	}
}

func workflowAgentFields(values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("harness").Title("Default Agent").Placeholder("claude").Value(&values.Harness),
		huh.NewInput().Key("flags").Title("Default Flags").Placeholder("--model opus").Value(&values.Flags),
		huh.NewInput().Key("reviewer_harness").Title("Reviewer agent").
			Description("the CLI reviewers run").
			Placeholder("the default agent").Value(&values.ReviewerHarness),
		huh.NewInput().Key("reviewer_flags").Title("Reviewer flags").
			Description("the flags reviewers launch with").
			Placeholder("the default flags, when the agent is the same").Value(&values.ReviewerFlags),
	}
}

func worktreeDetailFields(values *workflowFormValues, editing bool) []huh.Field {
	return []huh.Field{
		baseBranchField(values, editing),
		huh.NewInput().Key("worktree_dir").Title("Worktree parent directory").
			Description("komrad makes this workflow's worktrees here, for a repo that wants them in a place of its own").
			Placeholder("the configured worktrees directory").Value(&values.WorktreeDir),
		form.Text("worktree_copy_paths").Title("Worktree copy paths").
			Description("one repo-relative path per line that komrad copies into every worktree, such as .env").
			Lines(2).Value(&values.WorktreeCopyPaths),
		form.Text("worktree_symlink_paths").Title("Worktree symlink paths").
			Description("one repo-relative path per line that komrad symlinks into every worktree instead of copying, such as node_modules").
			Lines(2).Value(&values.WorktreeSymlinkPaths),
		huh.NewInput().Key("worktree_setup_command").Title("Worktree setup command").
			Description("a shell command komrad runs in the worktree before an agent starts or resumes there, such as mise trust && mise install").
			Value(&values.WorktreeSetupCommand),
		form.Text("worktree_diff_exclude").Title("Diff excludes").
			Description("one path or glob per line that the review screen leaves out of the diff, such as proto/gen or *.pb.go").
			Lines(2).Value(&values.WorktreeDiffExclude),
	}
}

// baseBranchField is the branch worktrees are cut from, filled in from the clone on create and
// required once stored.
func baseBranchField(values *workflowFormValues, editing bool) huh.Field {
	field := huh.NewInput().Key("worktree_base_branch").Title("Worktree base branch").
		Value(&values.WorktreeBaseBranch)
	if !editing {
		return field.Description("leave blank and komrad reads it from the clone, using origin/HEAD or else the checkout's branch")
	}

	return field.Description("komrad measures every agent's work against this branch").
		Validate(func(name string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("a workflow's base branch cannot be blank")
			}

			return nil
		})
}

// hookFields are the scripts hung on a moment rather than on the directory, one select each
// since a moment holds one hook.
func hookFields(values *workflowFormValues, available []string) []huh.Field {
	moments := config.HookMoments()
	fields := make([]huh.Field, 0, len(moments))
	for _, moment := range moments {
		label := protoenum.Label(moment)
		chosen := values.hookFor(moment)
		options := hookOptions(available, *chosen)

		// Options comes before Value so huh reads the zero value and leaves the list scrolled to
		// the top, which holds only while None, carrying the empty name, leads it.
		field := huh.NewSelect[string]().Key(hookKey(moment)).Title(label.GetTitle()).
			Description(hookFieldDescription(label.GetDescription(), available)).
			Options(options...).
			Value(chosen)

		fields = append(fields, field)
	}

	return fields
}

func hookKey(moment eventv1.Moment) string {
	return "hook." + protoenum.Name(moment)
}

// eventPromptKey is the form key of one event's prompt.
func eventPromptKey(event eventv1.Event) string {
	return "event_prompt." + protoenum.Name(event)
}

// eventMuteKey is the form key of whether one event is muted.
func eventMuteKey(event eventv1.Event) string {
	return "event_mute." + protoenum.Name(event)
}

// eventPromptFields are what the workflow asks of its agents on each event, under a heading of their own.
func eventPromptFields(values *workflowFormValues, fixed config.FixedKeys) []huh.Field {
	heading := huh.NewNote().Title("Event prompts").
		Description(fmt.Sprintf("komrad adds this to the mail it sends an agent on each event, and the agent follows it over komrad's own words; %s opens $EDITOR", keys.Label(fixed.Editor)))

	fields := []huh.Field{heading}
	for _, event := range protoenum.Values[eventv1.Event]() {
		label := protoenum.Label(event)
		fields = append(fields,
			form.Text(eventPromptKey(event)).Title(label.GetTitle()).
				Description("when "+label.GetDescription()).
				CharLimit(eventprompts.MaxPrompt).Lines(2).Value(values.eventPromptFor(event)),
			huh.NewSelect[bool]().Key(eventMuteKey(event)).Title(label.GetTitle()+" mail").
				Description("when muted, komrad sends no mail of its own and only rewrites mail the agent still holds").
				Options(huh.NewOption("Tell the agent", false), huh.NewOption("Muted", true)).
				Value(values.eventMuteFor(event)),
		)
	}

	return fields
}

// hookFieldDescription leads with the empty case rather than appending it.
func hookFieldDescription(description string, available []string) string {
	if len(available) == 0 {
		return "none found in your hooks directory; " + description
	}

	return description
}

// hookOptions offers None, every hook in the directory, and the one this workflow already names
// if the directory no longer holds it.
func hookOptions(available []string, chosen string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(available)+2)
	options = append(options, huh.NewOption("None", ""))

	options = append(options, xslice.Map(available, func(hook string) huh.Option[string] {
		return huh.NewOption(hook, hook)
	})...)

	offered := slices.Contains(available, chosen)
	if chosen != "" && !offered {
		options = append(options, huh.NewOption(chosen+"  (missing)", chosen))
	}

	return options
}
