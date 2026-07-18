package tui

import (
	"fmt"
	"strings"

	"github.com/inherentescapade/viaduct/dates"

	tea "github.com/charmbracelet/bubbletea"
)

type filtersModel struct {
	fields [2]string // before, after
	cursor int       // 0=before, 1=after
}

func newFiltersModel() filtersModel {
	return filtersModel{}
}

func (m filtersModel) Update(msg tea.Msg) (filtersModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			m.cursor = (m.cursor + 1) % 2
		case "shift+tab", "up":
			m.cursor = (m.cursor + 1) % 2 // with two fields, forward and back coincide
		case "enter":
			if m.fields[0] != "" {
				if _, err := dates.Parse(m.fields[0]); err != nil {
					return m, nil
				}
			}
			if m.fields[1] != "" {
				if _, err := dates.Parse(m.fields[1]); err != nil {
					return m, nil
				}
			}
			return m, func() tea.Msg {
				return filtersConfirmedMsg{
					before: m.fields[0],
					after:  m.fields[1],
				}
			}
		case "backspace":
			if len(m.fields[m.cursor]) > 0 {
				m.fields[m.cursor] = m.fields[m.cursor][:len(m.fields[m.cursor])-1]
			}
		default:
			if len(msg.String()) == 1 {
				m.fields[m.cursor] += msg.String()
			}
		}
	}
	return m, nil
}

func (m filtersModel) View() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Filters (optional)") + "\n\n")
	b.WriteString(dimStyle.Render("Leave blank to skip. Tab to switch fields. Enter to continue.") + "\n\n")

	labels := []string{"Before date:", "After date: "}

	for i := 0; i < 2; i++ {
		cursor := "  "
		if m.cursor == i {
			cursor = "▸ "
		}

		value := m.fields[i]
		if value == "" {
			value = dimStyle.Render("YYYY-MM-DD or 30d")
		} else {
			value = inputStyle.Render(value)
		}

		if m.cursor == i {
			value += "█"
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, labels[i], value))
	}

	b.WriteString("\n" + dimStyle.Render("Formats: 2024-01-01, 30d (30 days ago), 24h, 60m") + "\n")

	return b.String()
}
