# Commands Reference

This document provides a comprehensive reference for all Tock commands, flags, and usage patterns.

- [Core Commands](#core-commands)
  - [`start`](#start)
  - [`stop`](#stop-alias-s)
  - [`add`](#add)
  - [`continue`](#continue-alias-c)
  - [`watch`](#watch)
- [Viewing & Reporting](#viewing--reporting)
  - [`list`](#list-alias-ls-calendar)
  - [`current`](#current)
  - [`last`](#last-alias-lt)
  - [`report`](#report)
- [Data & Analysis](#data--analysis)
  - [`analyze`](#analyze)
  - [`ical`](#ical)
- [Global Flags](#global-flags)

## Core Commands

### `start`
Start a new activity.

**Usage:**
```bash
tock start [project] [description] [flags]
```

**Examples:**
```bash
# Interactive mode (requires no arguments)
tock start

# Quick start with arguments
tock start "Backend" "API implementation"

# Start at a specific time
tock start -p "Backend" -d "API implementation" -t 09:30
```

**Flags:**
- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `-t, --time string`: Start time (HH:MM or "h:mm AM/PM")

---

### `stop` (alias: `s`)
Stop the current activity.

**Usage:**
```bash
tock stop [flags]
```

**Examples:**
```bash
# Stop now
tock stop

# Stop at a specific time
tock stop -t 17:00
```

**Flags:**
- `-t, --time string`: End time (HH:MM or "h:mm AM/PM")

---

### `add`
Add a completed activity manually.

**Usage:**
```bash
tock add [flags]
```

**Examples:**
```bash
# Interactive mode
tock add

# Add with start and end times
tock add -p "Meeting" -d "Daily Standup" -s 10:00 -e 10:15

# Add with duration
tock add -p "Study" -d "Go Context" -s 14:00 --duration 1h30m

# Add for a specific date
tock add -p "Work" -d "Report" -s "2023-10-01 09:00" -e "2023-10-01 12:00"
```

**Flags:**
- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `-s, --start string`: Start time (HH:MM or YYYY-MM-DD HH:MM)
- `-e, --end string`: End time (HH:MM or YYYY-MM-DD HH:MM)
- `--duration string`: Duration (e.g., "1h30m", "10m"). Used if end time is omitted.

---

### `continue` (alias: `c`)
Resume a previously tracked activity.

**Usage:**
```bash
tock continue [index] [flags]
```

**Examples:**
```bash
# Continue the most recent activity
tock continue

# Continue the 2nd most recent activity (see 'tock last')
tock continue 2

# Continue with a different description
tock continue -d "Fixing regression"
```

**Flags:**
- `-p, --project string`: Override project name
- `-d, --description string`: Override description
- `-t, --start string`: Start time

---

### `watch`
Display a full-screen stopwatch for the current activity.

**Usage:**
```bash
tock watch [flags]
```

**Controls:**
- `Space`: Pause/Resume
- `q` / `Ctrl+C`: Quit

**Flags:**
- `-s, --stop`: Stop tracking when exiting watch mode

---

## Viewing & Reporting

### `list` (alias: `ls`, `calendar`)
Open the interactive terminal calendar (TUI) to view your history.

**Usage:**
```bash
tock list
```

**Controls:**
- `Arrow Keys` / `h, j, k, l`: Navigate days
- `n`: Next month
- `p`: Previous month
- `q` / `Esc`: Quit

---

### `current`
Display information about the currently running activity.

**Usage:**
```bash
tock current [flags]
```

**Examples:**
```bash
# Default output
tock current

# JSON output
tock current --json

# Custom format
tock current --format "{{.Project}}: {{.Duration}}"
```

**Flags:**
- `--format string`: Go template for output format
- `--json`: Output as JSON

---

### `last` (alias: `lt`)
List recent unique activities. Useful for finding IDs for `tock continue`.

**Usage:**
```bash
tock last [flags]
```

**Examples:**
```bash
# Show last 10 activities (default)
tock last

# Show last 20
tock last -n 20
```

**Flags:**
- `-n, --number int`: Number of activities to show (default 10)
- `--json`: Output as JSON

---

### `report`
Generate a text-based report of your time.

**Usage:**
```bash
tock report [flags]
```

**Examples:**
```bash
# Report for today
tock report --today

# Report for yesterday
tock report --yesterday

# Report for a specific date
tock report --date 2023-10-15

# Filter by project "Work" and show summaries only
tock report -p "Work" --summary

# JSON output for external scripts
tock report --today --json

# JSON output for a specific project
tock report --date 2023-10-15 -p "Work" --json
```

**Flags:**
- `--today`: Report for today
- `--yesterday`: Report for yesterday
- `--date string`: Report for date (YYYY-MM-DD)
- `-p, --project string`: Filter by project name
- `-d, --description string`: Filter by description text (substring)
- `-s, --summary`: Show only project totals, hide individual tasks
- `--json`: Output as JSON

---

## Data & Analysis

### `analyze`
Analyze productivity patterns like Deep Work, Context Switching, and Focus distribution.

**Usage:**
```bash
tock analyze [flags]
```

**Examples:**
```bash
# Analyze last 30 days (default)
tock analyze

# Analyze last 7 days
tock analyze -n 7
```

**Flags:**
- `-n, --days int`: Number of days to analyze (default 30)

---

### `ical`
Export activities to iCal format (.ics).

**Usage:**
```bash
tock ical [id] [flags]
```

**Examples:**
```bash
# Export single activity by ID (from list/report view)
tock ical 2026-01-29-01

# Export and open in default calendar app (macOS)
tock ical 2026-01-29-01 --open

# Bulk export all activities to a directory
tock ical --path ./calendar_export
```

**Flags:**
- `--path string`: Output directory for files
- `--open`: Open generated file in system calendar

---

## Global Flags

These flags can be used with any command.

| Flag | Description |
|------|-------------|
| `-b, --backend string` | Storage backend: `file` (default) or `timewarrior` |
| `--config string` | Path to config file (default `~/.config/tock/tock.yaml`) |
| `-f, --file string` | Path to data file (or directory for TimeWarrior) |
| `-h, --help` | Show help for command |
| `-v, --version` | Show version info |
