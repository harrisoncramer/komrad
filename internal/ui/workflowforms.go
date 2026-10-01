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
	PreLaunchHook        string
	PostLaunchHook       string
	PreResumeHook        string
	PostResumeHook       string
	PreArchiveHook       string
	PostArchiveHook      string
	PreRestoreHook       string
	PostRestoreHook      string
	PreDeleteHook        string
	PostDeleteHook       string
	WorktreeBaseBranch   string
	WorktreeCopyPaths    string
	WorktreeSymlinkPaths string
	WorktreeDiffExclude  string
	WorktreeDir          string
	// EventPrompts are the workflow's prompts keyed by event.
	EventPrompts map[string]*string
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
		PreLaunchHook:        hookNamed(cfg.Hooks, config.HookPreLaunch),
		PostLaunchHook:       hookNamed(cfg.Hooks, config.HookPostLaunch),
		PreResumeHook:        hookNamed(cfg.Hooks, config.HookPreResume),
		PostResumeHook:       hookNamed(cfg.Hooks, config.HookPostResume),
		PreArchiveHook:       hookNamed(cfg.Hooks, config.HookPreArchive),
		PostArchiveHook:      hookNamed(cfg.Hooks, config.HookPostArchive),
		PreRestoreHook:       hookNamed(cfg.Hooks, config.HookPreRestore),
		PostRestoreHook:      hookNamed(cfg.Hooks, config.HookPostRestore),
		PreDeleteHook:        hookNamed(cfg.Hooks, config.HookPreDelete),
		PostDeleteHook:       hookNamed(cfg.Hooks, config.HookPostDelete),
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    strings.Join(cfg.Worktree.CopyPaths, "\n"),
		WorktreeSymlinkPaths: strings.Join(cfg.Worktree.SymlinkPaths, "\n"),
		WorktreeDiffExclude:  strings.Join(cfg.Worktree.DiffExclude, "\n"),
		WorktreeDir:          cfg.Worktree.Dir,
		EventPrompts:         eventPromptsByEvent(cfg.EventPrompts),
	}
}

func (v workflowFormValues) toConfig() panel.WorkflowConfig {
	cfg := panel.WorkflowConfig{
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
		Hooks: slices.Concat(
			hookOn(v.PreLaunchHook, config.HookPreLaunch),
			hookOn(v.PostLaunchHook, config.HookPostLaunch),
			hookOn(v.PreResumeHook, config.HookPreResume),
			hookOn(v.PostResumeHook, config.HookPostResume),
			hookOn(v.PreArchiveHook, config.HookPreArchive),
			hookOn(v.PostArchiveHook, config.HookPostArchive),
			hookOn(v.PreRestoreHook, config.HookPreRestore),
			hookOn(v.PostRestoreHook, config.HookPostRestore),
			hookOn(v.PreDeleteHook, config.HookPreDelete),
			hookOn(v.PostDeleteHook, config.HookPostDelete),
		),
		EventPrompts: v.eventPromptList(),
	}

	return cfg
}

// hookNamed is the hook a workflow hangs on one moment, and the empty string for a moment
// nothing hangs on.
func hookNamed(hooks []panel.Hook, event string) string {
	for _, hook := range hooks {
		if hook.Event == event {
			return hook.Script
		}
	}

	return ""
}

// hookOn is a moment's hook as the wire takes it, which is one entry or none.
func hookOn(name, event string) []panel.Hook {
	if name == "" {
		return nil
	}

	return []panel.Hook{{Event: event, Script: name}}
}

