package frames_test

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/frames"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func on() frames.Settings { return frames.Settings{On: true, Keep: 3} }

func written(t *testing.T, dir, agentID string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(dir, agentID))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)

	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// TestRecordWritesTheFrameItWasRead is the whole point of a recording: the bytes come back
// exactly as the harness saw them, so the frame can be replayed through the classifier.
func TestRecordWritesTheFrameItWasRead(t *testing.T) {
	dir := t.TempDir()
	frame := "✻ Cogitating… (12s)\n───────\n❯ \n───────"

	require.NoError(t, frames.NewRecorder(dir, on).Record(frames.RecordParams{
		AgentID:  "a1",
		Frame:    frame,
		Activity: "working",
	}))

	names := written(t, dir, "a1")
	require.Len(t, names, 1)
	assert.Contains(t, names[0], "-working.txt")

	kept, err := os.ReadFile(filepath.Join(dir, "a1", names[0]))
	require.NoError(t, err)
	assert.Equal(t, frame, string(kept))
}

// TestRecordWritesNothingWhileOff covers the setting a recorder built for nobody reads.
func TestRecordWritesNothingWhileOff(t *testing.T) {
	dir := t.TempDir()
	off := func() frames.Settings { return frames.Settings{Keep: 3} }

	require.NoError(t, frames.NewRecorder(dir, off).Record(frames.RecordParams{AgentID: "a1", Frame: "anything"}))

	assert.Empty(t, written(t, dir, "a1"))
}

// TestRecordSkipsAFrameItAlreadyHolds keeps a still pane from filling the directory with the
// same bytes over and over.
func TestRecordSkipsAFrameItAlreadyHolds(t *testing.T) {
	dir := t.TempDir()
	recorder := frames.NewRecorder(dir, on)

	for range 3 {
		require.NoError(t, recorder.Record(frames.RecordParams{
			AgentID:  "a1",
			Frame:    "same",
			Activity: "idle",
		}))
	}

	assert.Len(t, written(t, dir, "a1"), 1)
}

// TestRecordKeepsAFrameDisputingTheStateOnRecord is the case a recording exists for: the pane
// has not moved, and what weave believes about it is wrong.
func TestRecordKeepsAFrameDisputingTheStateOnRecord(t *testing.T) {
	dir := t.TempDir()
	recorder := frames.NewRecorder(dir, on)

	require.NoError(t, recorder.Record(frames.RecordParams{
		AgentID:  "a1",
		Frame:    "same",
		Activity: "idle",
	}))
	require.NoError(t, recorder.Record(frames.RecordParams{
		AgentID:  "a1",
		Frame:    "same",
		Activity: "idle",
		Disputes: true,
	}))

	assert.Len(t, written(t, dir, "a1"), 2)
}

// TestRecordSkipsAnUnrecognizedFrameItAlreadyHolds covers the frames a stuck pane repeats,
// which are the ones a recording is turned on to find and so must not be the ones it evicts.
func TestRecordSkipsAnUnrecognizedFrameItAlreadyHolds(t *testing.T) {
	dir := t.TempDir()
	recorder := frames.NewRecorder(dir, on)

	for range 5 {
		require.NoError(t, recorder.Record(frames.RecordParams{
			AgentID:  "a1",
			Frame:    "same",
			Activity: "unrecognized",
		}))
	}

	assert.Len(t, written(t, dir, "a1"), 1)
}

// TestRecordTakesAFrameAgainAfterAFailedWrite covers a full disk, where the frame the write
// lost must not then be skipped as one the recorder already holds.
func TestRecordTakesAFrameAgainAfterAFailedWrite(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	require.NoError(t, os.WriteFile(blocked, nil, 0o600))

	recorder := frames.NewRecorder(blocked, on)
	reading := frames.RecordParams{
		AgentID:  "a1",
		Frame:    "same",
		Activity: "idle",
	}

	require.Error(t, recorder.Record(reading), "a directory that cannot be made is an error")

	require.NoError(t, os.Remove(blocked))
	require.NoError(t, recorder.Record(reading))

	assert.Len(t, written(t, blocked, "a1"), 1, "the same recorder has to take the frame it failed to write")
}

// TestRecordDropsTheOldestPastTheLimit bounds what a busy agent can leave on disk.
func TestRecordDropsTheOldestPastTheLimit(t *testing.T) {
	dir := t.TempDir()
	recorder := frames.NewRecorder(dir, on)

	for _, frame := range []string{"one", "two", "three", "four", "five"} {
		require.NoError(t, recorder.Record(frames.RecordParams{
			AgentID:  "a1",
			Frame:    frame,
			Activity: "idle",
		}))
	}

	assert.Len(t, written(t, dir, "a1"), 3)
}

// TestRecordSeparatesAgents keeps one agent's evidence out of another's directory.
func TestRecordSeparatesAgents(t *testing.T) {
	dir := t.TempDir()
	recorder := frames.NewRecorder(dir, on)

	require.NoError(t, recorder.Record(frames.RecordParams{
		AgentID:  "a1",
		Frame:    "one",
		Activity: "idle",
	}))
	require.NoError(t, recorder.Record(frames.RecordParams{
		AgentID:  "a2",
		Frame:    "two",
		Activity: "idle",
	}))

	assert.Len(t, written(t, dir, "a1"), 1)
	assert.Len(t, written(t, dir, "a2"), 1)
}

// TestForgetDropsWhatADeletedAgentLeft stops a new agent inheriting the frame an old one under
// the same id was last read from.
func TestForgetDropsWhatADeletedAgentLeft(t *testing.T) {
	dir := t.TempDir()
	recorder := frames.NewRecorder(dir, on)

	require.NoError(t, recorder.Record(frames.RecordParams{
		AgentID:  "a1",
		Frame:    "same",
		Activity: "idle",
	}))
	recorder.Forget(map[string]struct{}{})
	require.NoError(t, recorder.Record(frames.RecordParams{
		AgentID:  "a1",
		Frame:    "same",
		Activity: "idle",
	}))

	assert.Len(t, written(t, dir, "a1"), 2)
}
