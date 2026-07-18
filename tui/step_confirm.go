package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type confirmModel struct {
	total int
}

func newConfirmModel(total int) confirmModel {
	return confirmModel{total: total}
}

func (m confirmModel) Update(msg tea.Msg) (confirmModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return m, func() tea.Msg {
				return deleteConfirmedMsg{}
			}
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	var b strings.Builder

	warning := fmt.Sprintf("About to delete %d messages. This cannot be undone.", m.total)
	b.WriteString(dangerBoxStyle.Render(warning) + "\n\n")
	b.WriteString(dimStyle.Render("  Press Enter to start, Esc to go back.") + "\n")

	return b.String()
}
