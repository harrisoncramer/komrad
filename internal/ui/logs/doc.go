// Package logs holds the screen that lists the sources an agent's logs can be read from, or the
// runs of every agent on a task. The screen tails the source under the cursor beside the list, and
// each read picks up where the last one left off. A pattern typed at the foot of the log narrows
// its lines, and a format strip renders komrad's server log compact or raw. A person can walk the
// log with a cursor and copy the line under it.
//
// The root is the host. It opens the screen and renders its list from the parameters the screen
// hands it. The logs screen reaches the host only through the requests it returns, and it never
// imports the root or another domain.
package logs
