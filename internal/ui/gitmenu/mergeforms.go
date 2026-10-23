package gitmenu

import (
	"cmp"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	forgev1 "komrad/proto/gen/forge/v1"

	"charm.land/huh/v2"
)

// mergeFormValues is the merge form's answers.
type mergeFormValues struct {
	Method   forgev1.MergeMethod
	Headline string
	Body     string
}

// fill writes the option's message into the form unless the person edited the last one filled, and
// returns the message the form now holds as filled.
func (v *mergeFormValues) fill(option *forgev1.MergeOption, filled *forgev1.CommitMessage) *forgev1.CommitMessage {
	if v.Headline != filled.GetHeadline() || v.Body != filled.GetBody() {
		return filled
	}

	message := option.GetMessage()
	v.Headline, v.Body = message.GetHeadline(), message.GetBody()

	return message
}

// buildMergeParams is the merge the form asks for at the head the panel showed, carrying each part
// of the message only when the person changed the forge's own.
func (v mergeFormValues) buildMergeParams(pullRequest panel.PullRequestStanding, plan *forgev1.PlanForgePullRequestMergeResponse) (*api.MergeParams, error) {
	params := &api.MergeParams{
		URL:     pullRequest.URL,
		Method:  v.Method,
		HeadSHA: cmp.Or(pullRequest.HeadSHA, plan.GetHeadSha()),
	}
	message := mergeOptionOf(plan, v.Method).GetMessage()
	if message == nil {
		return params, nil
	}
	if v.Headline != message.GetHeadline() {
		if strings.TrimSpace(v.Headline) == "" {
			return nil, errNoHeadline
		}

		params.Headline = &v.Headline
	}
	if v.Body != message.GetBody() {
		params.Body = &v.Body
	}

	return params, nil
}

type mergeFormParams struct {
	keys   config.FixedKeys
	title  string
	values *mergeFormValues
	plan   *forgev1.PlanForgePullRequestMergeResponse
}

// mergeForm asks how a pull request merges, and with what message when the method writes a
// commit of its own.
func mergeForm(params mergeFormParams) (*huh.Form, []string) {
	options := xslice.Map(params.plan.GetOptions(), func(option *forgev1.MergeOption) huh.Option[forgev1.MergeMethod] {
		return huh.NewOption(mergeOptionLabel(option), option.GetMethod())
	})
	hasMessage := mergeOptionOf(params.plan, params.values.Method).GetMessage() != nil

	description := fmt.Sprintf("writes no commit of its own; %s merges", keys.Label(params.keys.Submit))
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
			form.Text("body").Title("Body").
				Description(fmt.Sprintf("%s opens $EDITOR; %s merges", keys.Label(params.keys.Editor), keys.Label(params.keys.Submit))).
				Value(&params.values.Body),
		)
	}

	return form.Of(params.keys, fields...)
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
