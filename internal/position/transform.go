package position

import (
	"cmp"
	"slices"
)

// TransformParams says which places to move, what happened to their files, and what to do with a
// place the later commit wrote over.
type TransformParams struct {
	// Positions are the places in the earlier commit, answered for by index.
	Positions []Position
	// Mappings are what became of the files, with a second entry for a path only when a file was
	// copied.
	Mappings []Mapping
	// Policy is what a position whose lines were rewritten becomes.
	Policy Policy
}

// Transform moves every position to where the later commit holds it, resolving paths before
// lines.
func Transform(params TransformParams) []Result {
	byPath := indexMappings(params.Mappings)

	results := make([]Result, 0, len(params.Positions))
	for index, current := range params.Positions {
		mappings, ok := byPath[current.Path]
		if !ok {
			results = append(results, Result{
				Index:    index,
				Position: current,
				Outcome:  Placed,
			})

			continue
		}

		for _, mapping := range mappings {
			results = append(results, transformOne(transformOneParams{
				Index:    index,
				Position: current,
				Mapping:  mapping,
				Policy:   params.Policy,
			}))
		}
	}

	return results
}

// indexMappings gathers the mappings by the path they start from.
func indexMappings(mappings []Mapping) map[string][]Mapping {
	byPath := map[string][]Mapping{}
	for _, mapping := range mappings {
		byPath[mapping.File.OldPath] = append(byPath[mapping.File.OldPath], mapping)
	}

	return byPath
}

// transformOneParams is one position against one of the mappings its file has.
type transformOneParams struct {
	Index    int
	Position Position
	Mapping  Mapping
	Policy   Policy
}

func transformOne(params transformOneParams) Result {
	dropped := Result{
		Index:    params.Index,
		Position: params.Position,
		Outcome:  Dropped,
	}

	// A file the later commit does not hold leaves nothing to point at, whatever the policy.
	if params.Mapping.File.IsDeleted() {
		return dropped
	}

	moved := Position{Path: params.Mapping.File.NewPath}

	if params.Position.IsFileLevel() {
		return Result{
			Index:    params.Index,
			Position: moved,
			Outcome:  Placed,
		}
	}

	if !params.Position.Lines.IsValid() {
		return dropped
	}

	lines, ok := shiftRange(params.Mapping.Ranges, params.Position.Lines)
	if !ok {
		if params.Policy == FileOnConflict {
			return Result{
				Index:    params.Index,
				Position: moved,
				Outcome:  FileOnly,
			}
		}

		return dropped
	}

	moved.Lines = lines

	return Result{
		Index:    params.Index,
		Position: moved,
		Outcome:  Placed,
	}
}

// shiftRange carries the shift the regions above a range apply to it, and refuses a range any of
// them wrote over.
func shiftRange(mappings []RangeMapping, lines Range) (Range, bool) {
	ordered := slices.Clone(mappings)
	slices.SortFunc(ordered, func(a, b RangeMapping) int {
		return cmp.Compare(a.Old.Start, b.Old.Start)
	})

	start, end := lines.Start, lines.End
	for _, mapping := range ordered {
		if lines.doesOverlap(mapping.Old) {
			return Range{}, false
		}

		delta := mapping.New.End - mapping.New.Start - (mapping.Old.End - mapping.Old.Start)

		switch {
		case mapping.Old.End <= lines.Start:
			start, end = start+delta, end+delta
		case mapping.Old.Start < lines.End:
			// Only an insertion reaches here, and lines arriving inside a range lengthen it.
			end += delta
		default:
			return Range{Start: start, End: end}, true
		}
	}

	return Range{Start: start, End: end}, true
}
