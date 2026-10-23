// Package checks holds the screen that lists the status checks on the newest head of an agent's
// pull request, and the screen that reads their logs. A row of scopes narrows the checks listed,
// and the block beside the list shows the steps of the check under the cursor. The logs screen
// reads a check's log in chunks and follows the row under the cursor.
//
// The root is the host. It opens these screens and renders their lists from the parameters they
// hand it. The checks screens reach the host only through the requests they return, and they never
// import the root or another domain.
package checks
