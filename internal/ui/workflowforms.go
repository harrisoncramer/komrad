package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/eventprompts"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
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
	Hooks                map[string]*string
	WorktreeBaseBranch   string
	WorktreeCopyPaths    string
	WorktreeSymlinkPaths string
	WorktreeDiffExclude  string
	WorktreeDir          string
	// EventPrompts are the workflow's prompts keyed by event.
	EventPrompts map[string]*string
	// EventMutes are whether each event is kept from opening mail, keyed by event.
	EventMutes map[string]*bool
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
		Hooks:                hooksByEvent(cfg.Hooks),
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    strings.Join(cfg.Worktree.CopyPaths, "\n"),
		WorktreeSymlinkPaths: strings.Join(cfg.Worktree.SymlinkPaths, "\n"),
		WorktreeDiffExclude:  strings.Join(cfg.Worktree.DiffExclude, "\n"),
		WorktreeDir:          cfg.Worktree.Dir,
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
		},
		Hooks:        v.hookList(),
		EventPrompts: v.eventPromptList(),
	}
}

type hookMoment struct {
	event       string
	key         string
	title       string
	description string
}

var hookMoments = []hookMoment{
	{
		event:       config.HookPreLaunch,
		key:         "pre_launch_hook",
		title:       "Agent pre-launch hook",
		description: "runs in the agent's directory before its pane opens, and a failure abandons the create",
	},
	{
		event:       config.HookPostLaunch,
		key:         "post_launch_hook",
		title:       "Agent post-launch hook",
		description: "runs once the agent has been launched into its pane",
	},
	{
		event:       config.HookPreResume,
		key:         "pre_resume_hook",
		title:       "Agent pre-resume hook",
		description: "runs in the agent's directory before a dormant agent's pane opens, and a failure refuses the resume",
	},
	{
		event:       config.HookPostResume,
		key:         "post_resume_hook",
		title:       "Agent post-resume hook",
		description: "runs once a dormant agent is back in a pane",
	},
	{
		event:       config.HookPreArchive,
		key:         "pre_archive_hook",
		title:       "Agent pre-archive hook",
		description: "runs while the agent is still running, and a failure refuses the archive",
	},
	{
		event:       config.HookPostArchive,
		key:         "post_archive_hook",
		title:       "Agent post-archive hook",
		description: "runs in the agent's directory once its pane is gone and its row is archived",
	},
	{
		event:       config.HookPreRestore,
		key:         "pre_restore_hook",
		title:       "Agent pre-restore hook",
		description: "runs before an archived agent's row comes back, and a failure refuses the restore",
	},
	{
		event:       config.HookPostRestore,
		key:         "post_restore_hook",
		title:       "Agent post-restore hook",
		description: "runs when an archived agent's row comes back",
	},
	{
		event:       config.HookPreDelete,
		key:         "pre_delete_hook",
		title:       "Agent pre-delete hook",
		description: "runs in the agent's directory as it is deleted, before the worktree is given back",
	},
	{
		event:       config.HookPostDelete,
		key:         "post_delete_hook",
		title:       "Agent post-delete hook",
		description: "runs once the agent and its worktree are gone",
	},
	{
		event:       config.HookTaskPreOpen,
		key:         "task_pre_open_hook",
		title:       "Task pre-open hook",
		description: "runs before a task's first live agent opens its pane, and a failure abandons the create or restore",
	},
	{
		event:       config.HookTaskPostArchive,
		key:         "task_post_archive_hook",
		title:       "Task post-archive hook",
		description: "runs once a task's last live agent is archived and the task with it",
	},
	{
		event:       config.HookTaskPreDelete,
		key:         "task_pre_delete_hook",
		title:       "Task pre-delete hook",
		description: "runs as a task is deleted, before its worktree is given back, and a failure keeps the task",
	},
}

// hooksByEvent is each event's first hook, which is the one the form shows.
func hooksByEvent(hooks []panel.Hook) map[string]*string {
	byEvent := make(map[string]*string, len(hooks))
	for _, hook := range hooks {
		if _, ok := byEvent[hook.Event]; !ok {
			byEvent[hook.Event] = &hook.Script
		}
	}

	return byEvent
}

