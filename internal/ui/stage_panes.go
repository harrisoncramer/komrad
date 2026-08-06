package ui

import (
	"math"
	"os"

	"weave/config"
)

type stageTmux interface {
	IsPaneAlive(pane string) bool
	GetPaneWidth(pane string) int
	SplitRight(anchorPane, dir string, percent int) string
	SplitBelow(anchorPane, dir string, percent int) string
	ResizePane(pane string, percent int)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
	GetSiblingPanes(pane string) []string
	BreakPane(pane string)
	KillPane(pane string)
	GetPaneOwner(pane string) string
}

// panes owns the tmux panes the stage occupies: the one beside the panel, the
// one an agent has swapped into it, and the one hanging below either.
type panes struct {
	tmux           stageTmux
	split          float64
	secondarySplit float64
	navPane        string
	pane           string
	agentPane      string
	secondary      string
}

func newPanes(tmux stageTmux, layout config.Layout) panes {
	return panes{tmux: tmux, split: layout.Split, secondarySplit: layout.SecondarySplit}
}

func (p *panes) share() float64 {
	if p.split <= 0 || p.split >= 1 {
		return config.DefaultSplit
	}

	return p.split
}

func (p *panes) percent() int {
	return int(math.Round((1 - p.share()) * 100))
}

func (p *panes) secondaryPercent() int {
	share := p.secondarySplit
	if share <= 0 || share >= 1 {
		share = config.DefaultSecondarySplit
	}

	return int(math.Round(share * 100))
}

func (p *panes) wrap(pane string) int {
	return p.tmux.GetPaneWidth(pane)
}

// Init anchors the panes to the panel pane and applies the configured layout.
func (p *panes) Init(pane string) {
	p.navPane = pane
	p.reclaim()
	p.claim()

	if p.pane != "" {
		p.tmux.ResizePane(p.pane, p.percent())
	}
}

// NavPane returns the pane containing the panel.
func (p *panes) NavPane() string { return p.navPane }

// SlotPane returns the pane currently displayed beside the panel.
func (p *panes) SlotPane() string {
	if p.agentPane != "" {
		return p.agentPane
	}

	return p.pane
}

// Agent panes must be detached rather than killed when reclaiming a stale layout.
func (p *panes) reclaim() {
	if p.navPane == "" {
		return
	}

	for _, pane := range p.tmux.GetSiblingPanes(p.navPane) {
		if p.tmux.GetPaneOwner(pane) != "" {
			p.tmux.BreakPane(pane)

			continue
		}

		p.tmux.KillPane(pane)
	}
}

// claim ensures the main stage pane exists, rebuilding it if it or the agent pane
// it was minding has died. It reports whether it had to rebuild.
func (p *panes) claim() bool {
	if p.navPane == "" || p.hasSlot() {
		return false
	}

	p.abandon()

	home, _ := os.UserHomeDir()
	p.pane = p.tmux.SplitRight(p.navPane, home, p.percent())

	return true
}

func (p *panes) hasSlot() bool {
	if p.pane == "" || !p.tmux.IsPaneAlive(p.pane) {
		return false
	}

	return p.agentPane == "" || p.tmux.IsPaneAlive(p.agentPane)
}

func (p *panes) abandon() {
	if p.pane != "" && p.tmux.IsPaneAlive(p.pane) {
		p.tmux.KillPane(p.pane)
	}

	p.closeSecondary()
	p.pane, p.agentPane = "", ""
}

// openSecondary opens the pane below the main stage, reporting whether it had to
// split a new one.
func (p *panes) openSecondary() (string, bool) {
	if p.pane == "" {
		return "", false
	}
	if p.IsSecondaryAlive() {
		return p.secondary, false
	}

	home, _ := os.UserHomeDir()
	p.secondary = p.tmux.SplitBelow(p.SlotPane(), home, p.secondaryPercent())

	return p.secondary, true
}

func (p *panes) closeSecondary() {
	if p.secondary == "" {
		return
	}

	p.tmux.KillPane(p.secondary)
	p.secondary = ""
}

// IsSecondaryAlive reports whether the secondary pane is still running.
func (p *panes) IsSecondaryAlive() bool {
	return p.secondary != "" && p.tmux.IsPaneAlive(p.secondary)
}

func (p *panes) restore() {
	if p.agentPane == "" {
		return
	}

	if p.tmux.IsPaneAlive(p.agentPane) {
		p.tmux.SwapPanes(p.pane, p.agentPane)
	}

	p.agentPane = ""
}

// Release removes an agent pane from the stage before it is killed.
func (p *panes) Release(pane string) {
	if pane != "" && pane == p.agentPane {
		p.restore()
	}
}

// agent swaps a live agent pane into the stage slot, reporting whether it did.
func (p *panes) agent(pane string) bool {
	p.claim()
	if p.pane == "" || pane == "" || pane == p.agentPane {
		return false
	}

	p.restore()
	p.tmux.SwapPanes(pane, p.pane)
	p.agentPane = pane

	return true
}

// run replaces whatever a pane is currently showing with a new command.
func (p *panes) run(pane string, argv ...string) {
	home, _ := os.UserHomeDir()
	p.tmux.RespawnPane(pane, home, argv...)
}
