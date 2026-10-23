package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "komradexport:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var fix, methods bool
	var tags string
	var keepFiles []string

	root := &cobra.Command{
		Use:           "komradexport [packages]",
		Short:         "Report exported identifiers that nothing outside their package uses",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = []string{"./..."}
			}

			return run(cmd.OutOrStdout(), runOptions{
				patterns:  args,
				tags:      tags,
				keepFiles: keepFiles,
				fix:       fix,
				methods:   methods,
			})
		},
	}

	root.Flags().BoolVar(&fix, "fix", false, "rename what it finds instead of reporting it")
	root.Flags().BoolVar(&methods, "methods", false, "check methods too, which may satisfy an interface asserted only at run time")
	root.Flags().StringVar(&tags, "tags", "integration,rankprobe", "the build tags to read the packages under")
	root.Flags().StringArrayVar(&keepFiles, "keep", nil, "a file whose words name identifiers to leave exported, such as a linter config")

	return root
}

type runOptions struct {
	patterns  []string
	tags      string
	keepFiles []string
	fix       bool
	methods   bool
}

func run(out io.Writer, options runOptions) error {
	program, err := load(options.patterns, options.tags)
	if err != nil {
		return err
	}

	keep, err := keptNames(options.keepFiles)
	if err != nil {
		return err
	}

	generators, err := generatorUses(program)
	if err != nil {
		return err
	}

	found := find(program, keep, generators, options.methods)
	if options.fix {
		if err := rename(program, found); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(out, "unexported %d identifiers\n", len(found)); err != nil {
			return fmt.Errorf("failed to report: %w", err)
		}

		return nil
	}

	for _, c := range found {
		position := program.fset.Position(c.obj.Pos())
		if _, err := fmt.Fprintf(out, "%s: %s is only used inside %s, name it %s\n", position, c.qualifiedName(), c.obj.Pkg().Path(), c.newName); err != nil {
			return fmt.Errorf("failed to report: %w", err)
		}
	}

	if len(found) > 0 {
		return fmt.Errorf("%d exported identifiers are only used inside their own package, run 'mise run go:export --fix'", len(found))
	}

	return nil
}
