package blackbox

type Panel struct {
	model model
}

func NewPanel() *Panel {
	return &Panel{model: newModel()}
}
