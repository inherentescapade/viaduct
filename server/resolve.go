package server

import (
	"fmt"
	"github.com/inherentescapade/viaduct/dates"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"strings"
	"time"
)

// resolver turns the abstract specs that arrive over the wire into concrete
// engine jobs, using a Discord client authenticated as a particular user.
type resolver struct {
	client *discord.Client
	user   *discord.User
}

// newResolver validates the token and captures the acting user. It reuses the
// engine's Discord client so resolution and deletion share one set of rate
// limiters.
func newResolver(eng *engine.Engine) (*resolver, error) {
	user, err := eng.Client.ValidateToken()
	if err != nil {
		return nil, fmt.Errorf("the stored Discord token was rejected: %w", err)
	}
	return &resolver{client: eng.Client, user: user}, nil
}

// resolveGuild finds a guild by name or ID, with "@me" mapping to DMs.
func (r *resolver) resolveGuild(nameOrID string) (*discord.Guild, error) {
	if nameOrID == "@me" || nameOrID == "" {
		return &discord.Guild{Id: "@me", Name: "Direct Messages"}, nil
	}
	guilds, err := r.client.GetGuilds()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch servers: %w", err)
	}
	if g := r.client.ResolveGuild(nameOrID, guilds); g != nil {
		return g, nil
	}
	return nil, fmt.Errorf("server %q not found", nameOrID)
}

// selectChannels returns the channels of a guild that match the given selectors
// (a channel ID or a case-insensitive substring of its name). With no
// selectors, every text channel is returned.
func (r *resolver) selectChannels(guild *discord.Guild, selectors []string) ([]discord.Channel, error) {
	all, err := r.client.GetChannels(guild.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channels: %w", err)
	}
	var text []discord.Channel
	for _, ch := range all {
		if guild.Id == "@me" || ch.Type == 0 || ch.Type == 5 {
			text = append(text, ch)
		}
	}
	if len(selectors) == 0 {
		return text, nil
	}
	var out []discord.Channel
	for _, ch := range text {
		if channelMatchesAny(ch, selectors) {
			out = append(out, ch)
		}
	}
	return out, nil
}

// dmChannelsWith returns the DM/group-DM channels whose recipients include the
// given user (matched by ID or a case-insensitive username/global-name match).
func (r *resolver) dmChannelsWith(user string) ([]discord.Channel, error) {
	all, err := r.client.GetChannels("@me")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DM channels: %w", err)
	}
	needle := strings.ToLower(strings.TrimSpace(user))
	var out []discord.Channel
	for _, ch := range all {
		for _, rcp := range ch.Recipients {
			if rcp.Id == user ||
				strings.ToLower(rcp.Username) == needle ||
				strings.ToLower(rcp.GlobalName) == needle {
				out = append(out, ch)
				break
			}
		}
	}
	return out, nil
}

