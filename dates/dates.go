// Package dates parses the date expressions accepted across viaduct's
// interfaces (CLI flags, TUI filter fields, and the desktop GUI).
//
// Three forms are supported:
//
//	2024-01-01            calendar date (YYYY-MM-DD), midnight UTC
//	2024-01-01T15:04:05Z  full RFC3339 timestamp
//	30d / 24h / 60m       relative: that many days/hours/minutes ago
//
// Keeping this in one importable package means the CLI, TUI, and GUI all agree
// on exactly what "30d" means.
package dates

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var relativeRe = regexp.MustCompile(`^(\d+)([dhm])$`)

// Parse interprets s as a date/time. Relative values ("30d", "24h", "60m") are
// resolved against the current time. Returns an error if s matches none of the
// supported formats.
func Parse(s string) (time.Time, error) {
	if m := relativeRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "d":
			return time.Now().AddDate(0, 0, -n), nil
		case "h":
			return time.Now().Add(-time.Duration(n) * time.Hour), nil
		case "m":
			return time.Now().Add(-time.Duration(n) * time.Minute), nil
		}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("use YYYY-MM-DD, RFC3339, or relative like 30d/24h/60m")
}

// Valid reports whether s is a parseable date expression. Useful for inline UI
// validation without caring about the resulting time.
func Valid(s string) bool {
	_, err := Parse(s)
	return err == nil
}
