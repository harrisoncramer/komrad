package viewsplit

type screen struct {
	rows  []string
	width int
	shown map[string]bool
}

type frame struct{}

func (s *screen) resize(width int) {
	s.width = width
}

func (s screen) bandView() string { // want `bandView draws, so it belongs in a file ending in view.go beside the file holding its state`
	return "band"
}

func view(rows []string) string { // want `view draws, so it belongs in a file ending in view.go beside the file holding its state`
	return rows[0]
}

func (s screen) View() frame {
	return frame{}
}

func (s screen) pickView() error {
	return nil
}

func (s screen) review() string {
	return "review"
}
