package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// printTable writes the rows under the header, columns aligned.
func printTable(out io.Writer, header []string, rows [][]string) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, row := range append([][]string{header}, rows...) {
		if _, err := fmt.Fprintln(w, strings.Join(row, "\t")); err != nil {
			return err
		}
	}

	return w.Flush()
}
