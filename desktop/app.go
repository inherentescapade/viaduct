package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/dates"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"github.com/inherentescapade/viaduct/export"
	"github.com/inherentescapade/viaduct/server"
	"github.com/inherentescapade/viaduct/token"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound bridge between the React frontend and viaduct's Go
// engine. Every exported method is callable from JS as window.go.main.App.<Name>.
//
// Long-running deletions run in a goroutine and stream their state back through
// the runtime event bus (see events.go); the Start* methods return immediately
// so the UI thread never blocks.
type App struct {
	// appCtx is the Wails runtime context captured in startup(). It is the
	// context EventsEmit must use, never the per-run cancelable context.
	appCtx context.Context

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc

	config  *cfg.Config
	client  *discord.Client
	user    *discord.User
	token   string
	botMode bool

	// chanCache maps channel ID -> full discord.Channel from the last
	// ListChannels call, so Start/Preview can rebuild []discord.Channel (needed
	// for DM mode, where the engine searches each channel individually).
	chanCache map[string]discord.Channel

	// loaded data-package export, kept Go-side so we never ship message bodies
	// to the frontend.
	export *export.Export

	lastLog string

	// localMon runs in-process monitor policies while the app is open; the
	// "standalone on a box" path that needs no server. See local_monitor.go.
	localMon *server.MonitorManager
}

func NewApp() *App {
	config := cfg.DefaultConfig()
	_ = config.Load() // missing config is fine; user sets it up in the UI
	app := &App{
		config:    config,
		token:     config.Token,
		botMode:   config.BotMode,
		chanCache: map[string]discord.Channel{},
	}
	app.initLocalMonitors()
	return app
}

func (a *App) startup(ctx context.Context) {
	a.appCtx = ctx
	// Run local monitors for the lifetime of the process. context.Background is
	// intentional: the scheduler should keep running until the app exits.
	go a.localMon.Run(context.Background())
}

func (a *App) chanLabelFor(channelID string) string {
	a.mu.Lock()
	ch, ok := a.chanCache[channelID]
	a.mu.Unlock()
	if !ok {
		return ""
	}
	return channelLabel(ch)
}

// ---- Authentication ----

// ValidateToken verifies a token against Discord and, on success, persists it
// and becomes the active session. Returns the logged-in user.
func (a *App) ValidateToken(token string, botMode bool) (*UserDTO, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("paste a Discord token to continue")
	}

	client := discord.NewClient(token, botMode, http.DefaultClient)
	user, err := client.ValidateToken()
	if err != nil {
		return nil, fmt.Errorf("that token didn't work: %w", err)
	}

	a.mu.Lock()
	a.client = client
	a.user = user
	a.token = token
	a.botMode = botMode
	a.mu.Unlock()

	a.config.Token = token
	a.config.BotMode = botMode
	_ = a.config.Save()

	return toUserDTO(user), nil
}

// AutoLogin validates a previously saved token, if any. Returns null when no
// token is stored so the frontend knows to show the token screen. The raw token
// is never returned to JS.
func (a *App) AutoLogin() (*UserDTO, error) {
	a.mu.Lock()
	token := a.token
	bot := a.botMode
	a.mu.Unlock()
	if token == "" {
		return nil, nil
	}
	return a.ValidateToken(token, bot)
}

// HasAutoDetect reports whether automatic token extraction is supported on
// the current platform. Used by the UI to show/hide the auto-detect button.
func (a *App) HasAutoDetect() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "windows"
}

