// Package main answers which Go packages a branch changed, and how many packages import them.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "komradpkg:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "komradpkg",
		Short:         "Answer which Go packages a branch changed",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newChangedCmd())

	return root
}

func newChangedCmd() *cobra.Command {
	var base, dir, tags string

	changed := &cobra.Command{
		Use:   "changed",
		Short: "Print the packages a branch changed and how many packages import them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := describe(dir, base, tags)
			if err != nil {
				return err
			}

			return writePlan(cmd.OutOrStdout(), plan)
		},
	}

	changed.Flags().StringVar(&base, "base", "origin/main", "the ref to measure the branch against")
	changed.Flags().StringVar(&dir, "dir", ".", "the root of the repository to read")
	changed.Flags().StringVar(&tags, "tags", "integration", "the build tags to read the packages under, as the linter reads them")

	return changed
}
