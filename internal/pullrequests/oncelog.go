package pullrequests

// The kinds of thing the syncers say once.
const (
	saidUnconnected = "unconnected"
	saidMissing     = "missing"
	saidRefused     = "refused"
	saidRenamed     = "renamed"
	saidTruncated   = "truncated"
)

// onceLog remembers what has been said once already, by what kind of thing it was about and
// which one.
type onceLog map[string]bool

// first says whether nothing has been said about the key yet, and remembers that it has now.
func (l onceLog) first(kind, key string) bool {
	if l[kind+":"+key] {
		return false
	}

	l[kind+":"+key] = true

	return true
}

// forget lets the key be said about again.
func (l onceLog) forget(kind, key string) {
	delete(l, kind+":"+key)
}
