// Package export parses a Discord "Data Package" export (the ZIP you download
// from Privacy & Safety → Request Data) so its messages can be deleted directly.
//
// The relevant layout inside the package is:
//
//	Messages/
//	  index.json                 {"<channelID>": "<human name>", ...}
//	  c<channelID>/
//	    channel.json             {"id": "...", "type": "DM"|"GUILD_TEXT"|..., "recipients": [...]}
//	    messages.json            [{"ID": <int>, "Timestamp": "2025-01-16 18:47:37", "Contents": "...", "Attachments": "..."}]
//
// Older exports ship messages.csv instead of messages.json; both are supported.
package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/inherentescapade/viaduct/discord"
)

// exportTimeLayout is the format Discord uses for the "Timestamp" column.
// It is UTC with no timezone marker.
const exportTimeLayout = "2006-01-02 15:04:05"

// Message is a single exported message belonging to the account owner.
type Message struct {
	ID          string
	Content     string
	Attachments string
	Timestamp   time.Time
}

// Channel groups the exported messages for one channel together with the
// metadata needed to decide whether to delete from it.
type Channel struct {
	ID   string
	Type string // raw export type: DM, GROUP_DM, GUILD_TEXT, GUILD_VOICE, PUBLIC_THREAD, GUILD_ANNOUNCEMENT, ...
	Name string // resolved display name from index.json, or a synthesized fallback
	// IndexName is the raw value from index.json (empty if the channel was not
	// listed). "Unknown channel..." here means the account has lost access.
	IndexName  string
	Recipients []string
	Messages   []Message
}

// IsDM reports whether the channel is a direct or group direct message.
func (c Channel) IsDM() bool {
	return c.Type == "DM" || c.Type == "GROUP_DM"
}

// IsForgotten reports whether this is a server channel the account can no
// longer access. Discord cannot resolve a name for such channels, so its
// index entry is missing or reads "Unknown channel...". DMs are never
// considered forgotten.
func (c Channel) IsForgotten() bool {
	if c.IsDM() {
		return false
	}
	return c.IndexName == "" || strings.HasPrefix(c.IndexName, "Unknown channel")
}

// Export is a parsed data package.
type Export struct {
	Root     string // the resolved Messages directory
	Channels []Channel
}

// MessageCount returns the total number of messages across all channels.
func (e *Export) MessageCount() int {
	n := 0
	for i := range e.Channels {
		n += len(e.Channels[i].Messages)
	}
	return n
}

// Load reads an export from path. path may point at the Messages directory
// itself, at the unzipped package root (which contains Messages/), or at any
// directory one level above either of those.
func Load(path string) (*Export, error) {
	dir, err := findMessagesDir(path)
	if err != nil {
		return nil, err
	}

	index := loadIndex(dir) // best effort; names are nice-to-have

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read messages directory: %w", err)
	}

	ex := &Export{Root: dir}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ch, ok, err := loadChannel(filepath.Join(dir, entry.Name()), index)
		if err != nil {
			return nil, fmt.Errorf("channel %s: %w", entry.Name(), err)
		}
		if ok {
			ex.Channels = append(ex.Channels, ch)
		}
	}

	if len(ex.Channels) == 0 {
		return nil, fmt.Errorf("no channels found under %s — is this a Discord data package?", dir)
	}

	// Stable, human-friendly ordering: named channels first, then by name/ID.
	sort.Slice(ex.Channels, func(i, j int) bool {
		return ex.Channels[i].Name < ex.Channels[j].Name
	})

	return ex, nil
}

// findMessagesDir resolves path to the directory that actually holds the
// per-channel folders.
func findMessagesDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot open %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", path)
	}

	candidates := []string{
		path,
		filepath.Join(path, "Messages"),
		filepath.Join(path, "messages"),
		filepath.Join(path, "package", "Messages"),
	}
	for _, c := range candidates {
		if isMessagesDir(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("%q does not look like a Discord data package (no Messages folder with channel data)", path)
}

// isMessagesDir reports whether dir contains a channel index or at least one
// per-channel folder with a channel.json.
func isMessagesDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), "channel.json")); err == nil {
			return true
		}
	}
	return false
}

