package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
)

// This file defines the JSON-friendly data-transfer objects exchanged with the
// React frontend, plus converters from the engine/discord domain types. The
// frontend never sees raw engine types; most matter is cosmetic, but two
// conversions are load-bearing:
//
//   - engine.Progress.Error is a Go `error` (an interface) which marshals to a
//     useless `{}`. ProgressDTO.Error is a plain string.
//   - export.Channel embeds the full []Message slice (potentially 100k+
//     entries). We only ever ship counts to JS; the full export stays Go-side.

// ---- Requests (JS -> Go) ----

// DeleteRequest drives both Preview, Enumerate (dry-run) and StartDelete in the
// live search-API flow. Date fields are raw expressions parsed via dates.Parse.
type DeleteRequest struct {
	GuildID    string   `json:"guildId"`
	GuildName  string   `json:"guildName"`
	ChannelIDs []string `json:"channelIds"` // resolved to []discord.Channel Go-side (required for DMs)
	Before     string   `json:"before"`
	After      string   `json:"after"`
	MaxID      string   `json:"maxId"`
	MinID      string   `json:"minId"`
	// IncludePinned deletes pinned messages too; false (the default) keeps them.
	IncludePinned bool `json:"includePinned"`
}

// ImportRequest drives CountImport and StartImport. The selector semantics
// mirror the CLI: include/exclude tokens match a channel ID, a case-insensitive
// name substring, or a type (DM, GUILD_TEXT, ...).
type ImportRequest struct {
	Include   []string `json:"include"`
	Exclude   []string `json:"exclude"`
	Forgotten bool     `json:"forgotten"`
	NoDMs     bool     `json:"noDms"`
	Before    string   `json:"before"`
	After     string   `json:"after"`
}

// ---- Responses (Go -> JS) ----

type UserDTO struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"globalName"`
	AvatarURL  string `json:"avatarUrl"`
}

type TokenStateDTO struct {
	HasToken bool `json:"hasToken"`
	BotMode  bool `json:"botMode"`
}

type GuildDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
	Owner   bool   `json:"owner"`
	IsDM    bool   `json:"isDm"`
}

type ChannelDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      int    `json:"type"`
	Nsfw      bool   `json:"nsfw"`
	AvatarURL string `json:"avatarUrl"`
}

type ChannelExportDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	MessageCount int    `json:"messageCount"`
	IsDM         bool   `json:"isDm"`
	IsForgotten  bool   `json:"isForgotten"`
}

type ExportSummaryDTO struct {
	Root          string             `json:"root"`
	TotalMessages int                `json:"totalMessages"`
	Channels      []ChannelExportDTO `json:"channels"`
}

type CountDTO struct {
	Messages int `json:"messages"`
	Channels int `json:"channels"`
}

type FailReasonDTO struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type MessageDTO struct {
	ID              string          `json:"id"`
	ChannelID       string          `json:"channelId"`
	ChannelName     string          `json:"channelName"`
	Content         string          `json:"content"`
	Timestamp       string          `json:"timestamp"`
	AuthorName      string          `json:"authorName"`
	AuthorAvatarURL string          `json:"authorAvatarUrl"`
	Attachments     []AttachmentDTO `json:"attachments"`
}

type AttachmentDTO struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	IsImage     bool   `json:"isImage"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Filename    string `json:"filename"`
}

type ConfigInfoDTO struct {
	ConfigDir string `json:"configDir"`
	LogDir    string `json:"logDir"`
	LogBytes  int64  `json:"logBytes"`
}

type PrefsDTO struct {
	SkipConfirm bool `json:"skipConfirm"`
	PreScan     bool `json:"preScan"`
}

// ProgressDTO is the live progress payload for both deletion modes. Rate and
// ETA are computed Go-side so every consumer agrees on the numbers.
type ProgressDTO struct {
	GuildName   string  `json:"guildName"`
	Channel     string  `json:"channel"`
	Total       int     `json:"total"`
	Deleted     int     `json:"deleted"`
	Failed      int     `json:"failed"`
	Skipped     int     `json:"skipped"`
	Ignored     int     `json:"ignored"` // undeletable system messages scanned past (call notices, joins, pins)
	RateLimited int     `json:"rateLimited"`
	Done        bool    `json:"done"`
	Error       string  `json:"error"`
	ElapsedMs   int64   `json:"elapsedMs"`
	RatePerSec  float64 `json:"ratePerSec"`
	EtaMs       int64   `json:"etaMs"`
	// Starting is set on the synthetic event emitted the instant a run begins,
	// before the engine has counted anything. It lets the UI show an
	// "indexing…" state instead of looking frozen during deep-index waits.
	Starting bool `json:"starting"`
}

// ---- Converters ----

func toUserDTO(u *discord.User) *UserDTO {
	if u == nil {
		return nil
	}
	return &UserDTO{ID: u.Id, Username: u.Username, GlobalName: u.GlobalName, AvatarURL: userAvatarURL(u.Id, u.Avatar)}
}

