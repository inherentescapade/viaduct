package dates

import (
	"testing"
	"time"
)

func TestParseAbsolute(t *testing.T) {
	got, err := Parse("2024-01-02")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseRelative(t *testing.T) {
	before := time.Now().AddDate(0, 0, -30)
	got, err := Parse("30d")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Allow a small window since Parse calls time.Now() internally.
	if d := got.Sub(before); d < -time.Minute || d > time.Minute {
		t.Errorf("30d resolved to %v, expected ~%v", got, before)
	}
}

func TestParseRFC3339(t *testing.T) {
	if _, err := Parse("2024-01-02T15:04:05Z"); err != nil {
		t.Errorf("RFC3339 should parse: %v", err)
	}
}

func TestParseInvalid(t *testing.T) {
	for _, s := range []string{"", "tomorrow", "30x", "01-02-2024"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) should have failed", s)
		}
		if Valid(s) {
			t.Errorf("Valid(%q) should be false", s)
		}
	}
}
