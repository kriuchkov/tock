---
name: tock
description: Command-line time tracking via the `tock` CLI. Use when the user wants to start or stop tracking, inspect the current timer, review recent activities, or export daily activity data in machine-readable JSON.
metadata: { "openclaw": { "emoji": "⏱️", "homepage": "https://github.com/kriuchkov/tock", "requires": { "bins": ["tock"] }, "install": [{ "id": "brew", "kind": "brew", "formula": "tock", "bins": ["tock"], "label": "Install tock via Homebrew" }, { "id": "go", "kind": "go", "module": "github.com/kriuchkov/tock/cmd/tock@latest", "bins": ["tock"], "label": "Install tock via Go" }] } }
---

# Tock

Use `tock` for non-interactive time tracking from the shell.

## When to use

- Start a new timer with project and description.
- Stop the current timer.
- Check what is running right now.
- Fetch recent activities or a daily export as JSON.
- Add a finished activity retroactively.

## When not to use

- Do not use the interactive TUI commands for agent workflows: `tock list`, `tock calendar`, `tock watch`.
- Do not rely on positional arguments when flags are available.
- Do not call `tock start` or `tock add` without the required fields, because that opens interactive selection.

## Preferred commands

Status and inspection:

```bash
tock current --json
tock last --json -n 10
tock export --today --format json --stdout
tock export --date 2026-03-20 --format json --stdout
tock export --date 2026-03-20 --project "Backend" --format json --stdout
```

Mutations:

```bash
tock start -p "Backend" -d "Implement OpenClaw skill" --json
tock start -p "Backend" -d "Implement OpenClaw skill" --note "initial spike" --tag openclaw --tag integration --json
tock continue 1 --json
tock stop --json
tock stop --note "finished MVP" --tag done --json
tock add -p "Meeting" -d "Weekly sync" -s "2026-03-20 10:00" -e "2026-03-20 10:30" --json
tock remove 2026-03-20-01 --yes --json
```

## Operating rules

- Prefer JSON output whenever available.
- Use explicit flags like `-p`, `-d`, `-s`, `-e`, `--note`, and `--tag`.
- Quote user-provided strings.
- For deletion, prefer `--yes --json` so the command is non-interactive and machine-readable.
- For daily summaries or machine-readable history, prefer `tock export --format json --stdout` over parsing human-readable tables.
- For current activity status, prefer `tock current --json`.
- For recent reusable activity metadata, prefer `tock last --json`.

## Notes

- `tock current --json` returns an array because multiple running activities may exist in malformed or imported data.
- `tock export --format json --stdout` returns the raw activity list for the selected filter range.
- `tock start`, `tock continue`, `tock stop`, `tock add`, and `tock remove` return the affected activity as JSON when called with `--json`.