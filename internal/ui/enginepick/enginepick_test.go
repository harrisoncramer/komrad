package enginepick

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pickerRows() []Row {
	return []Row{
		{Name: ""},
		{Name: "remote", Facts: []Fact{{Label: "addr", Value: "127.0.0.1:8787"}}},
		{Name: "staging"},
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

func press(t *testing.T, m model, k string) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(key(k))

	pressed, ok := next.(model)
	require.True(t, ok)

	return pressed, cmd
}

// TestPickerProbesEveryRowOnce covers the probes starting with the picker and landing on the row
// each was asked about.
func TestPickerProbesEveryRowOnce(t *testing.T) {
	asked := map[string]int{}
	m := newModel(RunParams{
		Rows: pickerRows(),
		Probe: func(row Row) Status {
			asked[row.Name]++

			return Status{Build: "v1"}
		},
	})

	batch, ok := m.Init()().(tea.BatchMsg)
	require.True(t, ok)
	require.Len(t, batch, 3)

	for _, cmd := range batch {
		next, _ := m.Update(cmd())
		m = next.(model)
	}

	assert.Equal(t, map[string]int{"": 1, "remote": 1, "staging": 1}, asked)
	assert.Equal(t, []bool{true, true, true}, m.probed)
}

// TestPickerChoice covers what enter does on each kind of row and what escape leaves behind.
func TestPickerChoice(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		statuses   []Status
		wantChosen int
		wantNotice string
	}{
		{
			name:       "escape chooses nothing",
			keys:       []string{"j", "esc"},
			wantChosen: -1,
		},
		{
			name:       "a row still being checked cannot be chosen yet",
			keys:       []string{"j", "enter"},
			statuses:   nil,
			wantChosen: -1,
			wantNotice: "still checking what remote is running",
		},
		{
			name: "a mismatched engine refuses and says why",
			keys: []string{"j", "enter"},
			statuses: []Status{
				{Build: "v1"},
				{Build: "v0", Label: "mismatch", Refused: "the engine runs other code"},
				{Build: "v1"},
			},
			wantChosen: -1,
			wantNotice: "the engine runs other code",
		},
		{
			name:       "the cursor stops at the ends",
			keys:       []string{"k", "j", "j", "j", "j", "enter"},
			statuses:   []Status{{Build: "v1"}, {Build: "v1"}, {Build: "v1"}},
			wantChosen: 2,
		},
		{
			name:       "this machine is chosen with enter",
			keys:       []string{"enter"},
			statuses:   []Status{{Build: "nothing yet"}, {Build: "v1"}, {Build: "v1"}},
			wantChosen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(RunParams{Rows: pickerRows()})
			for index, status := range tt.statuses {
				next, _ := m.Update(probedMsg{index: index, status: status})
				m = next.(model)
			}

			var cmd tea.Cmd
			for _, k := range tt.keys {
				m, cmd = press(t, m, k)
			}

			assert.Equal(t, tt.wantChosen, m.chosen)
			assert.Equal(t, tt.wantNotice, m.notice)

			if tt.wantChosen >= 0 || tt.keys[len(tt.keys)-1] == "esc" {
				require.NotNil(t, cmd)
				assert.Equal(t, tea.Quit(), cmd(), "the picker closes")
			} else {
				assert.Nil(t, cmd, "a refused choice leaves the picker up")
			}
		})
	}
}

// TestPickerView covers what each row says about itself and the preview beside the cursor.
func TestPickerView(t *testing.T) {
	m := newModel(RunParams{Rows: pickerRows(), Self: "devel+abc"})
	m.width = 100

	next, _ := m.Update(probedMsg{index: 1, status: Status{Build: "v0.4.0 (8fe4a3b)", Label: "mismatch", Refused: "the engine runs other code"}})
	m = next.(model)
	next, _ = m.Update(probedMsg{index: 2, status: Status{Build: "not reachable", Label: "down", Refused: "ssh: connect timed out"}})
	m = next.(model)

	view := strings.Join(strings.Fields(m.View()), " ")

	assert.Contains(t, view, "this weave is devel+abc")
	assert.Contains(t, view, "▸ this machine checking")
	assert.Contains(t, view, "remote mismatch")
	assert.Contains(t, view, "staging down")

	m, _ = press(t, m, "j")
	view = strings.Join(strings.Fields(m.View()), " ")

	assert.Contains(t, view, "addr 127.0.0.1:8787")
	assert.Contains(t, view, "running v0.4.0 (8fe4a3b)")
	assert.Contains(t, view, "the engine runs other code")
}
