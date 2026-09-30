package diffview

import (
	"slices"
	"strings"

	"komrad/internal/diff"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// TreeRow is one row of the file tree drawn beside the diff.
type TreeRow struct {
	Folder bool
	Depth  int
	Name   string
	Path   string
	Stat   diff.Stat
	Viewed bool
	Counts ThreadCounts
	Row    int
}

// ThreadCounts is how many threads stand in each state.
type ThreadCounts struct {
	Unsent  int
	Unread  int
	Open    int
	Settled int
}

// add counts one thread under the first state it is in.
func (c *ThreadCounts) add(thread *agentv1.ReviewThread) {
	switch {
	case doesHoldDelivery(thread, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT):
		c.Unsent++
	case thread.GetUnreadByHuman():
		c.Unread++
	case thread.GetResolvedAt() == "":
		c.Open++
	default:
		c.Settled++
	}
}

// GetFileTree is the patch's files as a tree, in the order the diff draws them.
func (v *View) GetFileTree() []TreeRow {
	return v.tree
}

// CountThreads counts every thread in the review by the state it stands in.
func (v *View) CountThreads() ThreadCounts {
	var counts ThreadCounts
	for _, thread := range v.threads {
		counts.add(thread)
	}

	return counts
}

// GetTreeFocus is the file a diff row belongs to.
func (v *View) GetTreeFocus(y int) string {
	if thread, ok := v.GetThreadAt(y); ok {
		return v.pathOf(thread)
	}

	file, ok := v.GetDrawnFileAt(y)
	if !ok {
		return ""
	}

	return file.Path()
}

// treeFile is one file on its way into the tree, with the folders still to be opened above it.
type treeFile struct {
	index   int
	folders []string
}

// buildTree lays the files out as a tree in the order the diff draws them.
func (v *View) buildTree() {
	files := make([]treeFile, 0, len(v.patch.Files))
	for index := range v.patch.Files {
		parts := strings.Split(v.patch.Files[index].Path(), "/")
		files = append(files, treeFile{
			index:   index,
			folders: parts[:len(parts)-1],
		})
	}

	v.tree = nil
	v.appendTreeLevel(appendTreeLevelParams{
		files:  files,
		prefix: "",
		depth:  0,
		counts: v.countThreadsByFile(),
	})
}

// appendTreeLevelParams is one folder's worth of files and where it sits.
type appendTreeLevelParams struct {
	files  []treeFile
	prefix string
	depth  int
	counts map[string]ThreadCounts
}

// appendTreeLevel draws one folder's worth of files, each run of files sharing a subfolder
// under a row of its own.
func (v *View) appendTreeLevel(params appendTreeLevelParams) {
	for at := 0; at < len(params.files); {
		current := params.files[at]
		if len(current.folders) == 0 {
			v.appendTreeFile(current.index, params.depth, params.counts)
			at++

			continue
		}

		end := at + 1
		for end < len(params.files) && len(params.files[end].folders) > 0 &&
			params.files[end].folders[0] == current.folders[0] {
			end++
		}

		run := slices.Clone(params.files[at:end])
		name := current.folders[0]
		run = dropFolder(run)

		for isOneFolder(run) {
			name += "/" + run[0].folders[0]
			run = dropFolder(run)
		}

		path := params.prefix + name
		v.tree = append(v.tree, TreeRow{
			Folder: true,
			Depth:  params.depth,
			Name:   name + "/",
			Path:   path,
			Row:    -1,
		})

		v.appendTreeLevel(appendTreeLevelParams{
			files:  run,
			prefix: path + "/",
			depth:  params.depth + 1,
			counts: params.counts,
		})

		at = end
	}
}

// dropFolder takes the folder each file was just filed under off the front of its path.
func dropFolder(files []treeFile) []treeFile {
	return xslice.Map(files, func(file treeFile) treeFile {
		return treeFile{
			index:   file.index,
			folders: file.folders[1:],
		}
	})
}

// isOneFolder reports whether a folder holds one folder and nothing else.
func isOneFolder(files []treeFile) bool {
	if len(files) == 0 || len(files[0].folders) == 0 {
		return false
	}

	for _, file := range files {
		if len(file.folders) == 0 || file.folders[0] != files[0].folders[0] {
			return false
		}
	}

	return true
}

// appendTreeFile draws one file.
func (v *View) appendTreeFile(index, depth int, counts map[string]ThreadCounts) {
	file := v.patch.Files[index]
	path := file.Path()
	header, ok := v.fileHeader(path)
	if !ok {
		header = v.findFirstRow(index)
	}

	v.tree = append(v.tree, TreeRow{
		Depth:  depth,
		Name:   path[strings.LastIndex(path, "/")+1:],
		Path:   path,
		Stat:   file.Stat(),
		Viewed: v.viewed[path],
		Counts: counts[path],
		Row:    header,
	})
}

// findFirstRow is the first row a file draws, for a diff drawn without file headers.
func (v *View) findFirstRow(index int) int {
	for y, current := range v.rows {
		if current.file == index {
			return y
		}
	}

	return -1
}

// countThreadsByFile counts each file's threads by the state they stand in.
func (v *View) countThreadsByFile() map[string]ThreadCounts {
	byFile := map[string]ThreadCounts{}
	for _, thread := range v.threads {
		if thread.GetFilePath() == "" {
			continue
		}

		path := v.pathOf(thread)
		counts := byFile[path]
		counts.add(thread)
		byFile[path] = counts
	}

	return byFile
}
