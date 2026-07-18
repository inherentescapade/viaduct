package cfg

import (
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/discord"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	Token   string `json:"token"`
	BotMode bool   `json:"bot_mode"`
	Prefs   Prefs  `json:"preferences"`
	// Server holds settings used when this machine runs `viaduct serve` (the
	// self-hosting job owner, typically on a VPS).
	Server ServerConfig `json:"server"`
	// Remotes are servers this machine (as a client) knows how to reach,
	// keyed by a short name, with the pinned TLS fingerprint from first use.
	Remotes []RemoteServer `json:"remotes,omitempty"`
}

// DefaultPort is the port `viaduct serve` listens on when none is configured.
// It's a non-standard high port to avoid colliding with common services.
const DefaultPort = 21776

// ServerConfig configures the self-hosting server. A client detects its
// Discord token locally and pushes it (encrypted end-to-end inside an ECIES
// envelope); the server persists it keyed by a hash of the token itself, so
// monitors and jobs keep running while every client that pushed it is
// offline. Any client key that pushes the identical token shares that one
// account's jobs, monitors, and log directory — two machines presenting the
// same token are guaranteed to be the same person, so they're treated as one
// session, not two segregated ones. A client key that never pushes a token
// gets its own private bucket instead.
type ServerConfig struct {
	// Listen is the bind address (host without port). Empty means all interfaces.
	Listen string `json:"listen,omitempty"`
	// Port is the TCP port to listen on. Zero means DefaultPort.
	Port int `json:"port,omitempty"`
	// AuthorizedKeys are the client public keys (viaduct1:... form) allowed to
	// dispatch jobs.
	AuthorizedKeys []string `json:"authorized_keys,omitempty"`
	// Accounts holds each account's pushed Discord credentials, keyed by an
	// account key (a hash of the token; see server.AccountKey).
	Accounts map[string]ClientCredential `json:"accounts,omitempty"`
	// ClientKeys maps each paired client's public key (viaduct1:... form) to
	// the account key it last pushed credentials for, so a request that omits
	// credentials (ping, list_jobs, ...) still resolves to the right account
	// after a restart. A client key with no entry here has never pushed a
	// token and gets its own private bucket, keyed by its own public key.
	ClientKeys map[string]string `json:"client_keys,omitempty"`
}

// ClientCredential is the Discord auth one client pushed to the server.
type ClientCredential struct {
	Token   string `json:"token"`
	BotMode bool   `json:"bot_mode"`
}

// Addr returns the host:port the server should bind to.
func (s ServerConfig) Addr() string {
	port := s.Port
	if port == 0 {
		port = DefaultPort
	}
	return fmt.Sprintf("%s:%d", s.Listen, port)
}

// RemoteServer is a known server a client can dispatch to. PublicKey is the
// server's X25519 identity (viaduct1:... form), used to encrypt requests to it
// and to verify its replies.
type RemoteServer struct {
	Name      string `json:"name"`
	Address   string `json:"address"`              // host:port
	PublicKey string `json:"public_key,omitempty"` // server's viaduct1:... key
}

// IdentityPath is where the client's X25519 identity key lives.
func IdentityPath() string {
	return filepath.Join(ConfigDir(), "identity.key")
}

// ServerIdentityPath is where the server's own X25519 identity key lives.
func ServerIdentityPath() string {
	return filepath.Join(ConfigDir(), "server_identity.key")
}

// MonitorsPath is the base path for the server's per-client monitor stores. Each
// client's policies live in a sibling file derived from its key (see
// server.ClientMonitorPath), so monitors are segregated like tokens.
func MonitorsPath() string {
	return filepath.Join(ConfigDir(), "monitors.bin")
}

// LocalMonitorsPath is where the local `viaduct monitor` daemon and the desktop
// app persist their in-process monitor policies (separate from the server's).
func LocalMonitorsPath() string {
	return filepath.Join(ConfigDir(), "local_monitors.bin")
}

// AddAuthorizedKey appends a client public key to the server's authorized list
// if it isn't already present. It returns false if the key was already there.
func (c *Config) AddAuthorizedKey(key string) bool {
	for _, k := range c.Server.AuthorizedKeys {
		if k == key {
			return false
		}
	}
	c.Server.AuthorizedKeys = append(c.Server.AuthorizedKeys, key)
	return true
}

