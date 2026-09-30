package config

import (
	"fmt"
	"reflect"

	"komrad/internal/glyph"
	"komrad/internal/loader"
)

// Glyphs are the icons the panel draws, grouped by where each one is drawn.
type Glyphs struct {
	// The spinner drawn while something is loading, one of braille, arcs or ascii.
	Loader string `mapstructure:"loader"`
	// The repo column of the sidebar, which says where an agent's own checkout stands.
	Row RowGlyphs `mapstructure:"row"`
	// The forge column of the sidebar, which says where the pull request from an agent's branch stands.
	PullRequest PullRequestGlyphs `mapstructure:"pull_request"`
	// The marks drawn after a row's name in the sidebar and beside what they say in the details.
	Marks MarkGlyphs `mapstructure:"marks"`
	// The titles over the panel's lists, and the prompt and cursor they draw.
	Lists ListGlyphs `mapstructure:"lists"`
	// The review screen: the marks on its threads and files, and how far a comment has got.
	Review ReviewGlyphs `mapstructure:"review"`
	// The messages the panel shows, one for each kind.
	Messages MessageGlyphs `mapstructure:"messages"`
	// The schedules list, one for a schedule that runs and one for a schedule that is paused.
	Schedules ScheduleGlyphs `mapstructure:"schedules"`
}

// RowGlyphs are drawn in the repo column of the sidebar.
type RowGlyphs struct {
	// An agent with a checkout of its own, drawn in a warning colour while a rebase is stopped in
	// it, and the title over the worktrees list.
	Worktree string `mapstructure:"worktree"`
	// An agent whose checkout no longer exists.
	WorktreeGone string `mapstructure:"worktree_gone"`
}

// PullRequestGlyphs are drawn in the forge column of the sidebar.
type PullRequestGlyphs struct {
	// An open pull request, and an approved one that cannot merge yet.
	Open string `mapstructure:"open"`
	// A pull request a reviewer has asked for changes on.
	ChangesRequested string `mapstructure:"changes_requested"`
	// A draft pull request.
	Draft string `mapstructure:"draft"`
	// A pull request the forge would merge now.
	Approved string `mapstructure:"approved"`
	// A merged pull request.
	Merged string `mapstructure:"merged"`
	// A pull request closed without merging.
	Closed string `mapstructure:"closed"`
	// A pull request with a failing check.
	Failing string `mapstructure:"failing"`
	// A pull request that no longer merges cleanly.
	Conflicting string `mapstructure:"conflicting"`
}

// MarkGlyphs are drawn after a row's name in the sidebar.
type MarkGlyphs struct {
	// A script komrad fired for the agent failed and its log is unread.
	FailedScript string `mapstructure:"failed_script"`
	// Mail is waiting that the agent has not read.
	Mail string `mapstructure:"mail"`
	// The agent is paused, held by a person reading its worktree.
	Paused string `mapstructure:"paused"`
	// The row has been hidden from the sidebar.
	Hidden string `mapstructure:"hidden"`
	// A schedule launches into the workflow, and the title over the schedules list.
	Schedule string `mapstructure:"schedule"`
	// The agent has written documents for a person.
	Document string `mapstructure:"document"`
	// Beside the count of comments reviewer agents left on the agent's work.
	ReviewerComments string `mapstructure:"reviewer_comments"`
	// Beside the count of comments you left on the agent's work.
	PersonComments string `mapstructure:"person_comments"`
	// Your comments on the agent's work hold something you have not read.
	Unread string `mapstructure:"unread"`
}

// ListGlyphs are drawn in the titles over the panel's lists.
type ListGlyphs struct {
	// The title over the agents list.
	Workflow string `mapstructure:"workflow"`
	// The title over the archive.
	Archive string `mapstructure:"archive"`
	// The title over the transcript search.
	Search string `mapstructure:"search"`
	// The prompt of the filter over a list.
	Filter string `mapstructure:"filter"`
	// The dot beside the engine, coloured by whether it answers.
	Engine string `mapstructure:"engine"`
}

