// Package reader walks a cursor through rows that are already rendered. It keeps the window that
// renders the cursor's page, and the rows a search found in it.
//
// The root's doc reader, the checks and review screens and the diff view import reader. It imports
// only the cursor, paint and label packages, and never the root or a domain.
package reader
