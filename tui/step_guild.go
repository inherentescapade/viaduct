package tui

import (
	"fmt"
	"github.com/inherentescapade/viaduct/discord"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type guildModel struct {
	guilds  []discord.Guild
	cursor  int
	loading bool
	filter  string
}

func newGuildModel() guildModel {
	return guildModel{loading: true}
}

func (m guildModel) filtered() []discord.Guild {
	if m.filter == "" {
		return m.guilds
	}
	lower := strings.ToLower(m.filter)
	var out []discord.Guild
	for _, g := range m.guilds {
		if strings.Contains(strings.ToLower(g.Name), lower) {
			out = append(out, g)
		}
	}
	return out
}

func (m guildModel) Update(msg tea.Msg) (guildModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}

		filtered := m.filtered()

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(filtered)-1 {
				m.cursor++
			}
		case "enter":
			if len(filtered) > 0 && m.cursor < len(filtered) {
				return m, func() tea.Msg {
					return guildSelectedMsg{guild: filtered[m.cursor]}
				}
			}
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.cursor = 0
			}
		default:
			if len(msg.String()) == 1 {
				m.filter += msg.String()
				m.cursor = 0
			}
		}
	}
	return m, nil
}

func (m guildModel) View() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Select a Server") + "\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("Loading servers...") + "\n")
		return b.String()
	}

	if m.filter != "" {
		b.WriteString("Filter: " + inputStyle.Render(m.filter) + "█\n\n")
	} else {
		b.WriteString(dimStyle.Render("Type to filter, ↑/↓ to navigate, Enter to select") + "\n\n")
	}

	filtered := m.filtered()

	if len(filtered) == 0 {
		b.WriteString(dimStyle.Render("No servers match your filter.") + "\n")
		return b.String()
	}

	// Show a window of guilds around the cursor
	start := 0
	maxVisible := 15
	if len(filtered) > maxVisible {
		start = m.cursor - maxVisible/2
		if start < 0 {
			start = 0
		}
		if start+maxVisible > len(filtered) {
			start = len(filtered) - maxVisible
		}
	}

	end := start + maxVisible
	if end > len(filtered) {
		end = len(filtered)
	}

	for i := start; i < end; i++ {
		g := filtered[i]
		prefix := "  "
		style := dimStyle
		if i == m.cursor {
			prefix = "▸ "
			style = selectedStyle
		}

		owner := ""
		if g.Owner {
			owner = " (owner)"
		}

		b.WriteString(style.Render(fmt.Sprintf("%s%s%s", prefix, g.Name, owner)) + "\n")
	}

	if len(filtered) > maxVisible {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  %d/%d servers shown", maxVisible, len(filtered))) + "\n")
	}

	return b.String()
}
