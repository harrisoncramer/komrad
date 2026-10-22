package screen

// Panes is what a screen reads of the tmux panes around the panel.
type Panes interface {
	IsPaneAlive(pane string) bool
	GetHostPath() string
}
