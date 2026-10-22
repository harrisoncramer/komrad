package history

import (
	"context"
	"fmt"

	"komrad/internal/git"
	"komrad/internal/position"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

type rangeMapper interface {
	MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error)
}

type Anchor struct {
	ID       uuid.UUID
	Position position.Position
}

type Place struct {
	FilePath string
	NewStart int32
	FileOnly bool
	Exact    bool
}

type Tracer struct {
	git rangeMapper
}

// NewTracer builds a tracer over the git that maps ranges between snapshots.
func NewTracer(git rangeMapper) *Tracer {
	return &Tracer{git: git}
}

type TracePairParams struct {
	Dir     string
	FromSHA string
	ToSHA   string
	Anchors []Anchor
}

// TracePair moves every anchor of one snapshot pair, keeping the file when the lines are gone.
func (t *Tracer) TracePair(ctx context.Context, params TracePairParams) (map[uuid.UUID]Place, error) {
	mappings, err := t.git.MapRanges(ctx, git.MapRangesParams{
		Dir:     params.Dir,
		FromSHA: params.FromSHA,
		ToSHA:   params.ToSHA,
		Paths: xslice.Map(params.Anchors, func(anchor Anchor) string {
			return anchor.Position.Path
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to map ranges %s..%s: %w", params.FromSHA, params.ToSHA, err)
	}

	results := position.Transform(position.TransformParams{
		Positions: xslice.Map(params.Anchors, func(anchor Anchor) position.Position {
			return anchor.Position
		}),
		Mappings: mappings,
		Policy:   position.FileOnConflict,
	})

	places := map[uuid.UUID]Place{}
	for _, result := range results {
		if result.Index < 0 || result.Index >= len(params.Anchors) {
			continue
		}

		anchor := params.Anchors[result.Index]

		// Keep git's first answer for a file copied to two paths, not whichever copy it names last.
		if _, held := places[anchor.ID]; held {
			continue
		}

		places[anchor.ID] = placeOf(anchor, result)
	}

	return places, nil
}

func placeOf(anchor Anchor, result position.Result) Place {
	path := result.Position.Path
	if path == "" {
		path = anchor.Position.Path
	}

	if result.Outcome != position.Placed || result.Position.IsFileLevel() {
		return Place{FilePath: path, FileOnly: true}
	}

	return Place{FilePath: path, NewStart: int32(result.Position.Lines.Start)}
}
