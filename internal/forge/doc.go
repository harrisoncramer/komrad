// Package forge is komrad's boundary with the service that hosts a repository's pull requests.
//
// The [Forge] interface lists what komrad asks of one. It reads pull requests with their checks
// and review threads, merges, sets draft, base and auto-merge, and replies to a thread. The
// github and gitlab packages implement it.
//
// The [Service] connects and disconnects a forge for a host and builds the Forge for that
// connection. Each token sits in a file of its own, and tokens that rotate are renewed before
// they expire. The package also rolls a head's checks up into one standing and names a pull
// request the way its forge does, such as #12 on GitHub and !12 on GitLab.
package forge
