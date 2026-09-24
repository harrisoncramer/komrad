package ui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/ui"
)

func TestZZExplore(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "weave.log")
	os.WriteFile(logPath, []byte("x\n"), 0o644)
	runs := t.TempDir()
	logDir := config.RunLogDir(runs, "a1")
	os.MkdirAll(logDir, 0o755)
	for i := 1; i <= 3; i++ {
		os.WriteFile(filepath.Join(logDir, config.RunLogName(i, config.TriggerProvision, "make-worktree")), []byte("out\n"), 0o644)
	}
	h := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{LogPath: logPath, RunLogsDir: runs}})
	answerWorktreeReads(h)
	h.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))
	h.press("j")
	h.fireTimers()
	h.press("l", "L", "?")
	fmt.Println(h.readFrame())
	fmt.Printf("%q\n%q\n", h.panel.ReadRespawns(), h.panel.ReadKilledPanes())
}
