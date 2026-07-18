package tui

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/dates"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
)

type step int

const (
	stepToken step = iota
	stepGuild
	stepChannels
	stepFilters
	stepPreview
	stepConfirm
	stepProgress
	stepDone
)

type appModel struct {
	config *cfg.Config
	client *discord.Client
	engine *engine.Engine
	user   *discord.User
	width  int
	height int

	step    step
	token   tokenModel
	guild   guildModel
	chans   channelsModel
	filters filtersModel
	preview previewModel
	confirm confirmModel
	prog    progressModel
	done    doneModel

	// Shared state
	selectedGuild    *discord.Guild
	selectedChannels []discord.Channel
	err              error
}

var program *tea.Program

func Run(config *cfg.Config) error {
	m := newAppModel(config)
	program = tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func newAppModel(config *cfg.Config) appModel {
	m := appModel{
		config: config,
		step:   stepToken,
	}

	if config.Token != "" {
		m.token = newTokenModel(config.Token)
	} else {
		m.token = newTokenModel("")
	}

	return m
}

func (m appModel) Init() tea.Cmd {
	if m.config.Token != "" {
		return m.validateTokenCmd()
	}
	return m.token.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.step > stepToken && m.step < stepProgress {
				return m.goBack(), nil
			}
		}

	case tokenValidatedMsg:
		m.user = msg.user
		m.client = discord.NewClient(m.config.Token, m.config.BotMode, http.DefaultClient)
		m.engine = engine.New(m.config.Token, m.config.BotMode)
		m.step = stepGuild
		m.guild = newGuildModel()
		return m, m.fetchGuildsCmd()

	case tokenErrorMsg:
		m.token.err = msg.err
		m.token.validating = false
		m.token.input.SetValue("")
		m.config.Token = ""
		_ = m.config.Save()
		m.step = stepToken
		return m, nil

	case guildsLoadedMsg:
		m.guild.guilds = msg.guilds
		m.guild.loading = false
		_ = cfg.SaveGuildCache(msg.guilds)
		return m, nil

	case guildSelectedMsg:
		m.selectedGuild = &msg.guild
		m.step = stepChannels
		m.chans = newChannelsModel()
		return m, m.fetchChannelsCmd()

	case channelsLoadedMsg:
		m.chans.channels = msg.channels
		m.chans.loading = false
		// Pre-select all
		for i := range m.chans.channels {
			m.chans.selected[m.chans.channels[i].Id] = true
		}
		return m, nil

	case channelsConfirmedMsg:
		m.selectedChannels = msg.channels
		m.step = stepFilters
		m.filters = newFiltersModel()
		return m, nil

	case filtersConfirmedMsg:
		m.step = stepPreview
		m.preview = newPreviewModel()
		return m, m.previewCmd(msg)

	case previewLoadedMsg:
		m.preview.total = msg.total
		m.preview.loading = false
		return m, nil

	case previewConfirmedMsg:
		m.step = stepConfirm
		m.confirm = newConfirmModel(m.preview.total)
		return m, nil

	case deleteConfirmedMsg:
		m.step = stepProgress
		m.prog = newProgressModel()
		return m, m.executeDeleteCmd()

	case progressUpdateMsg:
		m.prog.update(msg.progress)
		if msg.progress.Done {
			m.step = stepDone
			m.done = newDoneModel(m.prog, m.engine.LogPath())
			return m, nil
		}
		return m, nil

	case deleteCompleteMsg:
		m.step = stepDone
		m.done = newDoneModel(m.prog, m.engine.LogPath())
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	// Delegate to current step
	var cmd tea.Cmd
	switch m.step {
	case stepToken:
		m.token, cmd = m.token.Update(msg, m.config)
		return m, cmd
	case stepGuild:
		m.guild, cmd = m.guild.Update(msg)
		return m, cmd
	case stepChannels:
		m.chans, cmd = m.chans.Update(msg)
		return m, cmd
	case stepFilters:
		m.filters, cmd = m.filters.Update(msg)
		return m, cmd
	case stepPreview:
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	case stepConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
		return m, cmd
	case stepProgress:
		m.prog, cmd = m.prog.Update(msg)
		return m, cmd
	case stepDone:
		m.done, cmd = m.done.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m appModel) View() string {
	header := titleStyle.Render("VIADUCT") + "  " + dimStyle.Render("Discord message deletion")
	stepLabel := dimStyle.Render(fmt.Sprintf("Step %d/8", int(m.step)+1))
	top := header + "  " + stepLabel + "\n\n"

	if m.err != nil {
		top += errorStyle.Render("Error: "+m.err.Error()) + "\n\n"
	}

	var body string
	switch m.step {
	case stepToken:
		body = m.token.View()
	case stepGuild:
		body = m.guild.View()
	case stepChannels:
		body = m.chans.View()
	case stepFilters:
		body = m.filters.View()
	case stepPreview:
		body = m.preview.View()
	case stepConfirm:
		body = m.confirm.View()
	case stepProgress:
		body = m.prog.View()
	case stepDone:
		body = m.done.View()
	}

	footer := "\n" + dimStyle.Render("ctrl+c quit • esc back")

	return top + body + footer
}

func (m appModel) goBack() appModel {
	switch m.step {
	case stepGuild:
		m.step = stepToken
	case stepChannels:
		m.step = stepGuild
	case stepFilters:
		m.step = stepChannels
	case stepPreview:
		m.step = stepFilters
	case stepConfirm:
		m.step = stepPreview
	}
	return m
}

// Messages
type tokenValidatedMsg struct{ user *discord.User }
type tokenErrorMsg struct{ err error }
type guildsLoadedMsg struct{ guilds []discord.Guild }
type guildSelectedMsg struct{ guild discord.Guild }
type channelsLoadedMsg struct{ channels []discord.Channel }
type channelsConfirmedMsg struct{ channels []discord.Channel }
type filtersConfirmedMsg struct {
	before string
	after  string
}
type previewLoadedMsg struct {
	total int
}
type previewConfirmedMsg struct{}
type deleteConfirmedMsg struct{}
type progressUpdateMsg struct{ progress engine.Progress }
type deleteCompleteMsg struct{}
type errMsg struct{ err error }

// Commands
func (m appModel) validateTokenCmd() tea.Cmd {
	return func() tea.Msg {
		client := discord.NewClient(m.config.Token, m.config.BotMode, http.DefaultClient)
		user, err := client.ValidateToken()
		if err != nil {
			return tokenErrorMsg{err: err}
		}
		return tokenValidatedMsg{user: user}
	}
}

func (m appModel) fetchGuildsCmd() tea.Cmd {
	return func() tea.Msg {
		guilds, err := m.client.GetGuilds()
		if err != nil {
			return errMsg{err: err}
		}
		return guildsLoadedMsg{guilds: guilds}
	}
}

func (m appModel) fetchChannelsCmd() tea.Cmd {
	return func() tea.Msg {
		channels, err := m.client.GetChannels(m.selectedGuild.Id)
		if err != nil {
			return errMsg{err: err}
		}
		// Filter to text channels
		var text []discord.Channel
		for _, ch := range channels {
			if ch.Type == 0 || ch.Type == 5 || m.selectedGuild.Id == "@me" {
				text = append(text, ch)
			}
		}
		return channelsLoadedMsg{channels: text}
	}
}

func (m appModel) previewCmd(filters filtersConfirmedMsg) tea.Cmd {
	return func() tea.Msg {
		job := engine.DeleteJob{
			GuildID:   m.selectedGuild.Id,
			GuildName: m.selectedGuild.Name,
			UserID:    m.user.Id,
		}

		if filters.before != "" {
			if t, err := dates.Parse(filters.before); err == nil {
				job.Before = t
			}
		}
		if filters.after != "" {
			if t, err := dates.Parse(filters.after); err == nil {
				job.After = t
			}
		}

		total, err := m.engine.Preview(context.Background(), job)
		if err != nil {
			return errMsg{err: err}
		}

		return previewLoadedMsg{total: total}
	}
}

func (m appModel) executeDeleteCmd() tea.Cmd {
	return func() tea.Msg {
		job := engine.DeleteJob{
			GuildID:   m.selectedGuild.Id,
			GuildName: m.selectedGuild.Name,
			Channels:  m.selectedChannels,
			UserID:    m.user.Id,
		}

		m.engine.OnProgress = func(p engine.Progress) {
			program.Send(progressUpdateMsg{progress: p})
		}

		err := m.engine.Execute(context.Background(), job)
		if err != nil {
			return errMsg{err: err}
		}

		return deleteCompleteMsg{}
	}
}
