package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"weave/cmd/paneldemo/scenario"
	"weave/internal/git"
)

// demoFile is one file of the fake branch and what it says at each round, the first entry being
// what main holds.
type demoFile struct {
	path      string
	revisions []string
}

// buildRepo writes a throwaway repository with a base commit and one commit per round.
func buildRepo(ctx context.Context, at string) (scenario.WithRepoParams, error) {
	if err := os.MkdirAll(at, 0o755); err != nil {
		return scenario.WithRepoParams{}, fmt.Errorf("failed to make %q: %w", at, err)
	}

	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "demo@example.com"},
		{"config", "user.name", "weave demo"},
	} {
		if _, err := git.Run(ctx, at, args...); err != nil {
			return scenario.WithRepoParams{}, err
		}
	}

	files := demoFiles()

	base, err := commitRevision(ctx, at, files, 0, "the code before the change")
	if err != nil {
		return scenario.WithRepoParams{}, err
	}

	if _, err := git.Run(ctx, at, "checkout", "-b", "agent/telescope"); err != nil {
		return scenario.WithRepoParams{}, err
	}

	var snapshots []string

	for round := 1; round < len(files[0].revisions); round++ {
		sha, err := commitRevision(ctx, at, files, round, fmt.Sprintf("round %d", round))
		if err != nil {
			return scenario.WithRepoParams{}, err
		}

		snapshots = append(snapshots, sha)
	}

	return scenario.WithRepoParams{Dir: at, BaseSHA: base, Snapshots: snapshots}, nil
}

// commitRevision writes every file as that round has it and commits the lot, answering with the
// commit it made.
func commitRevision(ctx context.Context, at string, files []demoFile, round int, said string) (string, error) {
	for _, file := range files {
		if round >= len(file.revisions) {
			continue
		}

		path := filepath.Join(at, file.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("failed to make the directory for %q: %w", file.path, err)
		}

		if err := os.WriteFile(path, []byte(file.revisions[round]), 0o644); err != nil {
			return "", fmt.Errorf("failed to write %q: %w", file.path, err)
		}
	}

	if _, err := git.Run(ctx, at, "add", "-A"); err != nil {
		return "", err
	}

	if _, err := git.Run(ctx, at, "commit", "-m", said); err != nil {
		return "", err
	}

	out, err := git.Run(ctx, at, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}

	return firstLineOf(out), nil
}

func firstLineOf(out string) string {
	for i, r := range out {
		if r == '\n' {
			return out[:i]
		}
	}

	return out
}

// demoFiles is the fake change: a band that grows a mode row, and a mode file that arrives with
// the second round.
func demoFiles() []demoFile {
	return []demoFile{
		{
			path: "internal/ui/reviewband.go",
			revisions: []string{
				bandRevision("\t\tm.reviewModeView(),\n"),
				bandRevision("\t\tm.reviewModeView(),\n\t\tm.reviewScopeView(),\n"),
				bandRevision("\t\tm.reviewModeView(m.review.mode),\n\t\tm.reviewScopeView(),\n"),
				bandRevision("\t\tm.reviewModeView(m.review.mode),\n\t\tm.reviewLivenessView(),\n\t\tm.reviewScopeView(),\n"),
			},
		},
		{
			path: "internal/ui/reviewmode.go",
			revisions: []string{
				modeRevision(false),
				modeRevision(false),
				modeRevision(true),
				modeRevision(true),
			},
		},
		{
			path: "docs/review.md",
			revisions: []string{
				"# Reviewing an agent\n\nThe screen draws the diff and the comments beside it.\n",
				"# Reviewing an agent\n\nThe screen draws the diff and the comments beside it.\n",
				"# Reviewing an agent\n\nThe screen is in one of three modes, and it says which.\n",
				"# Reviewing an agent\n\nThe screen is in one of three modes, and it says which.\n\nWatching keeps up with the agent on its own.\n",
			},
		},
	}
}

func bandRevision(rows string) string {
	return "package ui\n\n" +
		"// reviewBandRows is the band's rows in the order they give way.\n" +
		"func (m model) reviewBandRows() []string {\n" +
		"\trows := []string{\n" +
		rows +
		"\t\tm.reviewStatView(),\n" +
		"\t}\n\n" +
		"\treturn rows\n}\n"
}

func modeRevision(threeModes bool) string {
	if !threeModes {
		return "package ui\n\n" +
			"// reviewModeView says whether the reader is looking or writing.\n" +
			"func (m model) reviewModeView() string {\n" +
			"\tif m.review.writing {\n" +
			"\t\treturn m.reviewBandRow(reviewWritingStyle, \"writing a review\")\n" +
			"\t}\n\n" +
			"\treturn m.reviewBandRow(reviewReadingStyle, \"reading\")\n}\n"
	}

	return "package ui\n\n" +
		"// reviewModeView says which of the three the screen is in.\n" +
		"func (m model) reviewModeView(mode reviewMode) string {\n" +
		"\tswitch mode {\n" +
		"\tcase modeEditing:\n" +
		"\t\treturn m.reviewBandRow(reviewEditingStyle, \"editing\")\n" +
		"\tcase modeReviewing:\n" +
		"\t\treturn m.reviewBandRow(reviewWritingStyle, \"reviewing\")\n" +
		"\tcase modeWatching:\n" +
		"\t}\n\n" +
		"\treturn m.reviewWatchingView()\n}\n"
}
