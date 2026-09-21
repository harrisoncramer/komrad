package weavelint

import (
	"bytes"
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const noMiseDoc = "Checks that neither the Go source nor the pages a user reads name mise, " +
	"so nothing weave prints or documents sends a person who installed the binary to a task that only exists in a checkout."

// rootPackage is the package whose directory holds the README and docs the rule reads.
const rootPackage = "weave"

var miseWord = regexp.MustCompile(`(?:^|[^A-Za-z])(mise|Mise|MISE)(?:[^a-z]|$)`)

// newNoMise builds the rule that the word mise appears nowhere a user of the binary reads.
func newNoMise(settings config.NoMiseConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "no_mise",
		Doc:  noMiseDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				if err := reportMiseMentions(pass, file); err != nil {
					return nil, err
				}
			}

			if pass.Pkg.Path() != rootPackage || len(pass.Files) == 0 {
				return nil, nil
			}

			root := filepath.Dir(pass.Fset.Position(pass.Files[0].Pos()).Filename)

			return nil, reportPageMentions(pass, root, excluded)
		},
	}, nil
}

// reportMiseMentions reports every place a file's text says mise, in a comment, a string or a name.
func reportMiseMentions(pass *analysis.Pass, file *ast.File) error {
	tokenFile := pass.Fset.File(file.Pos())

	source, err := pass.ReadFile(tokenFile.Name())
	if err != nil {
		return err
	}

	for _, match := range miseWord.FindAllSubmatchIndex(source, -1) {
		pass.Reportf(tokenFile.Pos(match[2]), "names the task runner: a person with the binary has no checkout to run it in, so name what weave itself does")
	}

	return nil
}

// reportPageMentions reports every mention on the README and the docs pages under root, each
// pinned to the root package's first file.
func reportPageMentions(pass *analysis.Pass, root string, excluded []*regexp.Regexp) error {
	pages, err := listPages(root)
	if err != nil {
		return err
	}

	for _, page := range pages {
		if isPathUnder(page, excluded) {
			continue
		}

		source, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", page, err)
		}

		for _, match := range miseWord.FindAllSubmatchIndex(source, -1) {
			line := 1 + bytes.Count(source[:match[2]], []byte("\n"))
			pass.Reportf(pass.Files[0].Pos(), "%s:%d names the task runner: a person with the binary has no checkout to run it in, so say what weave itself does, or move the page under docs/internal", page, line)
		}
	}

	return nil
}

// listPages is the README and every markdown page under docs, relative to root.
func listPages(root string) ([]string, error) {
	pages := []string{}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err == nil {
		pages = append(pages, "README.md")
	}

	docs := filepath.Join(root, "docs")
	if _, err := os.Stat(docs); err != nil {
		return pages, nil
	}

	err := filepath.WalkDir(docs, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		pages = append(pages, filepath.ToSlash(relative))

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk %s: %w", docs, err)
	}

	return pages, nil
}

// isPathUnder says whether a repo-relative path matches any of the patterns.
func isPathUnder(path string, patterns []*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}

	return false
}
