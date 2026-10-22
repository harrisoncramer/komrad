package blackbox

type Screen struct {
	focused bool
}

func NewScreen() Screen {
	return Screen{}
}

func (s *Screen) Focus() {
	s.focused = true
}