func toMessageDTO(m discord.Message, channelName string) MessageDTO {
	dto := MessageDTO{
		ID:              m.Id,
		ChannelID:       m.ChannelId,
		ChannelName:     channelName,
		Content:         m.Content,
		Timestamp:       m.Timestamp.Format(time.RFC3339),
		AuthorName:      m.Author.GlobalName,
		AuthorAvatarURL: userAvatarURL(m.Author.Id, m.Author.Avatar),
	}
	if dto.AuthorName == "" {
		dto.AuthorName = m.Author.Username
	}
	for _, a := range m.Attachments {
		// Prefer the proxy URL for loading thumbnails; it is CDN-cached.
		url := a.ProxyUrl
		if url == "" {
			url = a.Url
		}
		dto.Attachments = append(dto.Attachments, AttachmentDTO{
			URL:         url,
			ContentType: a.ContentType,
			IsImage:     isImageAttachment(a),
			Width:       a.Width,
			Height:      a.Height,
			Filename:    a.Filename,
		})
	}
	return dto
}

// isImageAttachment reports whether an attachment is a displayable image, by its
// content type or, as a fallback, a common image file extension.
func isImageAttachment(a discord.Attachment) bool {
	if strings.HasPrefix(a.ContentType, "image/") {
		return true
	}
	lower := strings.ToLower(a.Filename)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

const cdnBase = "https://cdn.discordapp.com"

// guildIconURL builds a server icon URL, or "" when the guild has no icon.
func guildIconURL(id, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%s/icons/%s/%s.png?size=64", cdnBase, id, hash)
}

// userAvatarURL builds a user avatar URL, or "" when the user has no avatar.
func userAvatarURL(id, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%s/avatars/%s/%s.png?size=64", cdnBase, id, hash)
}

// groupIconURL builds a group-DM icon URL, or "" when there is none.
func groupIconURL(channelID, hash string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%s/channel-icons/%s/%s.png?size=64", cdnBase, channelID, hash)
}

func toFailReasonDTOs(in []engine.FailReason) []FailReasonDTO {
	out := make([]FailReasonDTO, 0, len(in))
	for _, r := range in {
		out = append(out, FailReasonDTO{Reason: r.Reason, Count: r.Count})
	}
	return out
}

func toProgressDTO(p engine.Progress) ProgressDTO {
	d := ProgressDTO{
		GuildName:   p.GuildName,
		Channel:     p.Channel,
		Total:       p.Total,
		Deleted:     p.Deleted,
		Failed:      p.Failed,
		Skipped:     p.Skipped,
		Ignored:     p.Ignored,
		RateLimited: p.RateLimited,
		Done:        p.Done,
	}
	if p.Error != nil {
		d.Error = p.Error.Error()
	}
	if !p.StartTime.IsZero() {
		elapsed := time.Since(p.StartTime)
		d.ElapsedMs = elapsed.Milliseconds()
		if elapsed.Seconds() > 0 {
			d.RatePerSec = float64(p.Deleted) / elapsed.Seconds()
		}
		processed := p.Deleted + p.Skipped + p.Failed
		remaining := p.Total - processed
		if d.RatePerSec > 0 && remaining > 0 {
			d.EtaMs = int64(float64(remaining) / d.RatePerSec * 1000)
		}
	}
	return d
}

// channelLabel produces a display name for a channel. 1:1 DMs show the other
// person's name; group DMs keep their title (or the joined recipient names);
// guild channels use their channel name.
func channelLabel(ch discord.Channel) string {
	switch ch.Type {
	case 1: // DM
		if len(ch.Recipients) > 0 {
			return recipientName(ch.Recipients[0])
		}
		return "Direct Message"
	case 3: // group DM
		if n := strings.TrimSpace(ch.Name); n != "" {
			return n
		}
		if len(ch.Recipients) > 0 {
			names := make([]string, 0, len(ch.Recipients))
			for i := range ch.Recipients {
				names = append(names, recipientName(ch.Recipients[i]))
			}
			return strings.Join(names, ", ")
		}
		return "Group DM"
	default:
		if n := strings.TrimSpace(ch.Name); n != "" {
			return n
		}
		return "channel " + ch.Id
	}
}

func recipientName(u discord.User) string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	if u.Username != "" {
		return u.Username
	}
	return "Unknown"
}

// channelAvatarURL returns a recipient avatar (1:1 DM) or group icon (group DM),
// or "" for guild channels / when no image is available.
func channelAvatarURL(ch discord.Channel) string {
	switch ch.Type {
	case 1:
		if len(ch.Recipients) > 0 {
			return userAvatarURL(ch.Recipients[0].Id, ch.Recipients[0].Avatar)
		}
	case 3:
		return groupIconURL(ch.Id, ch.Icon)
	}
	return ""
}
