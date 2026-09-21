package weavelint

import (
	"go/ast"
	"regexp"

	"weavelint/config"

	"golang.org/x/tools/go/analysis"
)

const noMiseDoc = "Checks that no Go source names mise, " +
	"so nothing weave prints or documents sends a person who installed the binary to a task that only exists in a checkout."

var miseWord = regexp.MustCompile(`(?:^|[^A-Za-z])(mise|Mise|MISE)(?:[^a-z]|$)`)

// newNoMise builds the rule that the word mise appears nowhere in a source file.
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

			return nil, nil
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
