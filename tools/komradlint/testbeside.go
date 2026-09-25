package komradlint

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"

	"komradlint/config"

	"golang.org/x/tools/go/analysis"
)

const testBesideDoc = "Checks that a test file sits beside the source file it is named for, " +
	"so a test is found where the code it tests is rather than under the change that added it."

// testSuffixes are the endings a test file may carry after its source file's name.
var testSuffixes = []string{"_internal", "_integration", "_fuzz"}

// newTestBeside builds the rule that a test file has the source file it is named for beside it.
func newTestBeside(settings config.BesideConfig) (*analysis.Analyzer, error) {
	excluded, err := compile(settings.Exclude)
	if err != nil {
		return nil, err
	}

	return &analysis.Analyzer{
		Name: "test_beside",
		Doc:  testBesideDoc,
		Run: func(pass *analysis.Pass) (any, error) {
			if settings.Skip {
				return nil, nil
			}

			for _, file := range pass.Files {
				if !isTest(pass, file) || isUnder(pass, file, excluded) {
					continue
				}

				reportTestBeside(pass, file)
			}

			return nil, nil
		},
	}, nil
}

// reportTestBeside reports a test file whose source file is missing.
func reportTestBeside(pass *analysis.Pass, file *ast.File) {
	path := pass.Fset.Position(file.Pos()).Filename
	source := strings.TrimSuffix(filepath.Base(path), "_test.go")
	for _, suffix := range testSuffixes {
		source = strings.TrimSuffix(source, suffix)
	}

	source += ".go"
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), source)); err == nil {
		return
	}

	pass.Reportf(file.Name.Pos(), "%s has no %s beside it; name a test file for the source file it tests", filepath.Base(path), source)
}
