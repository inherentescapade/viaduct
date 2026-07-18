package tui

import (
	"fmt"
	"github.com/inherentescapade/viaduct/discord"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type channelsModel struct {
	channels []discord.Channel
	selected map[string]bool
	cursor   int
	loading  bool
}

func newChannelsModel() channelsModel {
	return channelsModel{
		loading:  true,
		selected: make(map[string]bool),
	}
}

func (m channelsModel) Update(msg tea.Msg) (channelsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.loading {
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.channels)-1 {
				m.cursor++
			}
		case " ":
			if m.cursor < len(m.channels) {
				ch := m.channels[m.cursor]
				m.selected[ch.Id] = !m.selected[ch.Id]
			}
		case "a":
			for _, ch := range m.channels {
				m.selected[ch.Id] = true
			}
		case "n":
			m.selected = make(map[string]bool)
		case "enter":
			var selected []discord.Channel
			for _, ch := range m.channels {
				if m.selected[ch.Id] {
					selected = append(selected, ch)
				}
			}
			if len(selected) > 0 {
				return m, func() tea.Msg {
					return channelsConfirmedMsg{channels: selected}
				}
			}
		}
	}
	return m, nil
}

func (m channelsModel) View() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Select Channels") + "\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("Loading channels...") + "\n")
		return b.String()
	}

	count := 0
	for _, v := range m.selected {
		if v {
			count++
		}
	}

	b.WriteString(dimStyle.Render(fmt.Sprintf("Space to toggle, a=all, n=none, Enter to confirm (%d selected)", count)) + "\n\n")

	// Show channels with scroll window
	start := 0
	maxVisible := 15
	if len(m.channels) > maxVisible {
		start = m.cursor - maxVisible/2
		if start < 0 {
			start = 0
		}
		if start+maxVisible > len(m.channels) {
			start = len(m.channels) - maxVisible
		}
	}

	end := start + maxVisible
	if end > len(m.channels) {
		end = len(m.channels)
	}

	for i := start; i < end; i++ {
		ch := m.channels[i]

		checkbox := "[ ]"
		if m.selected[ch.Id] {
			checkbox = "[x]"
		}

		name := "#" + ch.Name
		style := dimStyle
		if i == m.cursor {
			style = selectedStyle
		}

		nsfw := ""
		if ch.Nsfw {
			nsfw = " (NSFW)"
		}

		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		b.WriteString(style.Render(fmt.Sprintf("%s%s %s%s", cursor, checkbox, name, nsfw)) + "\n")
	}

	if len(m.channels) > maxVisible {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  %d/%d channels shown", maxVisible, len(m.channels))) + "\n")
	}

	return b.String()
}