// AutoDetectToken scans local Discord client storage for a saved session
// token, validates it against Discord, and saves it on success. The UI must
// show a consent notice before calling this; the act of clicking the button
// constitutes consent.
//
// LevelDB files accumulate stale tokens, so we try every candidate in order
// and return the first one that Discord accepts.
func (a *App) AutoDetectToken() (*UserDTO, error) {
	candidates, err := token.GetTokens()
	if err != nil {
		return nil, fmt.Errorf("could not find token: %w", err)
	}
	var lastErr error
	for _, t := range candidates {
		user, err := a.ValidateToken(t, false)
		if err == nil {
			return user, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no valid token found (%d candidate(s) tried): %w", len(candidates), lastErr)
}

// GetSavedToken reports whether a token is stored, without revealing it.
func (a *App) GetSavedToken() TokenStateDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return TokenStateDTO{HasToken: a.token != "", BotMode: a.botMode}
}

// ---- Discovery ----

// ListGuilds returns the user's servers, with a synthetic "@me" entry for DMs
// prepended. Falls back to the on-disk cache if the live fetch fails.
func (a *App) ListGuilds() ([]GuildDTO, error) {
	client := a.activeClient()
	if client == nil {
		return nil, errNotAuthed
	}

	guilds, err := client.GetGuilds()
	if err != nil {
		cached, cerr := cfg.LoadGuildCache()
		if cerr != nil {
			return nil, err
		}
		guilds = cached
	} else {
		_ = cfg.SaveGuildCache(guilds)
	}

	out := []GuildDTO{{ID: "@me", Name: "Direct Messages", IsDM: true}}
	for _, g := range guilds {
		out = append(out, GuildDTO{
			ID:      g.Id,
			Name:    g.Name,
			IconURL: guildIconURL(g.Id, g.Icon),
			Owner:   g.Owner,
		})
	}
	return out, nil
}

// snowflake parses a Discord snowflake ID into a comparable uint64. Empty or
// malformed IDs (e.g. a DM that has never had a message) sort to 0 / the end.
func snowflake(id string) uint64 {
	n, _ := strconv.ParseUint(id, 10, 64)
	return n
}

// ListChannels returns the deletable channels for a guild (text + announcement),
// or every DM channel when guildID is "@me". The full channel objects are cached
// for later job construction.
func (a *App) ListChannels(guildID string) ([]ChannelDTO, error) {
	client := a.activeClient()
	if client == nil {
		return nil, errNotAuthed
	}

	channels, err := client.GetChannels(guildID)
	if err != nil {
		return nil, err
	}

	isDM := guildID == "@me"
	// DMs come back from Discord in an arbitrary order; sort them so the most
	// recently active conversations appear first. Discord snowflake IDs are
	// monotonic in time, so a higher last_message_id means more recent.
	if isDM {
		sort.SliceStable(channels, func(i, j int) bool {
			return snowflake(channels[i].LastMessageId) > snowflake(channels[j].LastMessageId)
		})
	}
	out := []ChannelDTO{}
	a.mu.Lock()
	for _, ch := range channels {
		if !isDM && ch.Type != 0 && ch.Type != 5 {
			continue
		}
		a.chanCache[ch.Id] = ch
		out = append(out, ChannelDTO{
			ID:        ch.Id,
			Name:      channelLabel(ch),
			Type:      ch.Type,
			Nsfw:      ch.Nsfw,
			AvatarURL: channelAvatarURL(ch),
		})
	}
	a.mu.Unlock()
	return out, nil
}

// GuildMessageCount returns how many of the signed-in user's messages exist in a
// guild (search TotalResults with an author filter). Used to annotate the server
// list. Cheap: one search request.
func (a *App) GuildMessageCount(guildID string) (int, error) {
	client := a.activeClient()
	if client == nil {
		return 0, errNotAuthed
	}
	a.mu.Lock()
	user := a.user
	a.mu.Unlock()
	if user == nil {
		return 0, errNotAuthed
	}
	return client.CountMessages(context.Background(), guildID, nil, &user.Id, nil, nil)
}

// DMMessageCount returns how many of the user's messages exist in a single DM or
// group-DM channel. Used to annotate the conversation list.
func (a *App) DMMessageCount(channelID string) (int, error) {
	client := a.activeClient()
	if client == nil {
		return 0, errNotAuthed
	}
	a.mu.Lock()
	user := a.user
	a.mu.Unlock()
	if user == nil {
		return 0, errNotAuthed
	}
	resp, err := client.GetDMMessages(context.Background(), channelID, &user.Id, nil, nil, 0)
	if err != nil {
		return 0, err
	}
	return resp.TotalResults, nil
}

// SampleMessages returns a handful of the user's most recent messages that the
// job would delete, for a visual preview. Non-destructive (search only).
func (a *App) SampleMessages(req DeleteRequest) ([]MessageDTO, error) {
	job, err := a.buildDeleteJob(req)
	if err != nil {
		return nil, err
	}
	client := a.activeClient()
	if client == nil {
		return nil, errNotAuthed
	}

	const limit = 25
	var resp *discord.SearchResponse
	if job.IsDM() {
		if len(job.Channels) == 0 {
			return nil, nil
		}
		resp, err = client.GetDMMessages(context.Background(), job.Channels[0].Id, &job.UserID, nil, nil, 0)
	} else {
		resp, err = client.GetMessages(context.Background(), job.GuildID, nil, &job.UserID, nil, nil, 0, true)
	}
	if err != nil {
		return nil, err
	}

	out := make([]MessageDTO, 0, limit)
	for _, cluster := range resp.Messages {
		if len(cluster) == 0 || cluster[0].Author.Id != job.UserID {
			continue
		}
		out = append(out, toMessageDTO(cluster[0], a.chanLabelFor(cluster[0].ChannelId)))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ---- Live deletion (search API) ----

// Preview counts the messages a live job would delete, without deleting.
func (a *App) Preview(req DeleteRequest) (int, error) {
	job, err := a.buildDeleteJob(req)
	if err != nil {
		return 0, err
	}
	return a.newEngine().Preview(context.Background(), job)
}

// StartDelete launches a live deletion in the background. Progress arrives via
// run:progress events; completion via run:finished. Returns immediately.
func (a *App) StartDelete(req DeleteRequest) error {
	job, err := a.buildDeleteJob(req)
	if err != nil {
		return err
	}
	if err := a.beginRun(); err != nil {
		return err
	}

	ctx := a.newRunContext()
	eng := a.newEngine()
	eng.OnProgress = a.progressEmitter()
	eng.OnNotice = a.noticeEmitter()

	// Stream each deleted message to the UI's "rainfall" and tally a cumulative
	// count (Progress.Deleted resets per mop-up pass, so we count here instead).
	var deleted int64
	eng.OnMessage = func(m discord.Message) {
		atomic.AddInt64(&deleted, 1)
		wruntime.EventsEmit(a.appCtx, EvMessage, toMessageDTO(m, a.chanLabelFor(m.ChannelId)))
	}

	// Immediate heartbeat so the UI shows activity during the engine's initial
	// (event-less) deep-historical-index waits.
	wruntime.EventsEmit(a.appCtx, EvProgress, ProgressDTO{GuildName: job.GuildName, Starting: true})

	go func() {
		defer a.endRun()
		remaining, err := eng.ExecuteVerified(ctx, job, 3, func() {
			wruntime.EventsEmit(a.appCtx, EvVerifying, nil)
		})
		a.finishDelete(eng, ctx, err, remaining, int(atomic.LoadInt64(&deleted)))
	}()
	return nil
}

// finishDelete emits the terminal events for a verified live deletion, carrying
// the verification result and the cumulative deleted count.
func (a *App) finishDelete(eng *engine.Engine, ctx context.Context, err error, remaining, deleted int) {
	logPath := eng.LogPath()
	if logPath != "" {
		a.mu.Lock()
		a.lastLog = logPath
		a.mu.Unlock()
	}
	if err != nil && ctx.Err() == nil {
		wruntime.EventsEmit(a.appCtx, EvError, errPayload(err))
	}
	wruntime.EventsEmit(a.appCtx, EvFinished, map[string]interface{}{
		"logPath":   logPath,
		"cancelled": ctx.Err() != nil,
		"verified":  err == nil && ctx.Err() == nil && remaining == 0,
		"remaining": remaining,
		"deleted":   deleted,
	})
}

// Enumerate runs a non-destructive dry-run, emitting each matching message via
// run:message and a final run:enumDone with the count.
func (a *App) Enumerate(req DeleteRequest) error {
	job, err := a.buildDeleteJob(req)
	if err != nil {
		return err
	}
	if err := a.beginRun(); err != nil {
		return err
	}

	ctx := a.newRunContext()
	eng := a.newEngine()
	eng.OnMessage = func(m discord.Message) {
		wruntime.EventsEmit(a.appCtx, EvMessage, toMessageDTO(m, a.chanLabelFor(m.ChannelId)))
	}

	go func() {
		defer a.endRun()
		count, err := eng.Enumerate(ctx, job)
		if err != nil && ctx.Err() == nil {
			wruntime.EventsEmit(a.appCtx, EvError, errPayload(err))
		}
		wruntime.EventsEmit(a.appCtx, EvEnumDone, map[string]int{"count": count})
		wruntime.EventsEmit(a.appCtx, EvFinished, finishedPayload("", ctx))
	}()
	return nil
}

// ---- Import (data package) ----

// PickExportFolder opens a native folder picker for the unzipped data package.
func (a *App) PickExportFolder() (string, error) {
	return wruntime.OpenDirectoryDialog(a.appCtx, wruntime.OpenDialogOptions{
		Title: "Select your Discord data package",
	})
}

// LoadExport parses a data package at path and returns lightweight channel
// descriptors (counts only; message bodies stay Go-side).
func (a *App) LoadExport(path string) (*ExportSummaryDTO, error) {
	ex, err := export.Load(path)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.export = ex
	a.mu.Unlock()

	sum := &ExportSummaryDTO{Root: ex.Root, TotalMessages: ex.MessageCount()}
	for _, ch := range ex.Channels {
		sum.Channels = append(sum.Channels, ChannelExportDTO{
			ID:           ch.ID,
			Name:         ch.Name,
			Type:         ch.Type,
			MessageCount: len(ch.Messages),
			IsDM:         ch.IsDM(),
			IsForgotten:  ch.IsForgotten(),
		})
	}
	return sum, nil
}

// CountImport reports how many channels and messages the import filters select.
func (a *App) CountImport(req ImportRequest) (CountDTO, error) {
	job, nch, err := a.buildImportJob(req)
	if err != nil {
		return CountDTO{}, err
	}
	return CountDTO{Messages: a.newEngine().CountImport(job), Channels: nch}, nil
}

// StartImport launches a data-package deletion in the background. Progress
// arrives via run:progress; the failure breakdown via import:done; completion
// via run:finished.
func (a *App) StartImport(req ImportRequest) error {
	job, nch, err := a.buildImportJob(req)
	if err != nil {
		return err
	}
	if nch == 0 {
		return fmt.Errorf("no channels selected; adjust your filters")
	}
	a.mu.Lock()
	authed := a.token != ""
	a.mu.Unlock()
	if !authed {
		return errNotAuthed
	}
	if err := a.beginRun(); err != nil {
		return err
	}

	ctx := a.newRunContext()
	eng := a.newEngine()
	eng.OnProgress = a.progressEmitter()
	eng.OnNotice = a.noticeEmitter()

	go func() {
		defer a.endRun()
		err := eng.ExecuteImport(ctx, job)
		wruntime.EventsEmit(a.appCtx, EvImportDone, toFailReasonDTOs(eng.FailureSummary()))
		a.finish(eng, ctx, err)
	}()
	return nil
}

// ---- Control & misc ----

// Cancel requests the current run stop. Because the engine checks cancellation
// at loop boundaries (and some backoff sleeps aren't context-aware), the stop
// can take a few seconds; the UI should show "Stopping…" until run:finished.
func (a *App) Cancel() {
	a.mu.Lock()
	c := a.cancel
	a.mu.Unlock()
	if c != nil {
		c()
	}
}

// LogPath returns the most recent run's log file, or the log directory if no
// run has happened yet.
func (a *App) LogPath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastLog != "" {
		return a.lastLog
	}
	return cfg.LogDir()
}

// OpenLogFolder reveals the log directory in the OS file manager.
func (a *App) OpenLogFolder() error { return openInFileManager(cfg.LogDir()) }

// DownloadLog copies a single run's deletion log out of the log directory to a
// location the user picks, so a record can be archived or shared. file must be a
// bare log filename (delete_*.ndjson); any path component is rejected so the
// read stays confined to the log directory. Returns the saved path, or "" if the
// user cancels the dialog.
func (a *App) DownloadLog(file string) (string, error) {
	name, err := logFileName(file)
	if err != nil {
		return "", err
	}
	src := filepath.Join(cfg.LogDir(), name)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("that log no longer exists")
	}
	dest, err := wruntime.SaveFileDialog(a.appCtx, wruntime.SaveDialogOptions{
		Title:           "Save deletion log",
		DefaultFilename: name,
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // user cancelled
	}
	if err := copyFile(src, dest); err != nil {
		return "", fmt.Errorf("could not save the log: %w", err)
	}
	return dest, nil
}

// DeleteLog permanently removes a single run's deletion log from the log
// directory. file must be a bare log filename (delete_*.ndjson); any path
// component is rejected so the delete stays confined to the log directory.
// Deleting a log erases the local record only — it does not affect Discord.
func (a *App) DeleteLog(file string) error {
	name, err := logFileName(file)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(cfg.LogDir(), name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not delete the log: %w", err)
	}
	return nil
}

// logFileName validates that file is a bare deletion-log filename (no path
// component) and returns it, so log operations can't be pointed outside the log
// directory via a crafted path.
func logFileName(file string) (string, error) {
	name := strings.TrimSpace(file)
	if name != filepath.Base(name) || !strings.HasPrefix(name, "delete_") || !strings.HasSuffix(name, ".ndjson") {
		return "", fmt.Errorf("not a deletion log: %q", file)
	}
	return name, nil
}

// OpenPath reveals an arbitrary file or folder in the OS file manager.
func (a *App) OpenPath(p string) error { return openInFileManager(p) }

// ConfigInfo returns the on-disk locations and log size for the Settings panel.
func (a *App) ConfigInfo() ConfigInfoDTO {
	logDir := cfg.LogDir()
	return ConfigInfoDTO{
		ConfigDir: cfg.ConfigDir(),
		LogDir:    logDir,
		LogBytes:  dirSize(logDir),
	}
}

// ClearLogs deletes all deletion log files from the log directory.
func (a *App) ClearLogs() error {
	dir := cfg.LogDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "delete_") && strings.HasSuffix(n, ".ndjson") {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
	return nil
}

// ClearSession removes the saved token and signs the user out, without
// touching log files or other data.
func (a *App) ClearSession() error {
	a.mu.Lock()
	a.token = ""
	a.botMode = false
	a.client = nil
	a.user = nil
	a.config.Token = ""
	a.config.BotMode = false
	cfgCopy := a.config
	a.mu.Unlock()
	return cfgCopy.Save()
}

// dirSize returns the total size in bytes of all files under a directory.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// GetPrefs returns the user's saved preferences.
func (a *App) GetPrefs() PrefsDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return PrefsDTO{SkipConfirm: a.config.Prefs.SkipConfirm, PreScan: a.config.Prefs.PreScan}
}

// SetSkipConfirm persists whether the final confirmation step is skipped.
func (a *App) SetSkipConfirm(v bool) error {
	a.mu.Lock()
	a.config.Prefs.SkipConfirm = v
	cfgCopy := a.config
	a.mu.Unlock()
	return cfgCopy.Save()
}

// SetPreScan persists whether deletions enumerate the full message list first
// (exact total/ETA) before deleting.
func (a *App) SetPreScan(v bool) error {
	a.mu.Lock()
	a.config.Prefs.PreScan = v
	cfgCopy := a.config
	a.mu.Unlock()
	return cfgCopy.Save()
}

// ---- internal helpers ----

var errNotAuthed = fmt.Errorf("not signed in; validate a token first")

func (a *App) activeClient() *discord.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client
}

