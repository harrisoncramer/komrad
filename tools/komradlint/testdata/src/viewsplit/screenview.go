package viewsplit

import "strings"

func (s screen) rowsView() string {
	var out strings.Builder
	for i := range s.rows {
		line := s.rows[i]
		line += "\n"
		out.WriteString(line)
	}

	return out.String()
}

func (s *screen) layoutView(width int) string {
	s.width = width // want `layoutView writes s while it draws; change state where a message is handled and let the view read it`
	s.rows[0] = ""  // want `layoutView writes s while it draws; change state where a message is handled and let the view read it`
	s.width++       // want `layoutView writes s while it draws; change state where a message is handled and let the view read it`

	return ""
}

func markView(shown map[string]bool, count *int, key string) string {
	shown[key] = true // want `markView writes shown while it draws; change state where a message is handled and let the view read it`
	*count = 1        // want `markView writes count while it draws; change state where a message is handled and let the view read it`
	key = "local"

	return key
}

func (s screen) copyView() string {
	held := s
	held.width = 0

	return strings.Repeat(" ", held.width)
}

func (s screen) localView(width int) string {
	s.width = width
	s.rows[0] = "" // want `localView writes s while it draws; change state where a message is handled and let the view read it`

	return s.rows[0]
}

func countView(held screen) string {
	held.width++

	return ""
}
