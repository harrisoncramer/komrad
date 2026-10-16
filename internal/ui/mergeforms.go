package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	forgev1 "komrad/proto/gen/forge/v1"

	"charm.land/huh/v2"
)

type mergeFormParams struct {
	keys   config.FixedKeys
	title  string
	values *mergeValues
	plan   *forgev1.PlanForgePullRequestMergeResponse
}

// mergeForm asks how a pull request merges, and with what message when the method writes a
// commit of its own.
func mergeForm(params mergeFormParams) (*huh.Form, []string) {
	options := xslice.Map(params.plan.GetOptions(), func(option *forgev1.MergeOption) huh.Option[forgev1.MergeMethod] {
		return huh.NewOption(mergeOptionLabel(option), option.GetMethod())
	})
	hasMessage := mergeOptionOf(params.plan, params.values.Method).GetMessage() != nil

	description := fmt.Sprintf("writes no commit of its own; %s merges", keyLabel(params.keys.Submit))
	if hasMessage {
		description = "how the commits land on the base"
	}

	fields := []huh.Field{
		// Options comes before Value so huh leaves the list scrolled to the top.
		huh.NewSelect[forgev1.MergeMethod]().Key("method").Title(params.title).Description(description).
			Options(options...).Value(&params.values.Method),
	}
	if hasMessage {
		fields = append(fields,
			huh.NewInput().Key("headline").Title("Headline").Value(&params.values.Headline),
			textField("body").Title("Body").
				Description(fmt.Sprintf("%s opens $EDITOR; %s merges", keyLabel(params.keys.Editor), keyLabel(params.keys.Submit))).
				Value(&params.values.Body),
		)
	}

	return formOf(params.keys, fields...)
}

// mergeFormTitle names what the merge form is about to do.
func mergeFormTitle(task panel.Task, choice mergeChoice) string {
	ref := task.PullRequest.Ref()

	switch choice {
	case mergeChoiceMergeArchive:
		return "Merge " + ref + " and archive " + task.Slug
	case mergeChoiceMergeRebase:
		return "Merge " + ref + " then rebase " + task.Slug
	case mergeChoiceAutoMerge:
		return "Merge " + ref + " when ready"
	case mergeChoiceMerge:
	}

	return "Merge " + ref
}

// mergeOptionLabel names a merge method, muted when the base branch does not allow it.
func mergeOptionLabel(option *forgev1.MergeOption) string {
	name := mergeMethodLabel(option.GetMethod())
	if option.GetAllowed() {
		return name
	}

	return paint.Panel.Style(paint.ToneMuted).Render(name + "  not allowed")
}

func mergeMethodLabel(method forgev1.MergeMethod) string {
	switch method {
	case forgev1.MergeMethod_MERGE_METHOD_MERGE:
		return "merge commit"
	case forgev1.MergeMethod_MERGE_METHOD_SQUASH:
		return "squash"
	case forgev1.MergeMethod_MERGE_METHOD_REBASE:
		return "rebase"
	case forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD:
		return "fast-forward"
	case forgev1.MergeMethod_MERGE_METHOD_UNSPECIFIED:
	}

	return "merge"
}
