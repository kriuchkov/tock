# Commands Reference

This document provides a comprehensive reference for all Tock commands, flags, and usage patterns.

- [Core Commands](#core-commands)
  - [`start`](#start)
  - [`stop`](#stop-alias-s)
  - [`add`](#add)
  - [`continue`](#continue-alias-c)
  - [`watch`](#watch)
- [Viewing & Reporting](#viewing--reporting)
  - [`calendar`](#calendar)
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
tock start [project] [description] [notes] [tags] [flags]
```

**Examples:**

```bash

tock start                                              # Interactive mode (requires no arguments)
tock start "Backend" "API implementation"                # Quick start with arguments
tock start "Project" "Desc" "My note" "tag1, tag2"       # Positional notes/tags
tock start -p "Backend" -d "API implementation" -t 09:30 # Start at a specific time
tock start --note "Meeting notes" --tag "meeting"        # Start with note & tag flags
```

**Flags:**

- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `-t, --time string`: Start time (HH:MM or "h:mm AM/PM")
- `--note string`: Activity notes
- `--tag strings`: Activity tags (can be specified multiple times)

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

# Stop and add a note
tock stop --note "Finished the API integration"

# Stop and add tags
tock stop --tag "coding,feature"
```

**Flags:**

- `-t, --time string`: End time (HH:MM or "h:mm AM/PM")
- `--note string`: Activity notes
- `--tag strings`: Activity tags

---

### `add`

Add a completed activity manually.

**Usage:**

```bash
tock add [flags]
```

**Examples:**

```bash

tock add                                                                   # Interactive mode
tock add -p "Meeting" -d "Daily Standup" -s 10:00 -e 10:15.                # Add with start and end times
tock add -p "Study" -d "Go Context" -s 14:00 --duration 1h30m              # Add with duration
tock add -p "Work" -d "Report" -s "2023-10-01 09:00" -e "2023-10-01 12:00" # Add for a specific date
tock add -p "Research" -d "Tock Features" -s 13:00 --duration 1h --note "Investigate new features" --tag "planning" --tag "tock" # Add with notes and tags
```

**Flags:**

- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `-s, --start string`: Start time (HH:MM or YYYY-MM-DD HH:MM)
- `-e, --end string`: End time (HH:MM or YYYY-MM-DD HH:MM)
- `--duration string`: Duration (e.g., "1h30m", "10m"). Used if end time is omitted.
- `--note string`: Activity notes
- `--tag strings`: Activity tags

---

### `continue` (alias: `c`)

Resume a previously tracked activity creating a new one.

**Description:**
Continue the most recent activity, or select a specific one from recent history. This is useful for quickly starting a new activity based on past work, without retyping the project and description. Continued activities receive a new timestamp and create a new entry in the log.

**Don’t confuse this with resuming a paused activity — this command always creates a new activity.**

**Usage:**

```bash
tock continue [index] [flags]
```

**Examples:**

```bash
# Continue the most recent activity
tock continue

# Continue the 2nd most recent activity (see 'tock last' or use `tock continue <tab>`)
tock continue 2

# Continue the most recent activity but with a different description
tock continue 1 -d "Code review"

# Continue with notes and tags
tock continue --note "Starting phase 2" --tag "phase-2"

# Continue with a different description
tock continue -d "Fixing regression"
```

**Flags:**

- `-p, --project string`: Override project name
- `-d, --description string`: Override description
- `-t, --start string`: Start time
- `--note string`: Activity notes
- `--tag strings`: Activity tags

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

### `list` (alias: `ls`)

View a simple list of activities for a specific day.

**Usage:**

```bash
tock list
```

**Description:**
This command opens an interactive table view focusing on the activities of a single day.
It is useful when you want to see a clean, detailed list of tasks without the calendar grid.
Activities with notes or tags will display indicators next to the description.

**Controls:**

- `Left` / `h`: Previous day
- `Right` / `l`: Next day
- `q` / `Ctrl+C`: Quit

---

### `calendar`

Open the comprehensive interactive dashboard.

**Usage:**

```bash
tock calendar
```

**Description:**
This is the full TUI experience for Tock. Depending on your terminal size, it displays:

1. **Calendar Grid**: A monthly view to visualize days with activity.
2. **Daily Details**: A timeline view of activities for the selected date, showing project, description, duration, and any tags or notes.
3. **Sidebar**: Contextual information and stats.

**Controls:**

- `Arrow Keys` / `h, j, k, l`: Navigate days
- `n`: Jump to next month
- `p`: Jump to previous month
- `j` / `k`: Scroll through the activity list (if it overflows)
- `q` / `Esc`: Quit

### `current`

Display information about the currently running activity.

**Usage:**

```bash
tock current [flags]
```

**Examples:**

```bash
tock current                                        # Default output
tock current --json                                 # JSON output
tock current --format "{{.Project}}: {{.Duration}}" # Custom format
```

**Format Variables:**

- `.Project`: Project name
- `.Description`: Activity description
- `.StartTime`: Start time (time.Time object)
- `.EndTime`: End time (time.Time object, usually nil)
- `.Duration`: Activity duration (time.Duration object)
- `.DurationHMS`: Duration formatted as HH:MM:SS

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