// ReviewGlyphs are drawn on the review screen.
type ReviewGlyphs struct {
	// A thread holding something you have not read.
	Unread string `mapstructure:"unread"`
	// A thread nobody has resolved, with nothing new in it.
	Read string `mapstructure:"read"`
	// A thread that has been resolved.
	Settled string `mapstructure:"settled"`
	// A thread holding a comment nobody has sent yet.
	Unsent string `mapstructure:"unsent"`
	// The header of a file you have read and folded away.
	Viewed string `mapstructure:"viewed"`
	// A comment the agent has been given.
	Delivered string `mapstructure:"delivered"`
	// A comment held back until you send it.
	Held string `mapstructure:"held"`
}

// MessageGlyphs are drawn before each message the panel shows.
type MessageGlyphs struct {
	// Something failed.
	Error string `mapstructure:"error"`
	// Something needs your attention.
	Warning string `mapstructure:"warning"`
	// Something finished as asked.
	Success string `mapstructure:"success"`
	// Something to know.
	Info string `mapstructure:"info"`
}

// ScheduleGlyphs are drawn beside each schedule in the schedules list.
type ScheduleGlyphs struct {
	// A schedule that runs.
	Enabled string `mapstructure:"enabled"`
	// A schedule that is paused.
	Disabled string `mapstructure:"disabled"`
}

// DefaultGlyphs are the icons used when the config omits them, read from the glyph package.
func DefaultGlyphs() Glyphs {
	glyphs := Glyphs{Loader: string(loader.Braille)}
	defaults := glyph.Defaults()

	eachGlyphField(&glyphs, func(key string, field reflect.Value) {
		g, ok := glyph.ByKey(key)
		if !ok {
			panic(fmt.Sprintf("glyphs.%s names no glyph the panel draws", key))
		}

		field.SetString(defaults.Spell(g))
	})

	return glyphs
}

// Set is the glyph set the panel draws with.
func (g Glyphs) Set() (glyph.Set, error) {
	overrides := map[glyph.Glyph]string{}

	var err error
	eachGlyphField(&g, func(key string, field reflect.Value) {
		if err != nil {
			return
		}

		found, ok := glyph.ByKey(key)
		if !ok {
			err = fmt.Errorf("glyphs.%s names no glyph the panel draws", key)

			return
		}

		overrides[found] = field.String()
	})
	if err != nil {
		return glyph.Set{}, err
	}

	set, err := glyph.Load(overrides)
	if err != nil {
		return glyph.Set{}, fmt.Errorf("failed to load the glyphs: %w", err)
	}

	return set, nil
}

// validateGlyphs refuses a glyph outside the approved set, naming the key it was written under.
func validateGlyphs(glyphs Glyphs) error {
	if _, err := loader.Parse(glyphs.Loader); err != nil {
		return fmt.Errorf("glyphs.loader: %w", err)
	}

	var err error
	eachGlyphField(&glyphs, func(key string, field reflect.Value) {
		if err != nil {
			return
		}
		if checked := glyph.Check(field.String()); checked != nil {
			err = fmt.Errorf("glyphs.%s: %w", key, checked)
		}
	})

	return err
}

// eachGlyphField calls visit with every field of the glyphs and the key it sits at, its group
// and its name as the config spells them.
func eachGlyphField(glyphs *Glyphs, visit func(key string, field reflect.Value)) {
	groups := reflect.ValueOf(glyphs).Elem()
	for i := range groups.NumField() {
		group := groups.Field(i)
		if group.Kind() != reflect.Struct {
			continue
		}

		groupName := groups.Type().Field(i).Tag.Get("mapstructure")

		for j := range group.NumField() {
			fieldName := group.Type().Field(j).Tag.Get("mapstructure")
			visit(groupName+"."+fieldName, group.Field(j))
		}
	}
}
