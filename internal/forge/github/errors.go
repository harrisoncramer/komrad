package github

import (
	"fmt"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
)

// noRepository is GitHub answering null for the repository a query's variables name.
func noRepository(variables map[string]any) error {
	return fmt.Errorf("%w: github has no repository %s/%s, which is also its answer for one the credential cannot see", forge.ErrNotFound, variables["owner"], variables["name"])
}

func noPullRequest(number int, owner, name string) error {
	return fmt.Errorf("%w: github has no pull request %d in %s/%s", forge.ErrNotFound, number, owner, name)
}

type mutationRefusal struct {
	sentinel *fail.Error
	verb     string
}

func (r mutationRefusal) explain(answer *graphql.Answer, address string, number int) error {
	for _, failure := range answer.Errors {
		switch failure.Type {
		case "UNPROCESSABLE":
			return r.sentinel.Saying("github will not %s #%d: %s", r.verb, number, failure.Message)
		case "FORBIDDEN":
			return forge.ErrUnauthorized.Saying("github will not let the credential %s %s: %s", r.verb, address, failure.Message)
		}
	}

	return failureOf(answer)
}

// failureOf is the first error the answer carries as komrad's, nil when it carries none.
func failureOf(answer *graphql.Answer) error {
	if len(answer.Errors) == 0 {
		return nil
	}

	failure := answer.Errors[0]
	if failure.Type == "NOT_FOUND" {
		return fmt.Errorf("%w: %s, which is also github's answer for a repository the credential cannot see", forge.ErrNotFound, failure.Message)
	}

	return fmt.Errorf("github refused the query: %s", failure.Message)
}