// loadIndex reads index.json (channelID -> name). Missing or malformed index
// is tolerated; channel names just fall back to synthesized values.
func loadIndex(dir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return map[string]string{}
	}
	var index map[string]string
	if err := json.Unmarshal(data, &index); err != nil {
		return map[string]string{}
	}
	return index
}

type channelMeta struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Recipients []string `json:"recipients"`
}

// rawMessage mirrors one entry of messages.json. ID is decoded as a
// json.Number to preserve the full 64-bit snowflake (it would lose precision
// as a float64).
type rawMessage struct {
	ID          json.Number `json:"ID"`
	Timestamp   string      `json:"Timestamp"`
	Contents    string      `json:"Contents"`
	Attachments string      `json:"Attachments"`
}

// loadChannel parses one c<id> folder. ok is false (with no error) when the
// folder has no channel.json — such folders are silently skipped.
func loadChannel(dir string, index map[string]string) (Channel, bool, error) {
	metaData, err := os.ReadFile(filepath.Join(dir, "channel.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return Channel{}, false, nil
		}
		return Channel{}, false, fmt.Errorf("read channel.json: %w", err)
	}

	var meta channelMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return Channel{}, false, fmt.Errorf("parse channel.json: %w", err)
	}
	if meta.ID == "" {
		// Derive the ID from the folder name (c<id>) as a last resort.
		meta.ID = strings.TrimPrefix(filepath.Base(dir), "c")
	}

	messages, err := loadMessages(dir)
	if err != nil {
		return Channel{}, false, err
	}

	indexName := index[meta.ID]
	ch := Channel{
		ID:         meta.ID,
		Type:       meta.Type,
		IndexName:  indexName,
		Recipients: meta.Recipients,
		Messages:   messages,
	}
	ch.Name = displayName(ch)
	return ch, true, nil
}

// loadMessages reads messages.json, falling back to messages.csv.
func loadMessages(dir string) ([]Message, error) {
	jsonPath := filepath.Join(dir, "messages.json")
	if data, err := os.ReadFile(jsonPath); err == nil {
		var raws []rawMessage
		if err := json.Unmarshal(data, &raws); err != nil {
			return nil, fmt.Errorf("parse messages.json: %w", err)
		}
		out := make([]Message, 0, len(raws))
		for _, r := range raws {
			id := r.ID.String()
			if id == "" {
				continue
			}
			out = append(out, Message{
				ID:          id,
				Content:     r.Contents,
				Attachments: r.Attachments,
				Timestamp:   parseTimestamp(id, r.Timestamp),
			})
		}
		return out, nil
	}

	csvPath := filepath.Join(dir, "messages.csv")
	if data, err := os.ReadFile(csvPath); err == nil {
		return parseCSV(data)
	}

	// No message file: a channel folder with only metadata. Treat as empty.
	return nil, nil
}

// parseCSV handles the messages.csv format found in older Discord data
// packages (newer packages ship messages.json). Its header is
// ID,Timestamp,Contents,Attachments.
func parseCSV(data []byte) ([]Message, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse messages.csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return row[i]
		}
		return ""
	}

	out := make([]Message, 0, len(rows)-1)
	for _, row := range rows[1:] {
		id := strings.TrimSpace(get(row, "id"))
		if id == "" {
			continue
		}
		out = append(out, Message{
			ID:          id,
			Content:     get(row, "contents"),
			Attachments: get(row, "attachments"),
			Timestamp:   parseTimestamp(id, get(row, "timestamp")),
		})
	}
	return out, nil
}

// parseTimestamp prefers the snowflake-derived time (exact and timezone-free)
// and falls back to the textual Timestamp column.
func parseTimestamp(id, raw string) time.Time {
	if t, err := discord.SnowflakeToTime(id); err == nil {
		return t
	}
	if t, err := time.Parse(exportTimeLayout, strings.TrimSpace(raw)); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// displayName produces a readable label for a channel, preferring the
// index.json name and otherwise synthesizing one from the type.
func displayName(c Channel) string {
	if c.IndexName != "" {
		return c.IndexName
	}
	switch c.Type {
	case "DM":
		return "Direct Message"
	case "GROUP_DM":
		return "Group DM"
	default:
		return "Unknown channel"
	}
}