func (v *workflowFormValues) hookFor(event string) *string {
	if v.Hooks == nil {
		v.Hooks = make(map[string]*string)
	}
	if v.Hooks[event] == nil {
		v.Hooks[event] = new(string)
	}

	return v.Hooks[event]
}

func (v workflowFormValues) hookList() []panel.Hook {
	var hooks []panel.Hook
	for _, moment := range hookMoments {
		script, ok := v.Hooks[moment.event]
		if !ok || *script == "" {
			continue
		}

		hooks = append(hooks, panel.Hook{Event: moment.event, Script: *script})
	}

	return hooks
}

// eventPromptsByEvent is every event's prompt, blank for an event the workflow attaches none to.
func eventPromptsByEvent(prompts []panel.EventPrompt) map[string]*string {
	byEvent := make(map[string]*string, len(prompts))
	for _, prompt := range prompts {
		byEvent[prompt.Event] = &prompt.Prompt
	}

	return byEvent
}

// eventMutesByEvent is whether each event is muted, false for an event the workflow does not mute.
func eventMutesByEvent(prompts []panel.EventPrompt) map[string]*bool {
	byEvent := make(map[string]*bool, len(prompts))
	for _, prompt := range prompts {
		byEvent[prompt.Event] = &prompt.Muted
	}

	return byEvent
}

// eventPromptFor is where the form holds one event's prompt.
func (v *workflowFormValues) eventPromptFor(event string) *string {
	if v.EventPrompts == nil {
		v.EventPrompts = make(map[string]*string)
	}
	if v.EventPrompts[event] == nil {
		v.EventPrompts[event] = new(string)
	}

	return v.EventPrompts[event]
}

