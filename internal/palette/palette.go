package palette

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	colorAccent = lipgloss.Color("4")
	colorMuted  = lipgloss.Color("8")
)

var (
	promptStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(colorMuted)
)

// Run draws the palette and returns once it has been closed.
func Run() error {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		return fmt.Errorf("failed to draw the palette: %w", err)
	}

	return nil
}

type model struct {
	query string

	height int
}

func newModel() model {
	return model{}
}

func (m model) Init() tea.Cmd { return nil }

// Update drives the palette, typing every printable key into the query and binding nothing
// else.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height

		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch k := key.String(); {
	case k == "esc" || k == "ctrl+c":
		return m, tea.Quit
	case k == "backspace":
		m.query = trimLastRune(m.query)
	case key.Type == tea.KeySpace:
		m.query += " "
	case key.Type == tea.KeyRunes:
		m.query += string(key.Runes)
	}

	return m, nil
}

func (m model) View() string {
	var out strings.Builder

	out.WriteString(promptStyle.Render("›") + " " + m.query + promptStyle.Render("▏") + "\n\n")
	out.WriteString(dimStyle.Render("nothing is wired up yet") + "\n\n")
	out.WriteString(dimStyle.Render("esc close"))

	return out.String()
}

func trimLastRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}

	return string(runes[:len(runes)-1])
}
