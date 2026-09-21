package cli

import (
	"os"
	"time"
)

type executableWatch struct {
	path    string
	size    int64
	modTime time.Time
}

func watchExecutable() *executableWatch {
	path, err := os.Executable()
	if err != nil {
		return nil
	}

	return watchFile(path)
}

func watchFile(path string) *executableWatch {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	return &executableWatch{
		path:    path,
		size:    info.Size(),
		modTime: info.ModTime(),
	}
}

// IsReplaced reports whether the file no longer matches what was recorded, a file that has gone
// counting as replaced.
func (w *executableWatch) IsReplaced() bool {
	info, err := os.Stat(w.path)
	if err != nil {
		return true
	}

	return info.Size() != w.size || !info.ModTime().Equal(w.modTime)
}
