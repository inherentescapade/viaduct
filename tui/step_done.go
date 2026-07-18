package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type doneModel struct {
	deleted int
	failed  int
	elapsed time.Duration
	logPath string
}

func newDoneModel(prog progressModel, logPath string) doneModel {
	return doneModel{
		deleted: prog.totalDeleted(),
		failed:  prog.totalFailed(),
		elapsed: time.Since(prog.startTime).Round(time.Second),
		logPath: logPath,
	}
}

func (m doneModel) Update(msg tea.Msg) (doneModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m doneModel) View() string {
	var b strings.Builder

	b.WriteString(successStyle.Render("Done!") + "\n\n")

	b.WriteString(fmt.Sprintf("  Messages deleted:  %d\n", m.deleted))
	if m.failed > 0 {
		b.WriteString(fmt.Sprintf("  Messages failed:   %s\n", errorStyle.Render(fmt.Sprintf("%d", m.failed))))
	}
	b.WriteString(fmt.Sprintf("  Time elapsed:      %s\n", m.elapsed))

	if m.logPath != "" {
		b.WriteString(fmt.Sprintf("\n  Log: %s\n", dimStyle.Render(m.logPath)))
	}

	b.WriteString("\n" + dimStyle.Render("Press q or Enter to exit.") + "\n")

	return b.String()
}