// eventPromptsByEvent is every event's prompt, blank for an event the workflow attaches none to.
func eventPromptsByEvent(prompts []panel.EventPrompt) map[string]*string {
	byEvent := make(map[string]*string, len(prompts))
	for _, prompt := range prompts {
		byEvent[prompt.Event] = &prompt.Prompt
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

// eventPromptList is the prompts as the wire takes them, in the order the events are listed and
// leaving out the blank ones.
func (v workflowFormValues) eventPromptList() []panel.EventPrompt {
	var prompts []panel.EventPrompt
	for _, event := range eventprompts.All() {
		prompt, ok := v.EventPrompts[event.Name]
		if !ok || strings.TrimSpace(*prompt) == "" {
			continue
		}

		prompts = append(prompts, panel.EventPrompt{Event: event.Name, Prompt: strings.TrimSpace(*prompt)})
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

// workflowFormParams is what the workflow form is drawn from and what it does once answered.
type workflowFormParams struct {
	NameTitle string
	Values    *workflowFormValues
	// Editing says the workflow already exists and its base branch is required.
	Editing bool
	Submit  func(model) (tea.Model, tea.Cmd)
}

func workflowForm(params workflowFormParams) sectionedForm {
	values := params.Values
	if values.Provisioner == "" {
		values.Provisioner = provisionerWorktree
	}

	drawnProvisioner := values.Provisioner
	choice := workflowSectionAgents

	return sectionedForm{
		kind:  formWorkflow,
		title: "Settings",
		top: func(model) []huh.Field {
			drawnProvisioner = values.Provisioner

			return workflowTopFields(params.NameTitle, values)
		},
		topEditable: []editableText{{key: "context", value: &values.Context}},
		reshaped:    func() bool { return values.Provisioner != drawnProvisioner },
		sections:    workflowSections(values, params.Editing),
		submit:      params.Submit,
		choice:      &choice,
	}
}

func workflowTopFields(nameTitle string, values *workflowFormValues) []huh.Field {
	return []huh.Field{
		huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
		textField("context").Title("Context").
			Description(fmt.Sprintf("shared notes for this workflow; %s opens $EDITOR", keyLabel(formKeys().Editor))).
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
			fields: func(model) []huh.Field {
				return workflowAgentFields(values)
			},
		},
		{
			key:   workflowSectionWorktree,
			title: "worktree",
			isOff: func() bool { return values.Provisioner != provisionerWorktree },
			fields: func(model) []huh.Field {
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
			fields: func(m model) []huh.Field {
				return hookFields(values, m.getAvailableHooks())
			},
		},
		{
			key:   workflowSectionEventPrompts,
			title: "event prompts",
			fields: func(model) []huh.Field {
				return eventPromptFields(values, formKeys())
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
			Description("one path or glob per line, left out of the diff the review screen draws, such as proto/gen or *.pb.go").
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
func hookFields(values *workflowFormValues, available []hookChoice) []huh.Field {
	moments := []struct {
		key         string
		title       string
		description string
		chosen      *string
	}{
		{
			key:         "pre_launch_hook",
			title:       "Pre-launch hook",
			description: "runs in the agent's directory before its pane opens, and a failure abandons the create",
			chosen:      &values.PreLaunchHook,
		},
		{
			key:         "post_launch_hook",
			title:       "Post-launch hook",
			description: "runs once the agent has been launched into its pane",
			chosen:      &values.PostLaunchHook,
		},
		{
			key:         "pre_resume_hook",
			title:       "Pre-resume hook",
			description: "runs in the agent's directory before a dormant agent's pane opens, and a failure refuses the resume",
			chosen:      &values.PreResumeHook,
		},
		{
			key:         "post_resume_hook",
			title:       "Post-resume hook",
			description: "runs once a dormant agent is back in a pane",
			chosen:      &values.PostResumeHook,
		},
		{
			key:         "pre_archive_hook",
			title:       "Pre-archive hook",
			description: "runs while the agent is still running, and a failure refuses the archive",
			chosen:      &values.PreArchiveHook,
		},
		{
			key:         "post_archive_hook",
			title:       "Post-archive hook",
			description: "runs in the agent's directory once its pane is gone and its row is archived",
			chosen:      &values.PostArchiveHook,
		},
		{
			key:         "pre_restore_hook",
			title:       "Pre-restore hook",
			description: "runs before an archived agent's row comes back, and a failure refuses the restore",
			chosen:      &values.PreRestoreHook,
		},
		{
			key:         "post_restore_hook",
			title:       "Post-restore hook",
			description: "runs when an archived agent's row comes back",
			chosen:      &values.PostRestoreHook,
		},
		{
			key:         "pre_delete_hook",
			title:       "Pre-delete hook",
			description: "runs in the agent's directory as it is deleted, before the worktree is given back",
			chosen:      &values.PreDeleteHook,
		},
		{
			key:         "post_delete_hook",
			title:       "Post-delete hook",
			description: "runs once the agent and its worktree are gone",
			chosen:      &values.PostDeleteHook,
		},
	}

	fields := make([]huh.Field, 0, len(moments))
	for _, moment := range moments {
		options := hookOptions(available, *moment.chosen)

		// Options comes before Value so huh reads the zero value and leaves the list scrolled to
		// the top, which holds only while None, carrying the empty name, leads it.
		field := huh.NewSelect[string]().Key(moment.key).Title(moment.title).
			Description(hookFieldDescription(moment.description, available)).
			Options(options...).
			Value(moment.chosen)

		fields = append(fields, field)
	}

	return fields
}

// eventPromptKey is the form key of one event's prompt.
func eventPromptKey(event string) string {
	return "event_prompt." + event
}

// eventPromptFields are what the workflow asks of its agents on each event, under a heading of their own.
func eventPromptFields(values *workflowFormValues, keys config.FormKeys) []huh.Field {
	heading := huh.NewNote().Title("Event prompts").
		Description(fmt.Sprintf("added to the mail komrad sends an agent on each event, and followed over what komrad says; %s opens $EDITOR", keyLabel(keys.Editor)))

	return append([]huh.Field{heading}, xslice.Map(eventprompts.All(), func(event eventprompts.Event) huh.Field {
		return textField(eventPromptKey(event.Name)).Title(event.Title).
			Description("when " + event.Description).
			CharLimit(eventprompts.MaxPrompt).Lines(2).Value(values.eventPromptFor(event.Name))
	})...)
}

// hookFieldDescription leads with the empty case rather than appending it.
func hookFieldDescription(description string, available []hookChoice) string {
	if len(available) == 0 {
		return "none found in your hooks directory; " + description
	}

	return description
}

// hookOptions offers None, every hook in the directory, and the one this workflow already names
// if the directory no longer holds it.
func hookOptions(available []hookChoice, chosen string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(available)+2)
	options = append(options, huh.NewOption("None", ""))

	options = append(options, xslice.Map(available, func(hook hookChoice) huh.Option[string] {
		return huh.NewOption(hook.name, hook.name)
	})...)

	offered := slices.ContainsFunc(available, func(a hookChoice) bool { return a.name == chosen })
	if chosen != "" && !offered {
		options = append(options, huh.NewOption(chosen+"  (missing)", chosen))
	}

	return options
}

type hookChoice struct {
	name string
}
