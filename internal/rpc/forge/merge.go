package forge

import (
	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

func renderMergeOption(option forge.MergeOption) *forgev1.MergeOption {
	rendered := &forgev1.MergeOption{
		Method:  option.Method,
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

// requireMergeMethod refuses a commit message without the merge method it is written for.
func requireMergeMethod(asked forgev1.MergeMethod, headline, body *string) error {
	if asked == forgev1.MergeMethod_MERGE_METHOD_UNSPECIFIED && (headline != nil || body != nil) {
		return forge.ErrNoMergeMethod.Saying("a commit message needs the merge method it is written for")
	}

	return nil
}
