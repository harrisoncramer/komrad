// Package main answers which Go packages a branch changed, and how many packages import them.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "weavepkg:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "changed" {
		return fmt.Errorf("usage: weavepkg changed [--base ref] [--dir root]")
	}

	flags := flag.NewFlagSet("changed", flag.ContinueOnError)
	base := flags.String("base", "origin/main", "the ref to measure the branch against")
	root := flags.String("dir", ".", "the root of the repository to read")
	tags := flags.String("tags", "integration", "the build tags to read the packages under, as the linter reads them")

	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	plan, err := describe(*root, *base, *tags)
	if err != nil {
		return err
	}

	return writePlan(out, plan)
}
