// Package run stores a record of each script komrad runs for an agent, with the moment that
// started it, where its log went and why it failed. A failed run stays unread until a caller
// marks it read, and an agent keeps only as many runs as it keeps log files.
package run
