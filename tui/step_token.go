package tui

import (
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/discord"
	"net/http"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type tokenModel struct {
	input      textinput.Model
	validating bool
	err        error
}

func newTokenModel(existingToken string) tokenModel {
	ti := textinput.New()
	ti.Placeholder = "paste token here"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Focus()
	ti.CharLimit = 200

	if existingToken != "" {
		ti.SetValue(existingToken)
	}

	return tokenModel{
		input:      ti,
		validating: existingToken != "",
	}
}

func (m tokenModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m tokenModel) Update(msg tea.Msg, config *cfg.Config) (tokenModel, tea.Cmd) {
	if m.validating {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			token := strings.TrimSpace(m.input.Value())
			if token == "" {
				return m, nil
			}
			m.input.SetValue(token)
			m.validating = true
			m.err = nil
			config.Token = token
			_ = config.Save()
			return m, func() tea.Msg {
				client := discord.NewClient(token, config.BotMode, http.DefaultClient)
				user, err := client.ValidateToken()
				if err != nil {
					return tokenErrorMsg{err: err}
				}
				return tokenValidatedMsg{user: user}
			}
		case "esc":
			m.input.SetValue("")
			m.err = nil
			config.Token = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m tokenModel) View() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Token Setup") + "\n\n")

	if m.validating {
		b.WriteString("Validating...\n")
		return b.String()
	}

	if m.err != nil {
		b.WriteString(errorStyle.Render("Token rejected: "+m.err.Error()) + "\n\n")
	}

	b.WriteString("Paste your Discord token below.\n\n")
	b.WriteString(dimStyle.Render("HOW TO GET YOUR TOKEN:") + "\n")
	b.WriteString(dimStyle.Render("  1. Open Discord in your browser") + "\n")
	b.WriteString(dimStyle.Render("  2. Press F12 → Network tab") + "\n")
	b.WriteString(dimStyle.Render("  3. Do anything (send a message, switch channels)") + "\n")
	b.WriteString(dimStyle.Render("  4. Click any request to discord.com") + "\n")
	b.WriteString(dimStyle.Render("  5. Find 'Authorization' in the request headers") + "\n")
	b.WriteString(dimStyle.Render("  6. Copy that value") + "\n\n")

	b.WriteString("Token: " + m.input.View() + "\n\n")

	if m.input.Value() == "" {
		b.WriteString(dimStyle.Render("Paste your token and press Enter") + "\n")
	} else {
		b.WriteString(dimStyle.Render("Press Enter to validate") + "\n")
	}

	return b.String()
}
