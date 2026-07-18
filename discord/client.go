package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DiscordApi = "https://discord.com/api/"

type Client struct {
	http *discordHTTP

	// Rate limiting depends on the token type. Bot tokens hit Discord's
	// documented per-route limits keyed by channel_id, so each channel gets its
	// own self-tuning limiter and channels delete in parallel. USER tokens are
	// instead governed by Discord's stricter, account-wide anti-spam delete
	// limit, so they share ONE limiter — independent per-channel limiters would
	// each draw on the same global budget without coordinating and just 429 each
	// other. bot selects which model applies.
	bot       bool
	limiterMu sync.Mutex
	limiters  map[string]*AdaptiveLimiter // per-channel (bot tokens)
	shared    *AdaptiveLimiter            // one global limiter (user tokens)
	startGap  time.Duration
	minGap    time.Duration
	maxGap    time.Duration

	// Discord rate-limits the search endpoint per user token, not per guild or
	// channel — it's an expensive query against a shared search index, so the
	// budget is account-wide regardless of which guild or channel is being
	// searched. All search calls on this Client therefore share ONE limiter so
	// concurrent scopes coordinate pacing instead of each independently retrying
	// into the others' backoff.
	searchLimiter *AdaptiveLimiter

	// metaLimiter paces the low-volume metadata reads (guild list, channels,
	// current user) that the UI fires as it loads. Discord rate-limits these too,
	// and firing them back-to-back on startup can trip a 429; routing them all
	// through one shared limiter spaces the burst out and lets a 429's retry_after
	// coordinate every read instead of each one retrying blindly into the others.
	metaLimiter *AdaptiveLimiter

	// OnRateEvent, if set, is called whenever pacing is adjusted, so callers can
	// show what the tool is deciding. Must be safe for concurrent use.
	OnRateEvent func(RateEvent)
}

func NewClient(token string, bot bool, httpClient *http.Client) *Client {
	if bot {
		token = "Bot " + token
	}
	c := &Client{
		http: &discordHTTP{
			client: httpClient,
			token:  token,
		},
		bot:      bot,
		limiters: make(map[string]*AdaptiveLimiter),
		// Start gently and let pacing tune itself from there. maxGap is capped low
		// so a saturated channel never stalls for tens of seconds — a 429's
		// retry_after (honoured separately) covers any longer pause.
		startGap: 800 * time.Millisecond,
		minGap:   300 * time.Millisecond,
		maxGap:   6 * time.Second,
	}
	if !bot {
		// User token: one shared limiter coordinates the account-wide delete rate.
		c.shared = NewAdaptiveLimiter(c.startGap, c.minGap, c.maxGap)
	}
	c.searchLimiter = NewAdaptiveLimiter(c.startGap, c.minGap, c.maxGap)
	// Metadata reads are cheap and infrequent, so pace them tighter than deletes:
	// snappy enough that the UI loads quickly, spaced enough not to burst into a
	// 429. The first read never waits (see AdaptiveLimiter.Wait).
	c.metaLimiter = NewAdaptiveLimiter(500*time.Millisecond, 250*time.Millisecond, 8*time.Second)
	return c
}

// PerChannelLimits reports whether deletes are rate-limited per channel (bot
// tokens) rather than by one account-wide limit (user tokens). Callers use it to
// describe accurately whether channels gain throughput from running in parallel.
func (c *Client) PerChannelLimits() bool { return c.bot }

// limiterFor returns the limiter governing deletes to channelID: a per-channel
// limiter for bot tokens, or the single shared limiter for user tokens. Safe for
// concurrent callers.
func (c *Client) limiterFor(channelID string) *AdaptiveLimiter {
	if !c.bot {
		return c.shared // user tokens: account-wide limit, coordinate globally
	}
	c.limiterMu.Lock()
	defer c.limiterMu.Unlock()
	l := c.limiters[channelID]
	if l == nil {
		l = NewAdaptiveLimiter(c.startGap, c.minGap, c.maxGap)
		c.limiters[channelID] = l
	}
	return l
}

