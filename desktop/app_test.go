package main

import (
	"testing"

	"github.com/inherentescapade/viaduct/export"
)

func msgs(n int) []export.Message {
	out := make([]export.Message, n)
	return out
}

func fixtureChannels() []export.Channel {
	return []export.Channel{
		{ID: "1", Type: "GUILD_TEXT", Name: "general", IndexName: "general", Messages: msgs(3)},
		{ID: "2", Type: "DM", Name: "Direct Message", Messages: msgs(2)},
		{ID: "3", Type: "GUILD_TEXT", Name: "Unknown channel", IndexName: "", Messages: msgs(1)}, // forgotten
		{ID: "4", Type: "GUILD_TEXT", Name: "empty", IndexName: "empty", Messages: nil},          // dropped: no messages
	}
}

func ids(chs []export.Channel) []string {
	out := make([]string, len(chs))
	for i, c := range chs {
		out[i] = c.ID
	}
	return out
}

func TestSelectExportChannels_DropsEmpty(t *testing.T) {
	got := selectExportChannels(fixtureChannels(), ImportRequest{})
	if want := []string{"1", "2", "3"}; !equal(ids(got), want) {
		t.Errorf("default selection = %v, want %v", ids(got), want)
	}
}

func TestSelectExportChannels_NoDMs(t *testing.T) {
	got := selectExportChannels(fixtureChannels(), ImportRequest{NoDMs: true})
	if want := []string{"1", "3"}; !equal(ids(got), want) {
		t.Errorf("no-dms selection = %v, want %v", ids(got), want)
	}
}

func TestSelectExportChannels_ForgottenOnly(t *testing.T) {
	got := selectExportChannels(fixtureChannels(), ImportRequest{Forgotten: true})
	if want := []string{"3"}; !equal(ids(got), want) {
		t.Errorf("forgotten selection = %v, want %v", ids(got), want)
	}
}

func TestSelectExportChannels_IncludeExclude(t *testing.T) {
	got := selectExportChannels(fixtureChannels(), ImportRequest{Include: []string{"general"}})
	if want := []string{"1"}; !equal(ids(got), want) {
		t.Errorf("include selection = %v, want %v", ids(got), want)
	}

	// Exclude by type token should drop the DM.
	got = selectExportChannels(fixtureChannels(), ImportRequest{Exclude: []string{"DM"}})
	if want := []string{"1", "3"}; !equal(ids(got), want) {
		t.Errorf("exclude selection = %v, want %v", ids(got), want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
