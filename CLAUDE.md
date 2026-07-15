# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Tock is a command-line time tracker (Go). It stores activity logs in plaintext and provides an
interactive terminal UI (Bubble Tea). It supports multiple storage backends and is compatible with
the Bartib, TodoTXT, and TimeWarrior file formats.

## Commands

Build (requires CGO — `go-sqlite3` is a cgo package):

```bash
go build -o tock ./cmd/tock          # local build
make build                           # build inside golang:1.26.3 docker image
```

Test:

```bash
go test ./...                        # all tests
go test ./internal/services/activity # single package
go test -run TestStart ./internal/app/commands   # single test by name
make test                            # full run + coverage report inside docker
```

Integration tests (`tests/{file,sqlite,todotxt}/`) `go build` the `tock` binary and exec it against
temp data files — no special build tags; they run as part of `go test ./...`.

Lint (config in `.golangci.yaml`, a large curated ruleset; `golangci-lint` v2):

```bash
make linter                          # golangci-lint run --fix inside docker (authoritative)
golangci-lint run --config .golangci.yaml   # if installed locally
```

Regenerate mocks after changing a `ports` interface (mockery, config in `.mockery.yaml`):

```bash
mockery                              # writes internal/core/ports/mocks/
```

## Architecture

Hexagonal (ports & adapters). Dependencies point inward toward `internal/core`.

- **`internal/core`** — the domain. It takes no infrastructure or framework dependencies; the only
  imports allowed inward are the standard library, the `go-faster/errors` wrapper, and the
  stdlib-only `internal/timeutil` helper (used by `models` for date-range resolution). Keep it that
  way — no viper/cobra/adapters here.
  - `models/` — `Activity` and request/DTO/filter types. Note: `Activity.ID()` is derived from
    `StartTime` (`"150405"`), not a stored field, so two activities in the same second collide.
  - `ports/` — the interfaces that wire the layers: `ActivityResolver` (the full service API used by
    commands — start/stop/list/report plus notes & tags mutation), `ActivityRepository` and
    `NotesRepository` (implemented by adapters). Mocks live in `ports/mocks/` (regenerate with
    `mockery`; the committed mocks use the v3 `testify` template — do not downgrade with a v2 binary).
  - `errors/` — sentinel errors (`ErrNoActiveActivity`, `ErrNotesUnavailable`, etc.).

- **`internal/services/activity`** — the business logic. `NewService(repo, notesRepo)` returns an
  `ActivityResolver`. All activity rules live here: start/stop/continue (e.g. starting an activity
  auto-stops running ones) and notes/tags merging (`AddNote`/`AddTags`). Delivery code must not
  re-implement these rules.

- **`internal/adapters/repositories`** — one subpackage per backend, each implementing
  `ActivityRepository`: `file` (default, Bartib format), `todotxt`, `timewarrior`, `sqlite`, plus
  `notes` (sidecar notes/tags store for the non-sqlite backends). Sqlite provides its own notes repo.

- **`internal/app`** — the delivery layer.
  - `runtime/` — the composition root / DI, and the **only** package that imports concrete adapters.
    `runtime.Load()` reads config, picks the backend, builds the repositories and the activity
    service, and returns a `*Runtime`. `root.go`'s cobra `PersistentPreRunE` calls it and stashes the
    `*Runtime` in the command `context.Context`; commands retrieve it via `getRuntime(cmd)` /
    `runtime.FromContext(ctx)` (a service-locator pattern — dependencies are pulled from context, not
    injected). Adding a backend means wiring it in `runtime.initRepositories` and
    `runtime.resolveFilePath`. Note `Runtime` also exposes infra (`*config.Config`, `*viper.Viper`);
    `update.go` and `i18n.go` reach into these directly, which is the main clean-arch leak in the code.
  - `commands/` — one cobra command per file, all registered in `root.go` `NewRootCmd()`. Commands go
    through the `ActivityResolver` from the runtime and must not touch repositories directly (the
    runtime no longer exposes a repository handle). The `tray` command (macOS menu bar icon) is
    platform-split: shared wiring in `tray.go`, real impl in `tray_darwin.go`/`tray_lifecycle_darwin.go`/
    `tray_icon_darwin.go`/`tray_launchd_darwin.go` (all `//go:build darwin`, using `fyne.io/systray`), and stubs in
    `tray_other.go` — so non-darwin builds never compile systray. It runs a live timer + start/stop
    menu; `tock start`/`continue` auto-spawn it in the background when `tray.auto_start` is set
    (single-instance via a `flock` on `~/.tock/tray.lock`), and `tock tray install`/`uninstall` manage
    a launchd LaunchAgent for an always-on icon. The menu bar template icon is generated at runtime
    (no image assets) from the Tock logo mark.
  - `insights/`, `watching/`, `export/` (text/CSV/JSON + iCal), `localization/` (i18n), `updatecheck/`.

- **`cmd/tock/main.go`** — entrypoint; blank-imports the sqlite driver + goqu dialect, then calls
  `commands.Execute()`.

## Conventions

- Configuration precedence (see `internal/config`): CLI flags > env vars > config file
  (`~/.config/tock/tock.yaml` or `./tock.yaml`) > defaults. `tock.yaml.example` documents all keys.
- Wrap errors with `github.com/go-faster/errors` (`errors.Wrap`), matching existing code.
- User-facing strings go through the localization layer (`text(cmd, "key")` / `defaultText`), not
  hardcoded literals.
- The lint config forbids global variables and `init()` functions (`gochecknoglobals`,
  `gochecknoinits`) and enforces complexity/length limits — keep functions small and pass state
  explicitly. Exception: `internal/app/commands` is excluded from `gochecknoglobals` and `forbidigo`
  (see the exclusion rules in `.golangci.yaml`), so cobra command globals and `fmt.Print*` are allowed
  there. `golines` wraps at 140 columns; local imports are grouped under `github.com/kriuchkov/tock`.
- `make linter`/`make test` run in Docker and are authoritative; a locally-installed `golangci-lint`
  may differ in version from CI (v2.12.2) and surface spurious `nolintlint` hits.
