package harness

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// ReadAppendOnlyLog reads what the log at path holds past offset and hands those bytes to
// flatten, which suits any log written a record per line and never rewritten.
func ReadAppendOnlyLog(path string, offset int64, flatten func([]byte) []Turn) (LogRead, error) {
	info, err := os.Stat(path)
	if err != nil {
		return LogRead{}, nil //nolint:nilerr // a log that is not on disk yet is not an error
	}

	// A log shorter than its offset, or a negative offset, is read again from the start.
	read := LogRead{Restart: offset < 0 || info.Size() < offset}
	if read.Restart {
		offset = 0
	}
	if info.Size() == offset {
		return read, nil
	}

	appended, consumed, err := readCompleteLines(path, offset)
	if err != nil {
		return LogRead{}, err
	}

	read.Turns, read.Consumed = flatten(appended), consumed

	return read, nil
}

func readCompleteLines(path string, offset int64) ([]byte, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open log %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("failed to seek log %q: %w", path, err)
	}

	tail, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read log %q: %w", path, err)
	}

	lastNL := bytes.LastIndexByte(tail, '\n')
	if lastNL < 0 {
		return nil, 0, nil
	}

	complete := tail[:lastNL+1]

	return complete, int64(len(complete)), nil
}

// CleanText trims a turn's prose and drops the NUL the transcript columns cannot hold, doing it
// as the prose is read since turns carry offsets a later removal would shift.
func CleanText(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
}
