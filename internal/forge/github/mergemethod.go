package github

import "slices"

// mergeMethodOrder is the order GitHub offers merge methods in.
var mergeMethodOrder = []string{"MERGE", "SQUASH", "REBASE"}

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
	Rules struct {
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

// methodFor is the credential's default merge method when the base branch allows it, and
// otherwise the first method the repository and the branch's rules allow.
func (s mergeSettings) methodFor(base *baseRef) string {
	allowed := map[string]bool{
		"MERGE":  s.MergeCommitAllowed,
		"SQUASH": s.SquashMergeAllowed,
		"REBASE": s.RebaseMergeAllowed,
	}
	if base != nil {
		if base.BranchProtectionRule != nil && base.BranchProtectionRule.RequiresLinearHistory {
			allowed["MERGE"] = false
		}
		for _, rule := range base.Rules.Nodes {
			switch {
			case rule.Type == "REQUIRED_LINEAR_HISTORY":
				allowed["MERGE"] = false
			case rule.Type == "PULL_REQUEST" && rule.Parameters != nil && len(rule.Parameters.AllowedMergeMethods) > 0:
				for method := range allowed {
					allowed[method] = allowed[method] && slices.Contains(rule.Parameters.AllowedMergeMethods, method)
				}
			}
		}
	}

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
