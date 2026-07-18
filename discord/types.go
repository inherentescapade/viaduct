package discord

import "time"

type SearchResponse struct {
	AnalyticsId              string      `json:"analytics_id"`
	DoingDeepHistoricalIndex bool        `json:"doing_deep_historical_index"`
	TotalResults             int         `json:"total_results"`
	Messages                 [][]Message `json:"messages"`
}

type Message struct {
	Id        string `json:"id"`
	Type      int    `json:"type"`
	Content   string `json:"content"`
	ChannelId string `json:"channel_id"`
	Author    struct {
		Id                   string      `json:"id"`
		Username             string      `json:"username"`
		GlobalName           string      `json:"global_name"`
		Avatar               string      `json:"avatar"`
		AvatarDecorationData interface{} `json:"avatar_decoration_data"`
		Discriminator        string      `json:"discriminator"`
		PublicFlags          int         `json:"public_flags"`
		PrimaryGuild         interface{} `json:"primary_guild"`
		Clan                 interface{} `json:"clan"`
	} `json:"author"`
	Attachments     []Attachment  `json:"attachments"`
	Embeds          []interface{} `json:"embeds"`
	Mentions        []interface{} `json:"mentions"`
	MentionRoles    []interface{} `json:"mention_roles"`
	MentionEveryone bool          `json:"mention_everyone"`
	Pinned          bool          `json:"pinned"`
	Tts             bool          `json:"tts"`
	Timestamp       time.Time     `json:"timestamp"`
	EditedTimestamp interface{}   `json:"edited_timestamp"`
	Flags           int           `json:"flags"`
	Components      []interface{} `json:"components"`
	Hit             bool          `json:"hit"`
}

// Attachment is a file attached to a message. Discord populates url/proxy_url
// for the file; content_type and width/height are present for media.
type Attachment struct {
	Id          string `json:"id"`
	Filename    string `json:"filename"`
	Size        int    `json:"size"`
	Url         string `json:"url"`
	ProxyUrl    string `json:"proxy_url"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type Messages []Message

type Channel struct {
	Id                   string    `json:"id"`
	Type                 int       `json:"type"`
	LastMessageId        string    `json:"last_message_id"`
	Flags                int       `json:"flags"`
	LastPinTimestamp     time.Time `json:"last_pin_timestamp"`
	GuildId              string    `json:"guild_id"`
	Name                 string    `json:"name"`
	ParentId             string    `json:"parent_id"`
	RateLimitPerUser     int       `json:"rate_limit_per_user"`
	Topic                string    `json:"topic"`
	Position             int       `json:"position"`
	PermissionOverwrites ChannelPermissionOverwrite
	Nsfw                 bool        `json:"nsfw"`
	IconEmoji            IconEmoji   `json:"icon_emoji"`
	ThemeColor           interface{} `json:"theme_color"`
	// Recipients is populated for DM/group-DM channels (from /users/@me/channels).
	Recipients []User `json:"recipients"`
	// Icon is the group-DM icon hash, if any.
	Icon string `json:"icon"`
}

type IconEmoji struct {
	Id   interface{} `json:"id"`
	Name string      `json:"name"`
}

type ChannelPermissionOverwrite struct {
	Id    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

type Channels []Channel

type Guilds []Guild

type Guild struct {
	Id             string   `json:"id"`
	Name           string   `json:"name"`
	Icon           string   `json:"icon"`
	Banner         string   `json:"banner"`
	Owner          bool     `json:"owner"`
	Permissions    int      `json:"permissions"`
	PermissionsNew string   `json:"permissions_new"`
	Features       []string `json:"features"`
}

type DeleteResponse struct {
	Message    string  `json:"message"`
	RetryAfter float64 `json:"retry_after"`
	Global     bool    `json:"global"`
	Code       int     `json:"code"`
}

type User struct {
	Id                   string        `json:"id"`
	Username             string        `json:"username"`
	Avatar               string        `json:"avatar"`
	Discriminator        string        `json:"discriminator"`
	PublicFlags          int           `json:"public_flags"`
	Flags                int           `json:"flags"`
	Banner               interface{}   `json:"banner"`
	AccentColor          interface{}   `json:"accent_color"`
	GlobalName           string        `json:"global_name"`
	AvatarDecorationData interface{}   `json:"avatar_decoration_data"`
	BannerColor          interface{}   `json:"banner_color"`
	Clan                 interface{}   `json:"clan"`
	PrimaryGuild         interface{}   `json:"primary_guild"`
	MfaEnabled           bool          `json:"mfa_enabled"`
	Locale               string        `json:"locale"`
	PremiumType          int           `json:"premium_type"`
	Email                string        `json:"email"`
	Verified             bool          `json:"verified"`
	Phone                string        `json:"phone"`
	NsfwAllowed          bool          `json:"nsfw_allowed"`
	LinkedUsers          []interface{} `json:"linked_users"`
	PurchasedFlags       int           `json:"purchased_flags"`
	Bio                  string        `json:"bio"`
	AuthenticatorTypes   []int         `json:"authenticator_types"`
}
