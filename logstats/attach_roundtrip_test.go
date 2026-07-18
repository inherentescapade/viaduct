package logstats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inherentescapade/viaduct/discord"
)

func TestAttachmentRoundTrip(t *testing.T) {
	var m discord.Message
	m.Id = "100"
	m.ChannelId = "c1"
	m.Content = "pic"
	m.Timestamp = time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	m.Attachments = []discord.Attachment{{Id: "a1", Filename: "x.png", ContentType: "image/png"}}
	b, _ := json.Marshal(m)
	t.Logf("logged line: %s", b)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "delete_2024-05-01_000000.ndjson"), append(b, '\n'), 0600)
	st, err := Parse(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.WithAttachments != 1 || st.Attachments != 1 {
		t.Fatalf("attachments not counted: withAttachments=%d attachments=%d", st.WithAttachments, st.Attachments)
	}
}
