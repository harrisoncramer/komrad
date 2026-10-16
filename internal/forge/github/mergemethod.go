package github

import (
	"slices"

	"komrad/internal/forge"
)

// mergeMethodOrder is the order GitHub offers merge methods in.
var mergeMethodOrder = []string{"MERGE", "SQUASH", "REBASE"}

// mergeMethodOf is each of GitHub's merge methods in komrad's words.
var mergeMethodOf = map[string]forge.MergeMethod{
	"MERGE":  forge.MergeMethodMerge,
	"SQUASH": forge.MergeMethodSquash,
	"REBASE": forge.MergeMethodRebase,
}

// mergeSettings is the merge methods a repository allows and the one the credential defaults to.
type mergeSettings struct {
	ViewerDefaultMergeMethod string `json:"viewerDefaultMergeMethod"`
	MergeCommitAllowed       bool   `json:"mergeCommitAllowed"`
	SquashMergeAllowed       bool   `json:"squashMergeAllowed"`
	RebaseMergeAllowed       bool   `json:"rebaseMergeAllowed"`
}

// baseRef is a pull request's base branch with the protection and rules GitHub enforces on it.
type baseRef struct {
	BranchProtectionRule *branchProtection `json:"branchProtectionRule"`
	Rules                struct {
		Nodes []branchRule `json:"nodes"`
	} `json:"rules"`
}

// branchProtection is the classic protection on a branch.
type branchProtection struct {
	RequiresLinearHistory bool `json:"requiresLinearHistory"`
}

// branchRule is one rule GitHub enforces on a branch.
type branchRule struct {
	Type       string          `json:"type"`
	Parameters *ruleParameters `json:"parameters"`
}

// ruleParameters is the merge methods a pull request rule allows.
type ruleParameters struct {
	AllowedMergeMethods []string `json:"allowedMergeMethods"`
}

// allowedMethods is whether the repository and base branch allow each of GitHub's merge methods.
func (s mergeSettings) allowedMethods(base *baseRef) map[string]bool {
	return map[string]bool{
		"MERGE":  s.MergeCommitAllowed && base.isMethodAllowed("MERGE"),
		"SQUASH": s.SquashMergeAllowed && base.isMethodAllowed("SQUASH"),
		"REBASE": s.RebaseMergeAllowed && base.isMethodAllowed("REBASE"),
	}
}

// chooseMethod returns the credential's default merge method when the repository and base
// branch allow it, and otherwise the first method they allow.
func (s mergeSettings) chooseMethod(base *baseRef) string {
	allowed := s.allowedMethods(base)
	if allowed[s.ViewerDefaultMergeMethod] {
		return s.ViewerDefaultMergeMethod
	}
	for _, method := range mergeMethodOrder {
		if allowed[method] {
			return method
		}
	}

	return s.ViewerDefaultMergeMethod
}

// methodFor is GitHub's name for the method asked for, or the default method when none was.
func (s mergeSettings) methodFor(base *baseRef, asked forge.MergeMethod) (string, error) {
	if asked == "" {
		return s.chooseMethod(base), nil
	}
	for _, method := range mergeMethodOrder {
		if mergeMethodOf[method] == asked {
			return method, nil
		}
	}

	return "", forge.ErrNoMergeMethod.Saying("github cannot merge by %s", asked)
}

func (b *baseRef) isMethodAllowed(method string) bool {
	if b == nil {
		return true
	}
	if method == "MERGE" && b.BranchProtectionRule != nil && b.BranchProtectionRule.RequiresLinearHistory {
		return false
	}
	for _, rule := range b.Rules.Nodes {
		if method == "MERGE" && rule.Type == "REQUIRED_LINEAR_HISTORY" {
			return false
		}
		if rule.Type == "PULL_REQUEST" && rule.Parameters != nil && len(rule.Parameters.AllowedMergeMethods) > 0 && !slices.Contains(rule.Parameters.AllowedMergeMethods, method) {
			return false
		}
	}

	return true
}
