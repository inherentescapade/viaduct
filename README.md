<p align="center">
  <img src="assets/logo.png" alt="Viaduct logo" width="96" height="96" />
</p>

# Viaduct

Viaduct deletes your own messages from Discord servers and DMs: in bulk, on a
schedule, or from an offline data export. There are three interfaces, all
built on one shared engine:

- **Desktop app** — a native GUI, no Go toolchain required (see [Desktop app](#desktop-app))
- **CLI** — scriptable commands (`viaduct delete`, `viaduct monitor`, ...)
- **TUI** — a guided terminal wizard (run `viaduct` with no arguments)

Everything can run locally on your own machine, or you can hand the job to a
small always-on **viaduct server** (a cheap VPS, say) so deletions and
scheduled cleanups continue even while your laptop is off.

> [!WARNING]
> **This is a self-bot, which technically violates Discord's Terms of
> Service.** Driving a personal user token with automation — including
> bulk-deleting your own messages — is [officially against the
> rules](https://support.discord.com/hc/en-us/articles/115002192352-Automated-User-Accounts-Self-Bots)
> and Discord classes it as platform manipulation, so in principle an account
> can be actioned for it. You use viaduct at your own risk.
>
> In practice, message-deletion tools like this one very rarely draw
> enforcement — viaduct has been tested extensively (200k+ messages deleted)
> with no action taken. Still, there is no guarantee, so weigh the risk for
> yourself. (A proper bot token used only in servers where the bot is
> authorized isn't a self-bot and doesn't carry this risk at all.)

> **You'll need your Discord token** (or a bot token) to use viaduct. See
> [Getting your token](#getting-your-token) below. Treat it like a password —
> anyone with it can act as your account.

---

## Contents

- [Desktop app](#desktop-app)
  - [Download](#download)
  - [What it does](#what-it-does)
  - [Build from source](#build-from-source)
- [Local setup (CLI / TUI)](#local-setup-cli--tui)
  - [Install](#install)
  - [Getting your token](#getting-your-token)
  - [Quick start (TUI)](#quick-start-tui)
  - [CLI usage](#cli-usage)
    - [`viaduct guilds`](#viaduct-guilds)
    - [`viaduct channels`](#viaduct-channels)
    - [`viaduct delete`](#viaduct-delete)
    - [`viaduct import`](#viaduct-import-data-package)
    - [`viaduct monitor`](#viaduct-monitor-local-scheduled-cleanup)
    - [`viaduct measure`](#viaduct-measure)
    - [`viaduct config`](#viaduct-config)
  - [Date filters](#date-filters)
- [Server setup](#server-setup)
  - [Why run a server](#why-run-a-server)
  - [Starting the server](#starting-the-server)
  - [Pairing a client](#pairing-a-client)
  - [Dispatching jobs to the server](#dispatching-jobs-to-the-server)
  - [Remote monitors](#remote-monitors)
  - [Security model](#security-model)
- [Configuration reference](#configuration-reference)
- [Logs](#logs)

---

## Desktop app

For most people, the desktop app is the easiest way to use viaduct — no Go
toolchain required.

### Download

Prebuilt binaries for Windows and Linux are attached to
[GitHub Releases](../../releases) tagged `v*`. Download the one for your
platform and run it directly.

### What it does

A native GUI (Wails v2, Go + React/TypeScript) that drives the same engine as
the CLI/TUI, so its behavior matches them exactly:

- **Live deletion** — sign in with your token, pick a server (or your DMs),
  clean the whole server or filter to specific channels, optionally narrow by
  date / message-ID range, preview the count (with an optional dry run), then
  delete with a live progress meter.
- **Data package** — load a downloaded Discord "data package" export and
  delete by message ID instead of via search; see
  [`viaduct import`](#viaduct-import-data-package) below for the equivalent
  CLI flags, including the `--forgotten` caveat about channels in servers
  you've left.
- **Logs** — every deletion is recorded to a local NDJSON log; the UI links
  straight to the file/folder.

### Build from source

See [`desktop/README.md`](desktop/README.md) for prerequisites (Go, Node,
the Wails CLI, and — on Linux — GTK/WebKit dev libraries), the Makefile-based
dev workflow (`make dev`, `make build`), and the app's architecture.

---

## Local setup (CLI / TUI)

### Install

Viaduct is a Go module (`github.com/inherentescapade/viaduct`, Go 1.24+).

Install the CLI/TUI directly:

```sh
go install github.com/inherentescapade/viaduct@latest
```

Or clone and build:

```sh
git clone https://github.com/inherentescapade/viaduct.git
cd viaduct
go build -o viaduct .
```

This builds a single `viaduct` binary containing the CLI and TUI. Put it on
your `PATH`, or run it in place with `./viaduct` / `go run .`.

The desktop GUI is a separate build — see [Desktop app](#desktop-app).

### Getting your token

Viaduct needs either your personal Discord token or a bot token.

**User token** (deletes your own messages as you):

1. Open Discord in a browser.
2. Press F12 to open DevTools, and switch to the **Network** tab.
3. Do anything in Discord (send a message, switch channels) so a request fires.
4. Click any request going to `discord.com`.
5. Find the `Authorization` header in that request and copy its value.

The TUI shows these same steps when you run `viaduct` for the first time.

**Bot token** (acts as a bot in servers it's added to): grab it from the
[Discord Developer Portal](https://discord.com/developers/applications), and
pass `--bot` (or set `bot_mode: true` in config) so viaduct prefixes it with
`Bot ` and uses the bot rate-limit model.

Viaduct can also try to **auto-detect** a token from a locally installed
Discord client (Discord, Discord Canary/PTB, Vesktop, Vencord Desktop) by
scanning its local storage — this only happens when you explicitly ask for it
(e.g. `viaduct remote connect`, `viaduct remote pair`), never automatically in
the background.

Your token is stored in your local config file (`~/.config/viaduct/config.json`
on Linux/macOS, `%APPDATA%\viaduct\config.json` on Windows; permissions `0600`
where the OS supports it) once you set it — see
[Configuration reference](#configuration-reference).

### Quick start (TUI)

Run viaduct with no arguments for an interactive, guided flow:

```sh
viaduct
```

This walks you through: paste/validate token → pick a server (or DMs) → pick
channels → set date filters → preview the count → confirm → live progress →
done, with a link to the log file. Press `esc` to go back a step, `ctrl+c` to
quit at any time.

### CLI usage

Every command that hits Discord needs a token — either already saved via the
TUI/`viaduct config set token ...`, or passed per-invocation with `--token`.
Global flags available on every command:

| Flag | Description |
|---|---|
| `--token <token>` | Use this token instead of the saved one |
| `--bot` | Treat the token as a bot token |
| `--config <path>` | Use a config file at a custom path |

#### `viaduct guilds`

List the servers you're a member of.

```sh
viaduct guilds
viaduct guilds --json
```

#### `viaduct channels`

List text channels in a server.

```sh
viaduct channels "My Server"
viaduct channels 123456789012345678 --json
```

Servers can be referred to by name or ID everywhere in the CLI; results are
cached locally so repeated lookups are fast.

#### `viaduct delete`

Delete your messages from a server or from DMs.

```sh
viaduct delete "My Server"
viaduct delete "My Server" --before 2024-01-01
viaduct delete "My Server" --after 30d --channels general,memes
viaduct delete @me --exclude alice,bob        # all DMs/groups except these
viaduct delete @me --dry-run                  # count only, deletes nothing
viaduct delete 123456789012345678 --yes       # skip the confirmation prompt
```

Use the special guild `@me` to target your direct messages and group DMs.

| Flag | Description |
|---|---|
| `--channels <list>` | Only these channels (comma-separated names or IDs) |
| `--exclude <list>` | Delete everywhere in scope **except** these channels/DMs (matches by ID, name substring, or — for DMs — the recipient's username) |
| `--before <date>` | Only messages before this date |
| `--after <date>` | Only messages after this date |
| `--maxid` / `--minid` | Restrict by Discord message snowflake ID range |
| `--dry-run` | Show the count only; deletes nothing |
| `--prescan` | Enumerate the full message list first for an exact total/ETA before deleting (otherwise deletion streams as it scans) |
| `--yes` | Skip the "press Enter to start" confirmation |

Viaduct prints each message as it's found, then a live progress bar
(rate, rate-limit hits, failures) while deleting. Press `Ctrl+C` to cancel
safely at any point. Every run also writes an NDJSON log — see [Logs](#logs).

#### `viaduct import` (data package)

Delete messages by ID straight from a Discord "Data Package" export instead
of discovering them via the search API — useful because the export lists
every channel you've ever sent in, including ones the search API won't
surface (e.g. very recent messages it hasn't indexed yet).

Request your data package from Discord's User Settings → Privacy & Safety →
Request all of my data, unzip it, then point viaduct at it:

```sh
viaduct import ~/Downloads/discord/package --list          # see what's there
viaduct import ~/Downloads/discord/package --forgotten     # only servers you no longer have access to
viaduct import ~/Downloads/discord/package --no-dms --exclude "My Server,123456789"
viaduct import ~/Downloads/discord/package/Messages --before 2024-01-01 --dry-run
```

| Flag | Description |
|---|---|
| `--list` | List matching channels and exit (no deletion) |
| `--include <list>` | Only these channels (ID, name substring, or type: `DM`, `GROUP_DM`, `GUILD_TEXT`, ...) |
| `--exclude <list>` | Skip these channels |
| `--forgotten` | Only server channels the export couldn't resolve a name for — i.e. ones you've since lost access to (never DMs) |
| `--no-dms` | Skip direct and group DM channels |
| `--before` / `--after` | Date filters, same syntax as `delete` |
| `--dry-run` | List the selected channels; deletes nothing |
| `--yes` | Skip the confirmation prompt |

`--forgotten` is best-effort: Discord's delete endpoint requires you to still
have access to the channel, so if you've genuinely left the server these
deletes will typically fail (403/404) rather than succeed. Failures are still
reported in the summary and NDJSON log rather than silently dropped.
`--forgotten` is more useful for channels you lost access to for other
reasons (e.g. a permission change) while still in the server.

#### `viaduct monitor` (local scheduled cleanup)

Keep messages trimmed to a maximum age automatically, entirely on this
machine — no server or keys needed. Leave `viaduct monitor run` running (under
systemd, tmux, or nohup) on an always-on box.

```sh
viaduct monitor add --name tidy --scope @me --age 7 --enable
viaduct monitor list
viaduct monitor on <id>
viaduct monitor off <id>
viaduct monitor rm <id>
viaduct monitor run        # runs enabled monitors on schedule until Ctrl+C
```

`viaduct monitor add` flags:

| Flag | Description |
|---|---|
| `--name <name>` | Monitor name (required) |
| `--scope <server or @me>` | What to watch (default `@me`) |
| `--mode <include\|exclude>` | `exclude`: delete everywhere in scope except `--channels`. `include`: delete only in `--channels` |
| `--channels <list>` | Channels for include/exclude mode |
| `--age <n>` | Delete messages older than this many `--unit` (required) |
| `--unit <minutes\|hours\|days\|weeks>` | Unit for `--age` (default `days`) |
| `--interval <hours>` | How often to run (default 6) |
| `--enable` | Enable immediately (otherwise saved disabled) |
| `--yes` | Skip the enable confirmation |

Adding a monitor previews its current impact ("this would delete N messages
right now") before you enable it. Monitors and their schedule are stored
locally in `local_monitors.bin` inside your [config directory](#configuration-reference).

If you'd rather run scheduled cleanup on a separate always-on box instead of
this machine, use `viaduct remote monitor` against a [viaduct server](#server-setup).

#### `viaduct measure`

Empirically measures Discord's DELETE rate limits by deleting real messages
you own, in two phases: a burst-capacity probe and a sustained-rate
validation. Mostly useful for tuning/debugging viaduct itself — **this
deletes real messages**.

```sh
viaduct measure "My Server" --rounds 6 --sustain 60s
```

#### `viaduct config`

```sh
viaduct config              # print the current config as JSON
viaduct config path         # print the config directory
viaduct config set token <token>
viaduct config set bot_mode true
```

### Date filters

`--before` / `--after` (on `delete`, `import`, and the TUI) accept:

| Form | Example | Meaning |
|---|---|---|
| Calendar date | `2024-01-01` | Midnight UTC on that date |
| RFC3339 timestamp | `2024-01-01T15:04:05Z` | Exact instant |
| Relative | `30d`, `24h`, `60m` | That many days/hours/minutes ago |

---

## Server setup

### Why run a server

`viaduct serve` runs viaduct as a long-lived job owner on a small always-on
machine (a cheap VPS works well). Once it's paired with a client:

- Dispatch deletion jobs (`viaduct remote delete`, `viaduct remote dm`) that
  keep running on the server even after you disconnect.
- Set up **remote monitors** — standing retention policies that run on the
  server's schedule — without needing your own machine online.
- Check job progress and cancel jobs from any paired client.

Everything is end-to-end encrypted (X25519 + ChaCha20-Poly1305); the server
never needs your token typed into it directly — a client pushes it over the
encrypted channel after pairing.

### Starting the server

On the machine you want to host the server (e.g. your VPS):

```sh
viaduct serve --port 21776
```

| Flag | Description |
|---|---|
| `--port <port>` | Port to listen on (default `21776`) |
| `--listen <address>` | Bind address (default: all interfaces) |

On first run, the server generates its own identity keypair
(`server_identity.key` in your [config directory](#configuration-reference))
and prints a startup banner with its listen address, public key, and how many
clients/accounts are already known:

```
viaduct server starting
  Listening:   :21776
  Server key:  viaduct1:...
  Authorized:  0 client key(s)
  Tokens:      none yet (clients push their own)

To pair a client, run on it:  viaduct remote pair --name vps --server <this-host>:21776
A code will appear here when it connects; enter that code on the client.

Ready. Waiting for clients. Press Ctrl+C to stop.
```

Leave this running (systemd, tmux, `nohup`, a process manager, etc). Run
`viaduct serve keys` at any time to list the client keys currently authorized
to use this server.

### Pairing a client

From the **client** machine (your laptop, say), pair with the server in one
step — no keys to copy by hand:

```sh
viaduct remote pair --name vps --server <server-host>:21776
```

This generates a local client identity if you don't have one yet
(`identity.key` in your [config directory](#configuration-reference)), asks
the server to begin pairing, and the server prints a short numeric code on
**its** terminal. Enter that code on the
client (or pass it with `--code`) to complete a SPAKE2 exchange that proves
both sides saw the same code. After that the server trusts this client's key
permanently (until you remove it), with no restart required.

If a Discord token is detected locally (or you pass `--token`), it's pushed to
the server in the same step, encrypted end-to-end, so the server is ready to
act immediately. The paired server is saved locally under the `--name` you
gave it, so every future command just needs `--server vps`.

Other client/key commands:

```sh
viaduct remote ping --server vps          # check reachability + who the server is acting as
viaduct remote connect --server vps       # push/refresh the token the server uses
viaduct key generate                      # (rarely needed directly — pair creates this for you)
viaduct key show                          # print your client public key
```

> **Account isolation:** the server keys jobs, monitors, and logs by a hash of
> the pushed token — any client that pushes the *same* token (e.g. you pairing
> a second machine) shares that one account's jobs/monitors, since the same
> token is guaranteed to be the same Discord account. A client key that never
> pushes a token gets its own private, empty bucket.

### Dispatching jobs to the server

Once paired, run the same kinds of operations as the local `delete` command,
but they execute on the server and keep running after you disconnect:

```sh
viaduct remote delete "My Server" --server vps
viaduct remote delete @me --exclude alice,bob --server vps
viaduct remote dm @someone --server vps
```

| Flag | Description |
|---|---|
| `--server <name>` | Which paired server to use (required; `-s` shorthand) |
| `--channels <list>` | (`remote delete` only) limit to these channels |
| `--exclude <list>` | (`remote delete` only) delete everywhere in scope except these |
| `--before` / `--after` | Date filters |
| `--verify` | Re-check and re-delete stragglers until none remain |
| `--no-count` | Skip the up-front count and dispatch immediately (avoids the preview timing out on very large accounts — the server tallies the real total as it works) |
| `--yes` | Skip the confirmation prompt |

By default, `remote delete`/`remote dm` first ask the server to count how many
messages would be affected (polled in the background so a slow count on a
huge account doesn't time out the request), print the total, and ask you to
confirm before dispatching.

Track and manage dispatched jobs:

```sh
viaduct remote jobs --server vps            # list all jobs
viaduct remote job <id> --server vps        # detailed status of one job
viaduct remote cancel <id> --server vps     # cancel a running job
```

### Remote monitors

A monitor on the server is a standing rule: keep messages no older than a set
age, in channels you choose, re-applied on a schedule — running on the
server's clock, not needing your own machine online.

```sh
viaduct remote monitor add --server vps --name tidy-dms \
  --scope @me --mode exclude --channels mom,work --age 7 --enable

viaduct remote monitor list --server vps
viaduct remote monitor rm <id> --server vps
```

Flags mirror the local `monitor add` command (`--scope`, `--mode`,
`--channels`, `--age`, `--unit`, `--interval`, `--enable`, `--yes`). Adding a
monitor previews its current impact before you confirm enabling it, same as
the local version.

### Security model

- **Transport**: every client↔server exchange is encrypted end-to-end with
  X25519 key agreement + ChaCha20-Poly1305 (ECIES-style), authenticated by
  each side's identity keypair.
- **Pairing**: a first-contact SPAKE2 exchange over a short numeric code
  (shown on the server's terminal, entered on the client) authorizes a new
  client key — no key material is ever copied by hand, and no server restart
  is needed.
- **Token handling**: your Discord token is pushed to the server encrypted,
  after pairing; the server persists it locally, keyed by a hash of the token
  itself (never stored or transmitted in plaintext logs).
- **Isolation**: jobs, monitors, and logs are segregated per Discord account
  (by pushed-token hash), so a client key that hasn't pushed a token can't see
  another account's data.

Identity keys live at `identity.key` (client) and `server_identity.key`
(server) inside your [config directory](#configuration-reference).

---

## Configuration reference

Config lives in a per-user config directory:

| Platform | Path |
|---|---|
| Linux/macOS | `$XDG_CONFIG_HOME/viaduct` if `XDG_CONFIG_HOME` is set, else `~/.config/viaduct` |
| Windows | `%APPDATA%\viaduct` |

The config file itself is `config.json` in that directory, created with
`0600` permissions where the OS supports them. Run `viaduct config path` to
print the exact directory for your machine, `viaduct config` to dump the
current contents.

Key fields:

| Field | Description |
|---|---|
| `token` | Your Discord token |
| `bot_mode` | Treat `token` as a bot token |
| `preferences.log_deletions` | Write an NDJSON log of every deletion (default on) |
| `preferences.pre_scan` | Default `--prescan` behavior for `delete` |
| `server.listen` / `server.port` | Bind settings when this machine runs `viaduct serve` |
| `server.authorized_keys` | Client public keys authorized to dispatch jobs |
| `remotes` | Servers this machine knows how to reach as a client, saved by `viaduct remote pair` |

You generally shouldn't hand-edit this file for token/server fields — use
`viaduct config set`, `viaduct remote pair`, and `viaduct serve`, which merge
their writes safely even when a server and client share one machine/config
file.

## Logs

Every deletion run (local `delete`/`import`, or a server-side job) writes an
NDJSON log under `logs/` inside your [config directory](#configuration-reference).
The CLI prints the log path when a run finishes; the TUI and desktop app link
to it directly from their "done"
screen.
