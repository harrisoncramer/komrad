// Command admin is the internal tooling weave is administered with, rather than anything that
// ships with it.
//
// It is a module of its own so that nothing here reaches the binary people install. A task that
// needs more than a shell script can reasonably do reaches for a subcommand here instead of
// growing an inline program in another language.
package main

import (
	"fmt"
	"os"

	"admin/tfvars"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "admin",
		Short:         "Internal tooling for administering weave",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(tfvars.Command())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
