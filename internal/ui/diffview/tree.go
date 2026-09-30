package diffview

import (
	"cmp"
	"slices"
	"strings"

	"komrad/internal/diff"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// TreeRowKind is what one row of the file tree stands for.
type TreeRowKind uint8

const (
	TreeFolder TreeRowKind = iota
	TreeFile
	TreeThread
)

// TreeRow is one row of the file tree drawn beside the diff.
type TreeRow struct {
	Kind   TreeRowKind
	Depth  int
	Name   string
	Path   string
	Stat   diff.Stat
	Viewed bool
	Counts ThreadCounts
	Thread *agentv1.ReviewThread
	Line   int
	Row    int
}

// ThreadCounts is how many threads stand in each state that asks something of the reader.
type ThreadCounts struct {
	Unsent int
	Unread int
	Open   int
}

// add counts one thread under the first state it is in.
func (c *ThreadCounts) add(thread *agentv1.ReviewThread) {
	switch {
	case doesHoldDelivery(thread, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT):
		c.Unsent++
	case thread.GetUnreadByHuman():
		c.Unread++
	case isUnsettled(thread):
		c.Open++
	}
}

// FileTree is the patch's files as a tree, in the order the diff draws them, with each file's
// threads under it.
func (v *View) FileTree() []TreeRow {
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

// GetTreeFocus is the file a diff row belongs to and the thread it carries, reading a thread at
// the foot of the diff as belonging to the file it was written on.
func (v *View) GetTreeFocus(y int) (string, string) {
	if thread, ok := v.GetThreadAt(y); ok {
		return v.pathOf(thread), thread.GetId()
	}

	file, ok := v.GetDrawnFileAt(y)
	if !ok {
		return "", ""
	}

	return file.Path(), ""
}

// treeFile is one file on its way into the tree, with the folders still to be opened above it.
type treeFile struct {
	index   int
	folders []string
}

// buildTree lays the files out as a tree, opening a folder row each time the path changes
// rather than sorting, so the tree walks the files in the order the diff does.
func (v *View) buildTree() {
	files := make([]treeFile, 0, len(v.patch.Files))
	for index := range v.patch.Files {
		parts := strings.Split(v.patch.Files[index].Path(), "/")
		files = append(files, treeFile{
			index:   index,
			folders: parts[:len(parts)-1],
		})
	}

	threads := v.threadsByFile()

	v.tree = nil
	v.appendTreeLevel(appendTreeLevelParams{
		files:   files,
		prefix:  "",
		depth:   0,
		threads: threads,
	})
}

// appendTreeLevelParams is one folder's worth of files and where it sits.
type appendTreeLevelParams struct {
	files   []treeFile
	prefix  string
	depth   int
	threads map[string][]*agentv1.ReviewThread
}

// appendTreeLevel draws one folder's worth of files, each run of files sharing a subfolder
// under a row of its own.
func (v *View) appendTreeLevel(params appendTreeLevelParams) {
	for at := 0; at < len(params.files); {
		current := params.files[at]
		if len(current.folders) == 0 {
			v.appendTreeFile(current.index, params.depth, params.threads)
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
			Kind:  TreeFolder,
			Depth: params.depth,
			Name:  name + "/",
			Path:  path,
			Row:   -1,
		})

		v.appendTreeLevel(appendTreeLevelParams{
			files:   run,
			prefix:  path + "/",
			depth:   params.depth + 1,
			threads: params.threads,
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

// isOneFolder reports whether a folder holds one folder and nothing else, which the tree draws
// as one row.
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

// appendTreeFile draws one file and the threads on it.
func (v *View) appendTreeFile(index, depth int, threads map[string][]*agentv1.ReviewThread) {
	file := v.patch.Files[index]
	path := file.Path()
	header, ok := v.fileHeader(path)
	if !ok {
		header = v.firstRowOf(index)
	}

	var counts ThreadCounts
	for _, thread := range threads[path] {
		counts.add(thread)
	}

	v.tree = append(v.tree, TreeRow{
		Kind:   TreeFile,
		Depth:  depth,
		Name:   path[strings.LastIndex(path, "/")+1:],
		Path:   path,
		Stat:   file.Stat(),
		Viewed: v.viewed[path],
		Counts: counts,
		Row:    header,
	})

	for _, thread := range threads[path] {
		row, drawn := v.GetThreadRow(thread.GetId())
		if !drawn {
			row = -1
		}

		v.tree = append(v.tree, TreeRow{
			Kind:   TreeThread,
			Depth:  depth + 1,
			Name:   firstLine(thread),
			Path:   path,
			Thread: thread,
			Line:   v.treeLineOf(thread, row),
			Row:    row,
		})
	}
}

// firstRowOf is the first row a file draws, for a diff drawn without file headers.
func (v *View) firstRowOf(index int) int {
	for y, current := range v.rows {
		if current.file == index {
			return y
		}
	}

	return -1
}

// treeLineOf is the line a thread stands on as the diff draws it, zero once it has moved off
// every line.
func (v *View) treeLineOf(thread *agentv1.ReviewThread, row int) int {
	if v.placed[thread.GetId()] == adrift {
		return 0
	}

	if line, ok := v.getLine(row); ok {
		return cmp.Or(line.NewNumber, line.OldNumber)
	}

	return int(lineOf(thread))
}

// threadsByFile is each file's threads in the order the diff draws them, those folded away
// with their file ordered by the line they were written on.
func (v *View) threadsByFile() map[string][]*agentv1.ReviewThread {
	byFile := map[string][]*agentv1.ReviewThread{}
	for _, thread := range v.threads {
		if thread.GetFilePath() == "" {
			continue
		}

		path := v.pathOf(thread)
		byFile[path] = append(byFile[path], thread)
	}

	rows := make(map[string]int, len(v.pinned))
	for y, thread := range v.pinned {
		rows[thread.GetId()] = y
	}

	rowOf := func(thread *agentv1.ReviewThread) int {
		row, ok := rows[thread.GetId()]
		if !ok {
			return len(v.rows)
		}

		return row
	}

	for _, threads := range byFile {
		slices.SortFunc(threads, func(a, b *agentv1.ReviewThread) int {
			return cmp.Or(
				cmp.Compare(rowOf(a), rowOf(b)),
				cmp.Compare(lineOf(a), lineOf(b)),
				cmp.Compare(a.GetId(), b.GetId()),
			)
		})
	}

	return byFile
}
