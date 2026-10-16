package forge

// MergeMethod is how a pull request's commits land on its base.
type MergeMethod string

const (
	// MergeMethodMerge lands the commits under a merge commit.
	MergeMethodMerge MergeMethod = "merge"
	// MergeMethodSquash lands the commits as one new commit.
	MergeMethodSquash MergeMethod = "squash"
	// MergeMethodRebase replays the commits onto the base one by one.
	MergeMethodRebase MergeMethod = "rebase"
	// MergeMethodFastForward moves the base to the head without a commit of its own.
	MergeMethodFastForward MergeMethod = "fast_forward"
)

// CommitMessage is the text of the commit a merge writes.
type CommitMessage struct {
	Headline string
	Body     string
}

// MergeOption is one way a pull request could merge.
type MergeOption struct {
	Method MergeMethod
	// Allowed says whether the repository and its base branch allow the method.
	Allowed bool
	// Message is what the forge writes for the method by itself, nil for a method without one.
	Message *CommitMessage
}

// MergePlan is the ways a pull request could merge and the head they were read at.
type MergePlan struct {
	Options []MergeOption
	// DefaultMethod is the method the forge merges with when asked for none.
	DefaultMethod MergeMethod
	HeadSHA       string
}

// MergeParams is the pull request to merge and how.
type MergeParams struct {
	URL string
	// Method is how to merge, empty for the plan's default method.
	Method MergeMethod
	// Headline is the first line of the commit, nil to leave it to the forge.
	Headline *string
	// Body is the rest of the commit, nil to leave it to the forge.
	Body *string
	// HeadSHA is the commit the person saw, which the forge refuses to merge once the branch
	// has moved past it, empty to merge whatever the head is.
	HeadSHA string
}
