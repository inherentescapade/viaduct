package tui

import (
	"fmt"
	"github.com/inherentescapade/viaduct/engine"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type progressModel struct {
	progress  engine.Progress
	startTime time.Time
}

func newProgressModel() progressModel {
	return progressModel{
		startTime: time.Now(),
	}
}

func (m *progressModel) update(p engine.Progress) {
	m.progress = p
}

func (m progressModel) Update(msg tea.Msg) (progressModel, tea.Cmd) {
	return m, nil
}

func (m progressModel) View() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Deleting Messages") + "\n\n")

	p := m.progress

	if p.Total == 0 && !p.Done {
		b.WriteString(dimStyle.Render("  Searching for messages...") + "\n")
	} else if p.Done {
		if p.Error != nil {
			b.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v", p.Error)) + "\n")
		} else {
			b.WriteString(successStyle.Render(fmt.Sprintf("  Done — %d deleted, %d failed", p.Deleted, p.Failed)) + "\n")
		}
	} else {
		pct := float64(p.Deleted) / float64(p.Total) * 100
		width := 30
		filled := int(float64(width) * pct / 100)
		if filled > width {
			filled = width
		}
		bar := progressFullStyle.Render(strings.Repeat("█", filled)) +
			progressEmptyStyle.Render(strings.Repeat("░", width-filled))

		b.WriteString(fmt.Sprintf("  [%s] %.1f%%\n\n", bar, pct))
		b.WriteString(fmt.Sprintf("  Deleted:  %d / %d\n", p.Deleted, p.Total))
		if p.Failed > 0 {
			b.WriteString(fmt.Sprintf("  Failed:   %s\n", errorStyle.Render(fmt.Sprintf("%d", p.Failed))))
		}
		if p.Ignored > 0 {
			// A DM full of call notices / joins deletes nothing while it scans past
			// them; show the running count so the 0% bar doesn't read as frozen.
			b.WriteString(fmt.Sprintf("  Skipped:  %d system message(s) that can't be deleted\n", p.Ignored))
		}

		elapsed := time.Since(p.StartTime)
		rate := float64(0)
		if elapsed.Seconds() > 0 {
			rate = float64(p.Deleted) / elapsed.Seconds()
		}
		b.WriteString(fmt.Sprintf("  Rate:     %.1f/s\n", rate))
		b.WriteString(fmt.Sprintf("  Elapsed:  %s\n", elapsed.Round(time.Second)))

		if rate > 0 {
			remaining := float64(p.Total-p.Deleted) / rate
			b.WriteString(fmt.Sprintf("  ETA:      %s\n", time.Duration(remaining*float64(time.Second)).Round(time.Second)))
		}
	}

	b.WriteString("\n" + dimStyle.Render("  Ctrl+C to cancel") + "\n")

	return b.String()
}

func (m progressModel) totalDeleted() int {
	return m.progress.Deleted
}

func (m progressModel) totalFailed() int {
	return m.progress.Failed
}
