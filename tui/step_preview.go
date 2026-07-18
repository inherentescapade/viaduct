package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type previewModel struct {
	total   int
	loading bool
}

func newPreviewModel() previewModel {
	return previewModel{loading: true}
}

func (m previewModel) Update(msg tea.Msg) (previewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}
		switch msg.String() {
		case "enter":
			if m.total > 0 {
				return m, func() tea.Msg {
					return previewConfirmedMsg{}
				}
			}
		}
	case previewLoadedMsg:
		m.total = msg.total
		m.loading = false
	}
	return m, nil
}

func (m previewModel) View() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Preview") + "\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("  Counting messages...") + "\n")
		return b.String()
	}

	if m.total == 0 {
		b.WriteString(warnStyle.Render("  No messages found matching your criteria.") + "\n")
		b.WriteString(dimStyle.Render("  Press Esc to go back and adjust filters.") + "\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("  Found %s messages to delete.\n\n", selectedStyle.Render(fmt.Sprintf("%d", m.total))))
	b.WriteString(dimStyle.Render("  Press Enter to continue.") + "\n")

	return b.String()
}