func (a *App) newEngine() *engine.Engine {
	a.mu.Lock()
	token, bot := a.token, a.botMode
	a.mu.Unlock()
	return engine.New(token, bot)
}

// beginRun claims the single run slot. Returns an error if one is already
// active, keeping the UI honest about concurrency.
func (a *App) beginRun() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return fmt.Errorf("a run is already in progress")
	}
	a.running = true
	return nil
}

func (a *App) endRun() {
	a.mu.Lock()
	a.running = false
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.mu.Unlock()
}

func (a *App) newRunContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	return ctx
}

// finish records the log path and emits the terminal events for a run.
func (a *App) finish(eng *engine.Engine, ctx context.Context, err error) {
	logPath := eng.LogPath()
	if logPath != "" {
		a.mu.Lock()
		a.lastLog = logPath
		a.mu.Unlock()
	}
	if err != nil && ctx.Err() == nil {
		wruntime.EventsEmit(a.appCtx, EvError, errPayload(err))
	}
	wruntime.EventsEmit(a.appCtx, EvFinished, finishedPayload(logPath, ctx))
}

// progressEmitter returns a throttled OnProgress callback. The engine emits per
// deleted message; we coalesce to ~10 updates/sec but always pass the terminal
// Done event through so the UI lands on a correct final state.
func (a *App) progressEmitter() func(engine.Progress) {
	var mu sync.Mutex
	var last time.Time
	return func(p engine.Progress) {
		if p.Done {
			wruntime.EventsEmit(a.appCtx, EvProgress, toProgressDTO(p))
			return
		}
		mu.Lock()
		now := time.Now()
		if now.Sub(last) < 100*time.Millisecond {
			mu.Unlock()
			return
		}
		last = now
		mu.Unlock()
		wruntime.EventsEmit(a.appCtx, EvProgress, toProgressDTO(p))
	}
}

