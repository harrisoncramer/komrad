package panelside

import "errors"

var errFlowControl = errors.New("the panel is not on screen on this tmux server")