// buildDeleteJob assembles an engine.DeleteJob from a spec.
func (r *resolver) buildDeleteJob(kind JobKind, spec DeleteSpec) (engine.DeleteJob, string, error) {
	var job engine.DeleteJob
	var label string

	switch kind {
	case KindDeleteDM:
		if strings.TrimSpace(spec.User) == "" {
			return job, "", fmt.Errorf("delete_dm requires a user")
		}
		chans, err := r.dmChannelsWith(spec.User)
		if err != nil {
			return job, "", err
		}
		if len(chans) == 0 {
			return job, "", fmt.Errorf("no DMs found with %q", spec.User)
		}
		job = engine.DeleteJob{GuildID: "@me", GuildName: "Direct Messages", Channels: chans, UserID: r.user.Id}
		label = fmt.Sprintf("DMs with %s", spec.User)

	case KindDeleteGuild:
		guild, err := r.resolveGuild(spec.Guild)
		if err != nil {
			return job, "", err
		}
		chans, err := r.selectChannels(guild, spec.Channels)
		if err != nil {
			return job, "", err
		}
		if len(chans) == 0 {
			return job, "", fmt.Errorf("no matching channels in %q", guild.Name)
		}
		// Drop any channels the caller asked to exclude ("delete all other
		// direct messages/groups"). This is the deny-list counterpart to
		// Channels' allow-list, and it forces an explicit channel list even for
		// a whole-guild delete — the engine can't express "guild-wide except X".
		excluded := len(spec.Exclude) > 0
		if excluded {
			kept := excludeChannels(chans, spec.Exclude)
			if len(kept) == 0 {
				return job, "", fmt.Errorf("the exclusion left no channels to delete in %q", guild.Name)
			}
			chans = kept
		}
		// A whole-guild delete needs no channel list (the engine searches
		// guild-wide); a filtered or excluded one passes the selected channels.
		if len(spec.Channels) == 0 && !excluded && guild.Id != "@me" {
			job = engine.DeleteJob{GuildID: guild.Id, GuildName: guild.Name, UserID: r.user.Id}
		} else {
			job = engine.DeleteJob{GuildID: guild.Id, GuildName: guild.Name, Channels: chans, UserID: r.user.Id}
		}
		// For DMs, name the conversations rather than the bland "Direct Messages".
		if guild.Id == "@me" {
			label = describeDMTargets(chans, len(spec.Channels) == 0 && !excluded)
		} else {
			label = guild.Name
			if len(spec.Channels) > 0 || excluded {
				label = fmt.Sprintf("%s (%d channel%s)", guild.Name, len(chans), plural(len(chans)))
			}
		}

	default:
		return job, "", fmt.Errorf("unknown job kind %q", kind)
	}

	if err := applyDateFilters(&job, spec.Before, spec.After); err != nil {
		return job, "", err
	}
	job.MaxID = spec.MaxID
	job.MinID = spec.MinID
	return job, label, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// describeDMTargets builds a human label for a DM deletion: "All direct
// messages", "DMs with Bob", or "DMs: Bob, Alice +2 more".
func describeDMTargets(chans []discord.Channel, all bool) string {
	if all {
		return "All direct messages"
	}
	var names []string
	for _, ch := range chans {
		n := ch.Name
		if n == "" {
			for _, r := range ch.Recipients {
				rn := r.GlobalName
				if rn == "" {
					rn = r.Username
				}
				if rn != "" {
					n = rn
					break
				}
			}
		}
		if n != "" {
			names = append(names, n)
		}
	}
	switch {
	case len(names) == 0:
		return "Direct messages"
	case len(names) == 1:
		return "DMs with " + names[0]
	case len(names) <= 3:
		return "DMs: " + strings.Join(names, ", ")
	default:
		return fmt.Sprintf("DMs: %s +%d more", strings.Join(names[:3], ", "), len(names)-3)
	}
}

// applyDateFilters parses and applies before/after date expressions.
func applyDateFilters(job *engine.DeleteJob, before, after string) error {
	if before != "" {
		t, err := dates.Parse(before)
		if err != nil {
			return fmt.Errorf("invalid before value: %w", err)
		}
		job.Before = t
	}
	if after != "" {
		t, err := dates.Parse(after)
		if err != nil {
			return fmt.Errorf("invalid after value: %w", err)
		}
		job.After = t
	}
	return nil
}

// buildMonitorJob turns a monitor policy into a DeleteJob whose Before cutoff is
// "now minus the retention window" — i.e. everything older than MaxAge.
func (r *resolver) buildMonitorJob(p MonitorPolicy) (engine.DeleteJob, error) {
	p.normalizeAge()
	guild, err := r.resolveGuild(p.Scope)
	if err != nil {
		return engine.DeleteJob{}, err
	}
	all, err := r.selectChannels(guild, nil)
	if err != nil {
		return engine.DeleteJob{}, err
	}
	if len(all) == 0 {
		scope := p.Scope
		if scope == "@me" || scope == "" {
			return engine.DeleteJob{}, fmt.Errorf("no DM conversations found for this account")
		}
		return engine.DeleteJob{}, fmt.Errorf("no channels found in %q", scope)
	}

	chans := applyMonitorMode(all, p)
	if len(chans) == 0 {
		if p.Mode == ModeInclude {
			return engine.DeleteJob{}, fmt.Errorf("the include filter %v matched no channels in %s", p.Channels, monitorScopeLabel(p.Scope))
		}
		return engine.DeleteJob{}, fmt.Errorf("the exclude filter left no channels in %s", monitorScopeLabel(p.Scope))
	}

	job := engine.DeleteJob{
		GuildID:   guild.Id,
		GuildName: guild.Name,
		Channels:  chans,
		UserID:    r.user.Id,
		Before:    time.Now().Add(-p.MaxAge()),
	}
	// A whole-guild include-everything monitor can search guild-wide.
	if guild.Id != "@me" && p.Mode == ModeExclude && len(p.Channels) == 0 {
		job.Channels = nil
	}
	return job, nil
}

// monitorScopeLabel renders a scope for error messages.
func monitorScopeLabel(scope string) string {
	if scope == "@me" || scope == "" {
		return "your DMs"
	}
	return scope
}

// applyMonitorMode filters channels by the policy's include/exclude rule.
func applyMonitorMode(all []discord.Channel, p MonitorPolicy) []discord.Channel {
	var out []discord.Channel
	for _, ch := range all {
		matched := channelMatchesAny(ch, p.Channels)
		switch p.Mode {
		case ModeInclude:
			if matched {
				out = append(out, ch)
			}
		default: // exclude
			if !matched {
				out = append(out, ch)
			}
		}
	}
	return out
}

// excludeChannels returns the channels that match NONE of the given selectors —
// the deny-list filter behind "delete all other direct messages/groups".
func excludeChannels(all []discord.Channel, selectors []string) []discord.Channel {
	var out []discord.Channel
	for _, ch := range all {
		if !channelMatchesAny(ch, selectors) {
			out = append(out, ch)
		}
	}
	return out
}

// channelMatchesAny reports whether a channel matches any selector: its exact
// ID, a case-insensitive substring of its name, or (for DMs) a recipient's name.
func channelMatchesAny(ch discord.Channel, selectors []string) bool {
	for _, raw := range selectors {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if s == ch.Id {
			return true
		}
		low := strings.ToLower(s)
		if ch.Name != "" && strings.Contains(strings.ToLower(ch.Name), low) {
			return true
		}
		for _, rcp := range ch.Recipients {
			if strings.Contains(strings.ToLower(rcp.Username), low) ||
				strings.Contains(strings.ToLower(rcp.GlobalName), low) {
				return true
			}
		}
	}
	return false
}
