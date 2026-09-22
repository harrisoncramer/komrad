package bufcheck_test

import (
	"testing"

	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check/checktest"
)

const (
	resourceFile = "resource/v1/resource.proto"
	rowsFile     = "watch/v1/rows.proto"
	rowlessFile  = "resource/v1/rowless.proto"
)

// TestRowPerResource runs the arms that match the option alongside an arm of no resource, a
// resource with no arm, and a field number skipped rather than reserved.
func TestRowPerResource(t *testing.T) {
	t.Parallel()

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata", "../../proto/proto"},
				FilePaths: []string{resourceFile, rowsFile},
			},
			RuleIDs: []string{bufcheck.RowsRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:       bufcheck.RowsRuleID,
				Message:      "resource.v1.Orphan carries (annotation.v1.resource) for the table \"orphans\" and watch.v1.Row has no arm for it, so the watch cannot carry its rows.",
				FileLocation: at(resourceFile, 30, 0, 33, 1),
			},
			{
				RuleID:       bufcheck.RowsRuleID,
				Message:      "resource.v1.Nameless carries (annotation.v1.resource) for the table \"\" and watch.v1.Row has no arm for it, so the watch cannot carry its rows.",
				FileLocation: at(resourceFile, 39, 0, 42, 1),
			},
			{
				RuleID:       bufcheck.RowsRuleID,
				Message:      "resource.v1.Muddled carries (annotation.v1.resource) for the table \"muddled\" and watch.v1.Row has no arm for it, so the watch cannot carry its rows.",
				FileLocation: at(resourceFile, 44, 0, 68, 1),
			},
			{
				RuleID:       bufcheck.RowsRuleID,
				Message:      "watch.v1.Row skips field number 4, so reserve it.",
				FileLocation: at(rowsFile, 6, 0, 13, 1),
			},
			{
				RuleID:       bufcheck.RowsRuleID,
				Message:      "\"bare\" is an arm of watch.v1.Row and is not a message carrying (annotation.v1.resource), so nothing says which table it is a row of.",
				FileLocation: at(rowsFile, 10, 4, 10, 30),
			},
		},
	}.Run(t)
}

// TestRowlessResource covers a resource declared where no Row exists to carry it.
func TestRowlessResource(t *testing.T) {
	t.Parallel()

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata", "../../proto/proto"},
				FilePaths: []string{rowlessFile},
			},
			RuleIDs: []string{bufcheck.RowsRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:       bufcheck.RowsRuleID,
				Message:      "resource.v1.Unrowed carries (annotation.v1.resource) and there is no watch.v1.Row to carry it on the watch.",
				FileLocation: at(rowlessFile, 6, 0, 9, 1),
			},
		},
	}.Run(t)
}

// TestResourceNamesItsTable runs a well-formed option alongside each way of getting one wrong.
func TestResourceNamesItsTable(t *testing.T) {
	t.Parallel()

	muddled := func(message string) checktest.ExpectedAnnotation {
		return checktest.ExpectedAnnotation{
			RuleID:       bufcheck.ResourceRuleID,
			Message:      message,
			FileLocation: at(resourceFile, 44, 0, 68, 1),
		}
	}

	checktest.CheckTest{
		Request: &checktest.RequestSpec{
			Files: &checktest.ProtoFileSpec{
				DirPaths:  []string{"testdata", "../../proto/proto"},
				FilePaths: []string{resourceFile},
			},
			RuleIDs: []string{bufcheck.ResourceRuleID},
		},
		Spec: bufcheck.Spec,
		ExpectedAnnotations: []checktest.ExpectedAnnotation{
			{
				RuleID:       bufcheck.ResourceRuleID,
				Message:      "resource.v1.Nameless carries (annotation.v1.resource) with no table, so nothing says which rows it is.",
				FileLocation: at(resourceFile, 39, 0, 42, 1),
			},
			muddled("resource.v1.Muddled projects \"both_ways\" with neither a column nor a lookup, or with both; a projected row finds its resource one way."),
			muddled("resource.v1.Muddled projects \"both_ways_too\" with neither a column nor a lookup, or with both; a projected row finds its resource one way."),
			muddled("resource.v1.Muddled projects \"half_lookup\" by lookup, which names both lookup, the column on \"muddled\", and via, the column on the projected row."),
			muddled("resource.v1.Muddled projects \"narrowed\" by column and gives a where, which only a lookup runs."),
			muddled("resource.v1.Muddled projects a table with no name."),
		},
	}.Run(t)
}

// at is a location as checktest counts it, from zero.
func at(file string, startLine, startColumn, endLine, endColumn int) *checktest.ExpectedFileLocation {
	return &checktest.ExpectedFileLocation{
		FileName:    file,
		StartLine:   startLine,
		StartColumn: startColumn,
		EndLine:     endLine,
		EndColumn:   endColumn,
	}
}