// getJSON performs a GET whose body decodes into out, transparently handling
// Discord's rate limiting. These are low-volume metadata reads (guild list,
// channels, current user) fired as the UI loads — but Discord still rate-limits
// them, and a 429 here is transient, not a real failure. Requests are paced
// through the shared metaLimiter so a startup burst doesn't trip the limit, and
// a 429 is retried after honouring retry_after rather than surfacing a raw
// status code. Only if it stays rate limited past our retries do we return a
// clean, actionable message.
func (c *Client) getJSON(url string, out any) error {
	const maxRetries = 4
	rateLimited := false
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.metaLimiter.Wait()

		resp, err := c.http.Do("GET", url)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return errors.New("could not read response from discord")
		}

		if resp.StatusCode == 429 {
			rateLimited = true
			var rl DeleteResponse // reused as Discord's rate-limit envelope (retry_after)
			_ = json.Unmarshal(body, &rl)
			c.metaLimiter.OnRateLimited(rl.RetryAfter)
			continue
		}

		if resp.StatusCode != 200 {
			return fmt.Errorf("discord returned status %d: %s", resp.StatusCode, string(body))
		}

		c.metaLimiter.OnSuccess()
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("could not decode response from discord: %w\nBody: %s", err, string(body))
		}
		return nil
	}

	if rateLimited {
		return errors.New("discord is rate limiting requests right now — please wait a few seconds and try again")
	}
	return errors.New("could not reach discord — please try again")
}

func (c *Client) GetGuilds() ([]Guild, error) {
	var gs Guilds
	if err := c.getJSON(DiscordApi+"users/@me/guilds", &gs); err != nil {
		return nil, err
	}
	return gs, nil
}

func (c *Client) GetChannels(guildId string) (Channels, error) {
	var channels Channels

	var endpoint string
	if guildId == "@me" {
		endpoint = DiscordApi + "users/@me/channels"
	} else {
		endpoint = fmt.Sprintf("%sguilds/%s/channels", DiscordApi, guildId)
	}

	if err := c.getJSON(endpoint, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// GetChannel fetches a single channel by ID (GET /channels/{id}). It returns
// the channel object — a name for guild channels, recipients for DMs — which
// the Insights view uses to label deletion targets recorded only by ID.
func (c *Client) GetChannel(channelID string) (*Channel, error) {
	var ch Channel
	if err := c.getJSON(fmt.Sprintf("%schannels/%s", DiscordApi, channelID), &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

func (c *Client) DeleteMessages(channelId string, messageId string) (*DeleteResponse, error) {
	target := fmt.Sprintf("%schannels/%s/messages/%s", DiscordApi, channelId, messageId)

	lim := c.limiterFor(channelId)

	for retries := 0; retries < 3; retries++ {
		// Wait honours both the channel's adaptive gap and any retry_after floor
		// set by a previous 429 on this channel.
		lim.Wait()

		resp, err := c.http.Do("DELETE", target)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 204 {
			resp.Body.Close()
			if changed, gap := lim.OnSuccess(); changed && c.OnRateEvent != nil {
				c.OnRateEvent(RateEvent{ChannelID: channelId, SpedUp: true, Gap: gap})
			}
			return nil, nil
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, errors.New("could not read response from discord")
		}

		var deleteResponse DeleteResponse
		err = json.Unmarshal(body, &deleteResponse)
		if err != nil {
			return nil, fmt.Errorf("could not decode response from discord (status %d): %w\nBody: %s", resp.StatusCode, err, string(body))
		}

		if resp.StatusCode == 429 {
			gap := lim.OnRateLimited(deleteResponse.RetryAfter)
			if c.OnRateEvent != nil {
				c.OnRateEvent(RateEvent{ChannelID: channelId, SpedUp: false, Gap: gap, RetryAfter: deleteResponse.RetryAfter})
			}
			continue
		}

		return &deleteResponse, fmt.Errorf("discord error: %s (code: %d)", deleteResponse.Message, deleteResponse.Code)
	}

	return nil, errors.New("max retries exceeded")
}

func (c *Client) GetDMMessages(ctx context.Context, channelId string, userId *string, maxid, minid *string, offset int) (*SearchResponse, error) {
	var messages SearchResponse

	baseURL := fmt.Sprintf("%schannels/%s/messages/search", DiscordApi, channelId)
	params := make(url.Values)

	if userId != nil && *userId != "" {
		params.Add("author_id", *userId)
	}
	if maxid != nil && *maxid != "" {
		params.Add("max_id", *maxid)
	}
	if minid != nil && *minid != "" {
		params.Add("min_id", *minid)
	}
	if offset > 0 {
		params.Add("offset", strconv.Itoa(offset))
	}

	target := baseURL
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		c.searchLimiter.Wait()

		resp, err := c.http.Do("GET", target)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.New("could not read response from discord")
		}

		if resp.StatusCode == 429 {
			// A 429 on search is transient rate limiting, not a real failure — keep
			// waiting (with the adaptive limiter's growing backoff) rather than
			// giving up. The caller's context is the only way out of a search that
			// never clears.
			var rateLimitResp DeleteResponse
			if jsonErr := json.Unmarshal(body, &rateLimitResp); jsonErr == nil {
				c.searchLimiter.OnRateLimited(rateLimitResp.RetryAfter)
				continue
			}
			c.searchLimiter.OnRateLimited(0)
			continue
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("discord returned status %d: %s", resp.StatusCode, string(body))
		}

		c.searchLimiter.OnSuccess()
		err = json.Unmarshal(body, &messages)
		if err != nil {
			return nil, fmt.Errorf("could not decode response from discord: %w\nBody: %s", err, string(body))
		}

		return &messages, nil
	}
}

func (c *Client) GetMessages(ctx context.Context, guildId string, channelId *string, userId *string, maxid, minid *string, offset int, includeNsfw bool) (*SearchResponse, error) {
	var messages SearchResponse

	baseURL := fmt.Sprintf("%sguilds/%s/messages/search", DiscordApi, guildId)
	params := make(url.Values)

	if channelId != nil && *channelId != "" {
		params.Add("channel_id", *channelId)
	}
	if userId != nil && *userId != "" {
		params.Add("author_id", *userId)
	}
	if maxid != nil && *maxid != "" {
		params.Add("max_id", *maxid)
	}
	if minid != nil && *minid != "" {
		params.Add("min_id", *minid)
	}
	if offset > 0 {
		params.Add("offset", strconv.Itoa(offset))
	}
	if includeNsfw {
		params.Add("include_nsfw", "true")
	}

	target := baseURL
	if len(params) > 0 {
		target += "?" + params.Encode()
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		c.searchLimiter.Wait()

		resp, err := c.http.Do("GET", target)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.New("could not read response from discord")
		}

		if resp.StatusCode == 429 {
			// A 429 on search is transient rate limiting, not a real failure — keep
			// waiting (with the adaptive limiter's growing backoff) rather than
			// giving up. The caller's context is the only way out of a search that
			// never clears.
			var rateLimitResp DeleteResponse
			if jsonErr := json.Unmarshal(body, &rateLimitResp); jsonErr == nil {
				c.searchLimiter.OnRateLimited(rateLimitResp.RetryAfter)
				continue
			}
			c.searchLimiter.OnRateLimited(0)
			continue
		}

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("discord returned status %d: %s", resp.StatusCode, string(body))
		}

		c.searchLimiter.OnSuccess()
		err = json.Unmarshal(body, &messages)
		if err != nil {
			return nil, fmt.Errorf("could not decode response from discord: %w\nBody: %s", err, string(body))
		}

		return &messages, nil
	}
}

