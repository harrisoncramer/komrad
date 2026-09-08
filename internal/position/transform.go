package position

import (
	"cmp"
	"slices"
)

// TransformParams says which places to move, what the later commit did to the files they sit in,
// and what to do with a place that commit wrote over.
type TransformParams struct {
	// Positions are the places in the earlier commit, returned against by index.
	Positions []Position
	// Mappings are what became of the files, at most one entry per path unless a file was
	// copied, which is the one case a single position comes back twice.
	Mappings []Mapping
	// Policy is what a position whose lines were rewritten becomes.
	Policy Policy
}

// Transform moves every position handed in to where the later commit holds it, resolving paths
// first and only then the lines, so that a rename is settled before any line is counted.
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

// indexMappings gathers the mappings by the path they start from, holding a slice per path since
// a file copied in two places has a mapping for each.
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

	// A file the later commit does not hold leaves nothing to point at, whatever the policy, so
	// what was said about it stays said where it was written.
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

// shiftRange walks a file from the top, carrying how much the regions above have moved the lines
// below them, and refuses a range any of those regions was written over.
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
			// Only an insertion reaches here, since anything with lines of its own inside the
			// range overlapped it, and lines arriving inside the range make it longer rather
			// than moving where it starts.
			end += delta
		default:
			return Range{Start: start, End: end}, true
		}
	}

	return Range{Start: start, End: end}, true
}
