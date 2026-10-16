package forge

import (
	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

// mergeMethods is each merge method in the API's words.
var mergeMethods = map[forge.MergeMethod]forgev1.MergeMethod{
	forge.MergeMethodMerge:       forgev1.MergeMethod_MERGE_METHOD_MERGE,
	forge.MergeMethodSquash:      forgev1.MergeMethod_MERGE_METHOD_SQUASH,
	forge.MergeMethodRebase:      forgev1.MergeMethod_MERGE_METHOD_REBASE,
	forge.MergeMethodFastForward: forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD,
}

func renderMergeMethod(method forge.MergeMethod) forgev1.MergeMethod {
	return mergeMethods[method]
}

func renderMergeOption(option forge.MergeOption) *forgev1.MergeOption {
	rendered := &forgev1.MergeOption{
		Method:  renderMergeMethod(option.Method),
		Allowed: option.Allowed,
	}
	if option.Message != nil {
		rendered.Message = &forgev1.CommitMessage{
			Headline: option.Message.Headline,
			Body:     option.Message.Body,
		}
	}

	return rendered
}

// readMergeChoice is the method and message a merge asked for, refusing a message without the
// method it is written for.
func readMergeChoice(asked forgev1.MergeMethod, message *forgev1.CommitMessage) (forge.MergeMethod, *forge.CommitMessage, error) {
	var method forge.MergeMethod
	for candidate, rendered := range mergeMethods {
		if rendered == asked {
			method = candidate
		}
	}
	if message == nil {
		return method, nil, nil
	}
	if method == "" {
		return "", nil, forge.ErrNoMergeMethod.Saying("a commit message needs the merge method it is written for")
	}

	return method, &forge.CommitMessage{Headline: message.GetHeadline(), Body: message.GetBody()}, nil
}