func (c *Client) GetMe() (*User, error) {
	var user User
	if err := c.getJSON(DiscordApi+"users/@me", &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func TimeToSnowflake(t time.Time) string {
	const discordEpoch = 1420070400000
	timestamp := t.UnixMilli() - discordEpoch
	return strconv.FormatInt(timestamp<<22, 10)
}

func SnowflakeToTime(snowflake string) (time.Time, error) {
	const discordEpoch = 1420070400000
	id, err := strconv.ParseInt(snowflake, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid snowflake: %w", err)
	}
	timestamp := (id >> 22) + discordEpoch
	return time.UnixMilli(timestamp), nil
}

func (c *Client) ValidateToken() (*User, error) {
	user, err := c.GetMe()
	if err != nil {
		return nil, fmt.Errorf("token check failed: %w", err)
	}
	return user, nil
}

func (c *Client) ResolveGuild(nameOrID string, guilds []Guild) *Guild {
	for i := range guilds {
		if guilds[i].Id == nameOrID {
			return &guilds[i]
		}
	}
	for i := range guilds {
		if strings.EqualFold(guilds[i].Name, nameOrID) {
			return &guilds[i]
		}
	}
	lower := strings.ToLower(nameOrID)
	for i := range guilds {
		if strings.Contains(strings.ToLower(guilds[i].Name), lower) {
			return &guilds[i]
		}
	}
	return nil
}

func (c *Client) ResolveChannel(nameOrID string, channels []Channel) *Channel {
	for i := range channels {
		if channels[i].Id == nameOrID {
			return &channels[i]
		}
	}
	lower := strings.ToLower(nameOrID)
	for i := range channels {
		if strings.EqualFold(channels[i].Name, nameOrID) {
			return &channels[i]
		}
	}
	for i := range channels {
		if strings.Contains(strings.ToLower(channels[i].Name), lower) {
			return &channels[i]
		}
	}
	return nil
}

func (c *Client) CountMessages(ctx context.Context, guildId string, channelId *string, userId *string, maxid, minid *string) (int, error) {
	resp, err := c.GetMessages(ctx, guildId, channelId, userId, maxid, minid, 0, true)
	if err != nil {
		return 0, err
	}
	return resp.TotalResults, nil
}

// ProbeRecord holds the result of one DELETE probe request. (Discord does not
// return X-RateLimit-* headers for user-token deletes, so there's nothing to
// capture from headers — everything is derived from response status and timing.)
type ProbeRecord struct {
	SentAt     time.Time
	DoneAt     time.Time     // when response was received
	RTT        time.Duration // DoneAt - SentAt
	Gap        time.Duration // SentAt - previous SentAt (zero for first)
	Status     int
	RetryAfter float64 // seconds, from body when status 429
}

// ProbeDeleteReal deletes real messages and records rate limit observations.
// firstChannelID/firstMsgID is the first message; next() supplies subsequent
// ones. It stops after maxRequests or the first 429.
func (c *Client) ProbeDeleteReal(firstChannelID, firstMsgID string, maxRequests int, delay time.Duration, next func() (string, string, bool)) ([]ProbeRecord, error) {
	type msg struct{ chID, msgID string }
	queue := []msg{{firstChannelID, firstMsgID}}

	records := make([]ProbeRecord, 0, maxRequests)
	for i := 0; i < maxRequests; i++ {
		if i >= len(queue) {
			chID, msgID, ok := next()
			if !ok {
				break
			}
			queue = append(queue, msg{chID, msgID})
		}

		if i > 0 && delay > 0 {
			time.Sleep(delay)
		}

		m := queue[i]
		target := fmt.Sprintf("%schannels/%s/messages/%s", DiscordApi, m.chID, m.msgID)

		rec := ProbeRecord{SentAt: time.Now()}
		if i > 0 {
			rec.Gap = rec.SentAt.Sub(records[i-1].SentAt)
		}

		resp, err := c.http.Do("DELETE", target)
		rec.DoneAt = time.Now()
		rec.RTT = rec.DoneAt.Sub(rec.SentAt)
		if err != nil {
			return records, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		rec.Status = resp.StatusCode

		if resp.StatusCode == 429 {
			var dr DeleteResponse
			if err := json.Unmarshal(body, &dr); err == nil {
				rec.RetryAfter = dr.RetryAfter
			}
			records = append(records, rec)
			break
		}

		records = append(records, rec)
	}
	return records, nil
}

// ProbeSustained deletes real messages paced at a fixed inter-request gap for a
// sustained run, riding *through* 429s instead of stopping at the first one.
// This is what actually characterises a rolling-window limit over time: it keeps
// sending at `gap` for `duration` (or until `maxRequests` confirmed deletes, or
// the message pool is exhausted), and on a 429 it honours retry_after and
// re-attempts the SAME message so no probe is wasted. Every attempt — including
// each 429 — is returned as a record, so the caller can count rate-limit hits
// and compute the effective sustained throughput.
func (c *Client) ProbeSustained(duration time.Duration, gap time.Duration, maxRequests int, next func() (string, string, bool)) ([]ProbeRecord, error) {
	// Only hint a modest initial capacity — maxRequests is a safety ceiling that
	// may be huge, so allocating it up front would needlessly reserve gigabytes.
	capHint := maxRequests
	if capHint > 1024 {
		capHint = 1024
	}
	records := make([]ProbeRecord, 0, capHint)

	start := time.Now()
	deletes := 0
	haveMsg := false
	var curCh, curMsg string

	var lastSent time.Time
	for deletes < maxRequests && time.Since(start) < duration {
		if !haveMsg {
			ch, m, ok := next()
			if !ok {
				break
			}
			curCh, curMsg, haveMsg = ch, m, true
		}

		// Pace requests at the requested gap, measured request-start to
		// request-start so RTT doesn't inflate the effective interval.
		if gap > 0 && !lastSent.IsZero() {
			if elapsed := time.Since(lastSent); elapsed < gap {
				time.Sleep(gap - elapsed)
			}
		}

		target := fmt.Sprintf("%schannels/%s/messages/%s", DiscordApi, curCh, curMsg)
		rec := ProbeRecord{SentAt: time.Now()}
		if n := len(records); n > 0 {
			rec.Gap = rec.SentAt.Sub(records[n-1].SentAt)
		}
		lastSent = rec.SentAt

		resp, err := c.http.Do("DELETE", target)
		rec.DoneAt = time.Now()
		rec.RTT = rec.DoneAt.Sub(rec.SentAt)
		if err != nil {
			return records, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		rec.Status = resp.StatusCode

		if resp.StatusCode == 429 {
			var dr DeleteResponse
			if err := json.Unmarshal(body, &dr); err == nil {
				rec.RetryAfter = dr.RetryAfter
			}
			records = append(records, rec)
			// Honour retry_after, then retry the SAME message (haveMsg stays true).
			wait := time.Duration(rec.RetryAfter*float64(time.Second)) + 100*time.Millisecond
			if wait <= 0 {
				wait = gap
			}
			time.Sleep(wait)
			continue
		}

		records = append(records, rec)
		haveMsg = false // consumed this message; advance on next iteration
		if resp.StatusCode == 204 {
			deletes++
		}
	}
	return records, nil
}
