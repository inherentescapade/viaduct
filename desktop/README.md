# Viaduct Desktop

A desktop GUI for viaduct's Discord message-deletion engine, built with
[Wails v2](https://wails.io) (Go backend + React/TypeScript frontend). It drives
the same `engine` package as the CLI and TUI — no logic is reimplemented — and
streams live progress to a soft, glassy interface.

## What it does

- **Live deletion** — sign in with your token, pick a server (or your DMs),
  clean the whole server or filter to specific channels, optionally narrow by
  date / message-ID range, preview the count (with an optional non-destructive
  dry run), then delete with a live progress meter.
- **Data package** — load a downloaded Discord "data package" export, filter
  channels (forgotten-only, no-DMs, include/exclude tokens, date range), and
  delete by message ID instead of via search. "Forgotten" channels (ones the
  export couldn't resolve a name for) can be targeted too, but Discord's
  delete endpoint requires channel access — if you've actually left the
  server those deletes will typically fail (403/404) rather than succeed.
- **Logs** — every deletion is recorded to a local NDJSON log; the UI links
  straight to the file/folder. Nothing leaves your machine except calls to
  Discord.

## Architecture

```
desktop/
  main.go      wails.Run, embeds frontend/dist
  app.go       the App struct — every exported method is callable from JS
  dto.go       JSON-friendly DTOs + converters (engine.Progress.Error -> string, etc.)
  events.go    event names streamed to the UI (run:progress, run:finished, ...)
  frontend/    React + TypeScript + Vite + Tailwind
```

The App struct holds the session (token, client, user, loaded export) and runs
each long deletion in a goroutine, emitting throttled `run:progress` events via
the Wails runtime. Cancellation flows through a per-run `context.CancelFunc`.

The frontend talks to Go through `src/lib/bridge.ts`, a thin typed wrapper over
the Wails-injected `window.go.main.App` and `window.runtime` event bus (so it
builds without relying on generated bindings).

## Develop

Prerequisites:

- Go 1.24+
- Node 18+ / npm
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Linux only: GTK/WebKit dev libraries — `libgtk-3-dev` and
  `libwebkit2gtk-4.1-dev`. Run `wails doctor` to verify your toolchain.

From this directory, use the Makefile so the right WebKit tag is applied
automatically (modern Linux ships WebKit2GTK 4.1, which needs `-tags webkit2_41`;
the Makefile auto-detects it and is a no-op on 4.0 / macOS / Windows):

```sh
make dev      # wails dev  — hot-reloading dev app
make build    # wails build — production binary in build/bin/
make doctor   # wails doctor — check toolchain + webkit version
```

Equivalent raw commands (you'd add `-tags webkit2_41` yourself on 4.1 systems):

```sh
wails dev
wails build
```

`wails build` runs `npm install` + `npm run build` first, producing
`frontend/dist`, which `main.go` embeds. If you build the Go package directly
(`go build ./desktop`) you must have run the frontend build at least once so the
embedded `frontend/dist` directory exists.

The CLI/TUI are unaffected: `go run .` at the repo root still builds the
Cobra/Bubble Tea app.
