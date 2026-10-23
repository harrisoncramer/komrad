// Package bufcheck holds the custom buf lint and breaking rules that komrad's own protobufs must
// pass, such as every read being marked idempotent, one service per file, and a field's validation
// never tightening. The buf-plugin-komrad command serves them to buf.
package bufcheck
