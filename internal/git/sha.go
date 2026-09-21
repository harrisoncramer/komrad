package git

// shortSHALength is how much of a commit hash a record names one by.
const shortSHALength = 12

// ShortSHA is a commit hash cut to the length a record names one by.
func ShortSHA(sha string) string {
	if len(sha) <= shortSHALength {
		return sha
	}

	return sha[:shortSHALength]
}
