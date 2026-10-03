package gitlab

import (
	"fmt"
	"strings"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
)

// failureOf is the first error the answer carries as komrad's, nil when it carries none.
func failureOf(answer *graphql.Answer) error {
	if len(answer.Errors) == 0 {
		return nil
	}

	failure := answer.Errors[0]
	if strings.Contains(failure.Message, "exceeds max complexity") {
		return fmt.Errorf("%w: %s", errTooComplex, failure.Message)
	}

	return fmt.Errorf("gitlab refused the query: %s", failure.Message)
}

func noProject(path string) error {
	return fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, path)
}

// noMergeRequest says GitLab has no such merge request, which is also its answer for a project the credential cannot see.
func noMergeRequest(number int, path string) error {
	return fmt.Errorf("%w: gitlab has no merge request %d in %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, number, path)
}