// noticeEmitter returns an OnNotice handler that forwards the engine's
// human-readable pacing/status lines to the frontend as run:notice events.
func (a *App) noticeEmitter() func(string) {
	return func(msg string) {
		wruntime.EventsEmit(a.appCtx, EvNotice, map[string]string{"message": msg})
	}
}

func (a *App) buildDeleteJob(req DeleteRequest) (engine.DeleteJob, error) {
	a.mu.Lock()
	user := a.user
	a.mu.Unlock()
	if user == nil {
		return engine.DeleteJob{}, errNotAuthed
	}

	a.mu.Lock()
	preScan := a.config.Prefs.PreScan
	a.mu.Unlock()

	job := engine.DeleteJob{
		GuildID:   req.GuildID,
		GuildName: req.GuildName,
		UserID:    user.Id,
		MaxID:     strings.TrimSpace(req.MaxID),
		MinID:     strings.TrimSpace(req.MinID),
		PreScan:   preScan,
	}

	if s := strings.TrimSpace(req.Before); s != "" {
		t, err := dates.Parse(s)
		if err != nil {
			return job, fmt.Errorf("invalid 'before' date: %w", err)
		}
		job.Before = t
	}
	if s := strings.TrimSpace(req.After); s != "" {
		t, err := dates.Parse(s)
		if err != nil {
			return job, fmt.Errorf("invalid 'after' date: %w", err)
		}
		job.After = t
	}

	a.mu.Lock()
	for _, id := range req.ChannelIDs {
		if ch, ok := a.chanCache[id]; ok {
			job.Channels = append(job.Channels, ch)
		}
	}
	a.mu.Unlock()

	if job.IsDM() && len(job.Channels) == 0 {
		return job, fmt.Errorf("select at least one DM conversation")
	}
	return job, nil
}

