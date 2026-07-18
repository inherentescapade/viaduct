package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7289DA")).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#99AAB5")).
			MarginBottom(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#43B581"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F04747"))

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAA61A"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#72767D"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7289DA")).
			Bold(true)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	dangerBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F04747")).
			Padding(1, 2)

	progressFullStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#43B581"))

	progressEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4F545C"))
)
