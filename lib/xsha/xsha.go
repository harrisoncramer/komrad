// Package xsha holds helpers for commit hashes.
package xsha

// shortLength is how much of a commit hash a record names one by.
const shortLength = 12

// Short is a commit hash cut to the length a record names one by.
func Short(sha string) string {
	if len(sha) <= shortLength {
		return sha
	}

	return sha[:shortLength]
}