func (a *App) buildImportJob(req ImportRequest) (engine.ImportJob, int, error) {
	a.mu.Lock()
	ex := a.export
	a.mu.Unlock()
	if ex == nil {
		return engine.ImportJob{}, 0, fmt.Errorf("load a data package first")
	}

	channels := selectExportChannels(ex.Channels, req)
	job := engine.ImportJob{Channels: channels}

	if s := strings.TrimSpace(req.Before); s != "" {
		t, err := dates.Parse(s)
		if err != nil {
			return job, 0, fmt.Errorf("invalid 'before' date: %w", err)
		}
		job.Before = t
	}
	if s := strings.TrimSpace(req.After); s != "" {
		t, err := dates.Parse(s)
		if err != nil {
			return job, 0, fmt.Errorf("invalid 'after' date: %w", err)
		}
		job.After = t
	}
	return job, len(channels), nil
}

// selectExportChannels applies the channel filters in the same order as the CLI:
// drop-empty, forgotten, no-dms, include, then exclude.
func selectExportChannels(all []export.Channel, req ImportRequest) []export.Channel {
	var out []export.Channel
	for _, ch := range all {
		if len(ch.Messages) == 0 {
			continue
		}
		if req.Forgotten && !ch.IsForgotten() {
			continue
		}
		if req.NoDMs && ch.IsDM() {
			continue
		}
		if len(req.Include) > 0 && !matchesAnySelector(ch, req.Include) {
			continue
		}
		if matchesAnySelector(ch, req.Exclude) {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// matchesAnySelector reports whether ch matches any token: an exact channel ID,
// a case-insensitive name substring, or its export type.
func matchesAnySelector(ch export.Channel, tokens []string) bool {
	for _, raw := range tokens {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if t == ch.ID || strings.EqualFold(t, ch.Type) {
			return true
		}
		lower := strings.ToLower(t)
		if strings.Contains(strings.ToLower(ch.Name), lower) {
			return true
		}
		if ch.IndexName != "" && strings.Contains(strings.ToLower(ch.IndexName), lower) {
			return true
		}
	}
	return false
}

func errPayload(err error) map[string]string {
	return map[string]string{"message": err.Error()}
}

func finishedPayload(logPath string, ctx context.Context) map[string]interface{} {
	return map[string]interface{}{
		"logPath":   logPath,
		"cancelled": ctx.Err() != nil,
	}
}

// copyFile streams src to dest (0600), creating or truncating dest. It writes
// through io.Copy so even a multi-gigabyte log never lives wholly in memory.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// openInFileManager reveals a path in the host OS file manager.
func openInFileManager(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