// SetAccountToken records (or clears) an account's pushed credentials, keyed
// by its account key. An empty token removes the entry.
func (c *Config) SetAccountToken(acctKey string, cred ClientCredential) {
	if cred.Token == "" {
		delete(c.Server.Accounts, acctKey)
		return
	}
	if c.Server.Accounts == nil {
		c.Server.Accounts = make(map[string]ClientCredential)
	}
	c.Server.Accounts[acctKey] = cred
}

// SetClientAccount records which account key a paired client's public key
// currently routes to.
func (c *Config) SetClientAccount(clientKey, acctKey string) {
	if c.Server.ClientKeys == nil {
		c.Server.ClientKeys = make(map[string]string)
	}
	c.Server.ClientKeys[clientKey] = acctKey
}

// FindRemote returns the saved remote with the given name, or nil.
func (c *Config) FindRemote(name string) *RemoteServer {
	for i := range c.Remotes {
		if c.Remotes[i].Name == name {
			return &c.Remotes[i]
		}
	}
	return nil
}

// UpsertRemote stores or updates a remote server by name.
func (c *Config) UpsertRemote(r RemoteServer) {
	if existing := c.FindRemote(r.Name); existing != nil {
		*existing = r
		return
	}
	c.Remotes = append(c.Remotes, r)
}

type Prefs struct {
	LogDeletions bool `json:"log_deletions"`
	// SkipConfirm, when true, lets the desktop app start a deletion without the
	// final confirmation step.
	SkipConfirm bool `json:"skip_confirm"`
	// PreScan, when true, enumerates the full message list before deleting
	// anything, giving an exact total/ETA up front at the cost of a short scan
	// before the first delete. When false, deletion streams as it scans.
	PreScan bool `json:"pre_scan"`
}

type GuildCache struct {
	Guilds []discord.Guild `json:"guilds"`
}

func ConfigDir() string {
	// On Windows, prefer the conventional per-user roaming app-data directory
	// over the Unix-style ~/.config used elsewhere.
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "viaduct")
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "viaduct")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".viaduct")
	}
	return filepath.Join(home, ".config", "viaduct")
}

func LogDir() string {
	return filepath.Join(ConfigDir(), "logs")
}

func configPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// ConfigPath is where the config file lives, for tools that need to inspect
// it directly (e.g. reading a JSON field an older version of Config dropped).
func ConfigPath() string {
	return configPath()
}

func cachePath() string {
	return filepath.Join(ConfigDir(), "cache.json")
}

func ensureDirs() error {
	if err := os.MkdirAll(ConfigDir(), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.MkdirAll(LogDir(), 0700); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	return nil
}

func DefaultConfig() *Config {
	return &Config{
		Prefs: Prefs{
			LogDeletions: true,
		},
	}
}

func (c *Config) Save() error {
	if err := ensureDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath(), data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func (c *Config) Load() error {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if err := json.Unmarshal(data, c); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	return nil
}

// Update applies mutate to the config as it currently exists on disk and writes
// the result back. Unlike loading a snapshot at startup and later calling Save,
// which serializes that whole (possibly stale) struct and clobbers any field a
// different process wrote in the meantime, Update re-reads first so it only
// changes the fields mutate touches. This matters when the server and a client
// run on the same machine and share one config file: the server persists its
// accounts and authorized keys while the client persists its paired `remotes`,
// and neither should erase the other's fields. Callers that may run Update
// concurrently within one process must still serialize it themselves.
func Update(mutate func(*Config)) error {
	c := DefaultConfig()
	// A missing file is fine — we start from defaults and create it on Save.
	if _, err := os.Stat(configPath()); err == nil {
		if err := c.Load(); err != nil {
			return err
		}
	}
	mutate(c)
	return c.Save()
}

func SaveGuildCache(guilds []discord.Guild) error {
	if err := ensureDirs(); err != nil {
		return err
	}

	cache := GuildCache{Guilds: guilds}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal guild cache: %w", err)
	}

	return os.WriteFile(cachePath(), data, 0600)
}

func LoadGuildCache() ([]discord.Guild, error) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil, err
	}

	var cache GuildCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse guild cache: %w", err)
	}

	return cache.Guilds, nil
}
