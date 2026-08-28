package commentlength

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"agentslinter/config"

	"golang.org/x/tools/go/analysis"
)

const explainerPrefix = "Explainer:"

// NewAnalyzer builds the comment-length analyzer. A comment says one thing, and a
// comment that has grown to several sentences is usually narrating the change
// rather than explaining the code, so anything longer has to announce itself with
// an Explainer: prefix. Files whose path matches one of the exclude patterns are
// left alone entirely.
func NewAnalyzer(cfg config.CommentLengthConfig) *analysis.Analyzer {
	excluded, compileErr := compileExcludes(cfg.Exclude)

	return &analysis.Analyzer{
		Name: "commentlength",
		Doc:  "Checks that comments are a single sentence unless they are prefixed with Explainer:",
		Run: func(pass *analysis.Pass) (any, error) {
			if compileErr != nil {
				return nil, compileErr
			}
			for _, file := range pass.Files {
				if isExcluded(excluded, pass.Fset.File(file.Pos()).Name()) {
					continue
				}
				for _, group := range file.Comments {
					text := strings.TrimSpace(group.Text())
					if text == "" || strings.HasPrefix(text, explainerPrefix) {
						continue
					}
					if countSentences(text) > 1 {
						pass.Report(analysis.Diagnostic{
							Pos:     group.Pos(),
							Message: fmt.Sprintf("comment must be a single sentence unless it is prefixed with %q", explainerPrefix),
						})
					}
				}
			}
			return nil, nil
		},
	}
}

func compileExcludes(patterns []string) ([]*regexp.Regexp, error) {
	var compiled []*regexp.Regexp
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("comment_length: exclude pattern %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}

	return compiled, nil
}

func isExcluded(patterns []*regexp.Regexp, name string) bool {
	path := filepath.ToSlash(name)
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}

	return false
}

var abbreviations = map[string]bool{
	"e.g.":    true,
	"i.e.":    true,
	"etc.":    true,
	"vs.":     true,
	"cf.":     true,
	"resp.":   true,
	"approx.": true,
	"no.":     true,
	"mr.":     true,
	"ms.":     true,
	"dr.":     true,
	"al.":     true,
}

func countSentences(text string) int {
	sentences := 0
	trailing := false
	for _, field := range strings.Fields(text) {
		if endsSentence(field) {
			sentences++
			trailing = false
			continue
		}
		trailing = true
	}
	if trailing {
		sentences++
	}

	return sentences
}

func endsSentence(field string) bool {
	if strings.Contains(field, "://") {
		return false
	}
	word := strings.TrimRight(field, `)]}"'`+"`")
	if !strings.HasSuffix(word, ".") && !strings.HasSuffix(word, "!") && !strings.HasSuffix(word, "?") {
		return false
	}

	return !abbreviations[strings.ToLower(word)]
}