// eventMuteFor is where the form holds whether one event is muted.
func (v *workflowFormValues) eventMuteFor(event string) *bool {
	if v.EventMutes == nil {
		v.EventMutes = make(map[string]*bool)
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
	for _, event := range eventprompts.All() {
		var prompt string
		if text, ok := v.EventPrompts[event.Name]; ok {
			prompt = strings.TrimSpace(*text)
		}
		muted := v.EventMutes[event.Name] != nil && *v.EventMutes[event.Name]
		if prompt == "" && !muted {
			continue
		}

		prompts = append(prompts, panel.EventPrompt{
			Event:  event.Name,
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
	Submit  func(*model) tea.Cmd
}

func workflowForm(params workflowFormParams) sectionedForm {
	values := params.Values
	if values.Provisioner == "" {
		values.Provisioner = provisionerWorktree
	}

	renderedProvisioner := values.Provisioner
	choice := workflowSectionAgents

	return sectionedForm{
		kind:  formWorkflow,
		title: "Settings",
		top: func(*model) []huh.Field {
			renderedProvisioner = values.Provisioner

			return workflowTopFields(params.NameTitle, values)
		},
		topEditable: []editableText{{key: "context", value: &values.Context}},
		reshaped:    func() bool { return values.Provisioner != renderedProvisioner },
		sections:    workflowSections(values, params.Editing),
		submit:      params.Submit,
		choice:      &choice,
	}
}

func workflowTopFields(nameTitle string, values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
		textField("context").Title("Context").
			Description(fmt.Sprintf("shared notes for this workflow; %s opens $EDITOR", keyLabel(fixedKeys().Editor))).
			Lines(2).Value(&values.Context),
		huh.NewInput().Key("dir").Title("Default Directory").
			Description("where agents start, and the checkout worktrees are cut from").
			Value(&values.Cwd),
		huh.NewSelect[string]().Key(provisionerKey).Title("Provisioner").
			Options(provisionerOptions()...).Value(&values.Provisioner),
	}
}

const (
	workflowSectionAgents       = "agents"
	workflowSectionWorktree     = "worktree"
	workflowSectionHooks        = "hooks"
	workflowSectionEventPrompts = "event_prompts"
)

func workflowSections(values *workflowFormValues, editing bool) []formSection {
	return []formSection{
		{
			key:   workflowSectionAgents,
			title: "agents",
			fields: func(*model) []huh.Field {
				return workflowAgentFields(values)
			},
		},
		{
			key:   workflowSectionWorktree,
			title: "worktree",
			isOff: func() bool { return values.Provisioner != provisionerWorktree },
			fields: func(*model) []huh.Field {
				return worktreeDetailFields(values, editing)
			},
			editable: []editableText{
				{key: "worktree_copy_paths", value: &values.WorktreeCopyPaths},
				{key: "worktree_symlink_paths", value: &values.WorktreeSymlinkPaths},
				{key: "worktree_diff_exclude", value: &values.WorktreeDiffExclude},
			},
		},
		{
			key:   workflowSectionHooks,
			title: "hooks",
			fields: func(m *model) []huh.Field {
				return hookFields(values, m.getAvailableHooks())
			},
		},
		{
			key:   workflowSectionEventPrompts,
			title: "event prompts",
			fields: func(*model) []huh.Field {
				return eventPromptFields(values, fixedKeys())
			},
			editable: eventPromptEditables(values),
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
			Description("where this workflow's worktrees are made, for a repo that expects them somewhere of its own").
			Placeholder("the configured worktrees directory").Value(&values.WorktreeDir),
		textField("worktree_copy_paths").Title("Worktree copy paths").
			Description("one repo-relative path per line, copied fresh into every worktree, such as .env").
			Lines(2).Value(&values.WorktreeCopyPaths),
		textField("worktree_symlink_paths").Title("Worktree symlink paths").
			Description("one repo-relative path per line, symlinked back instead of duplicated, such as node_modules").
			Lines(2).Value(&values.WorktreeSymlinkPaths),
		textField("worktree_diff_exclude").Title("Diff excludes").
			Description("one path or glob per line, left out of the diff the review screen renders, such as proto/gen or *.pb.go").
			Lines(2).Value(&values.WorktreeDiffExclude),
	}
}

// baseBranchField is the branch worktrees are cut from, filled in from the clone on create and
// required once stored.
func baseBranchField(values *workflowFormValues, editing bool) huh.Field {
	field := huh.NewInput().Key("worktree_base_branch").Title("Worktree base branch").
		Value(&values.WorktreeBaseBranch)
	if !editing {
		return field.Description("left blank, it is read from the clone: origin/HEAD, else the branch the checkout is on")
	}

	return field.Description("the branch every agent's work is measured against").
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
	fields := make([]huh.Field, 0, len(hookMoments))
	for _, moment := range hookMoments {
		chosen := values.hookFor(moment.event)
		options := hookOptions(available, *chosen)

		// Options comes before Value so huh reads the zero value and leaves the list scrolled to
		// the top, which holds only while None, carrying the empty name, leads it.
		field := huh.NewSelect[string]().Key(moment.key).Title(moment.title).
			Description(hookFieldDescription(moment.description, available)).
			Options(options...).
			Value(chosen)

		fields = append(fields, field)
	}

	return fields
}

// eventPromptKey is the form key of one event's prompt.
func eventPromptKey(event string) string {
	return "event_prompt." + event
}

// eventMuteKey is the form key of whether one event is muted.
func eventMuteKey(event string) string {
	return "event_mute." + event
}

// eventPromptFields are what the workflow asks of its agents on each event, under a heading of their own.
func eventPromptFields(values *workflowFormValues, keys config.FixedKeys) []huh.Field {
	heading := huh.NewNote().Title("Event prompts").
		Description(fmt.Sprintf("added to the mail komrad sends an agent on each event, and followed over what komrad says; %s opens $EDITOR", keyLabel(keys.Editor)))

	fields := []huh.Field{heading}
	for _, event := range eventprompts.All() {
		fields = append(fields,
			textField(eventPromptKey(event.Name)).Title(event.Title).
				Description("when "+event.Description).
				CharLimit(eventprompts.MaxPrompt).Lines(2).Value(values.eventPromptFor(event.Name)),
			huh.NewSelect[bool]().Key(eventMuteKey(event.Name)).Title(event.Title+" mail").
				Description("muted, it never opens mail of its own, only rewrites mail the agent still holds").
				Options(huh.NewOption("Tell the agent", false), huh.NewOption("Muted", true)).
				Value(values.eventMuteFor(event.Name)),
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
