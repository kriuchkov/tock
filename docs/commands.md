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
  - [`list`](#list-alias-ls)
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
tock start                                                                         # Interactive mode
tock start "Backend" "API implementation"                                          # Positional arguments: Project and Description
tock start "Project" "Desc" "My note" "tag1, tag2"                                 # Positional arguments including note and tags
tock start --project "Backend" --description "API implementation"                  # Using explicit flags
tock start -p "Backend" -d "API implementation" -t 09:30                           # Start at a specific time
tock start -p "Design" -d "Mockups" --note "Home page redesign" --tag "ui,figma"   # Start with notes and tags
tock start "Backend" "API implementation" -t 10:00                                 # Mixed usage (positional + flags)
```

**Flags:**

- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `-t, --time string`: Start time (HH:MM or "h:mm AM/PM")
- `--note string`: Activity notes
- `--tag strings`: Activity tags

---

### `stop` (alias: `s`)

Stop the current activity.

**Usage:**

```bash
tock stop [flags]
```

**Examples:**

```bash
tock stop                                                         # Stop current activity now
tock stop -t 17:00                                                # Stop at a specific time
tock stop --time "17:00"                                          # Stop at a specific time (long flag)
tock stop --note "Finished the API integration, ready for review" # Stop and append a note
tock stop --tag "coding,feature"                                  # Stop and add tags
tock stop -t 18:00 --note "Leaving office"                        # Stop at 18:00 with a note
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
tock add                                                                                                                           # Interactive mode
tock add -p "Meeting" -d "Daily Standup" -s 10:00 -e 10:15                                                                         # Add with start and end times
tock add -p "Study" -d "Go Context" -s 14:00 --duration 1h30m                                                                      # Add using start time and duration
tock add -p "Work" -d "Report" -s "2023-10-01 09:00" -e "2023-10-01 12:00"                                                         # Add for a specific past date
tock add -p "Research" -d "Tock Features" -s 13:00 --duration 1h --note "New features" --tag "planning" --tag "tock"               # Add with notes and tags
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
tock continue                                        # Continue the most recent activity
tock continue 1                                      # Continue the 2nd most recent activity (index 1)
tock continue -d "Code review"                       # Continue last activity with a new description
tock continue 1 -p "New Project"                     # Continue 2nd last activity with a new project
tock continue --note "Starting phase 2" --tag "dev"  # Continue with new notes and tags
tock continue -t 09:00                               # Continue starting at a specific time
```

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

---

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

### `current`

Display information about the currently running activity.

**Usage:**

```bash
tock current [flags]
```

**Examples:**

```bash
tock current                                        # Show current activity details
tock current --json                                 # Output as JSON  
tock current --format "{{.Project}}: {{.Duration}}" # Show with custom Go template format
tock current --format "{{.Duration}}"               # Show only duration
tock current --format "{{.DurationHMS}}"            # Show duration in HH:MM:SS format
```

### `last` (alias: `lt`)

List recent activities.

**Usage:**

```bash
tock last [flags]
```

**Examples:**

```bash
tock last        # Show last 10 activities (default)
tock last -n 20  # Show last 20 activities
```

**Flags:**

- `-n, --number int`: Number of activities to show (default 10)

---

### `report`

Generate a text-based report of your time.

**Usage:**

```bash
tock report [flags]
```

**Examples:**

```bash
tock report --today                               # Report for today
tock report --yesterday                           # Report for yesterday
tock report --date 2023-10-15                     # Report for a specific date
tock report -p "Work"                             # Filter by project "Work"
tock report -d "meeting"                          # Filter by description containing "meeting"
tock report --summary                             # Show summary statistics only
tock report -p "Work" --summary                   # Show summary for project "Work"
tock report --today --json                        # JSON output for today
tock report --date 2023-10-15 -p "Work" --json    # Filtered JSON output
```

### `analyze`

Analyze productivity patterns like Deep Work, Context Switching, and Focus distribution.

**Usage:**

```bash
tock analyze [flags]
```

**Examples:**

```bash
tock analyze      # Analyze last 30 days (default)
tock analyze -n 7 # Analyze last 7 days
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
tock ical 2026-01-29-01                 # Export specific activity to stdout
tock ical 2026-01-29-01 > meeting.ics   # Save specific activity to file
tock ical 2026-01-29-01 --open          # Export and open in default calendar app
tock ical --path ./calendar_export      # Bulk export all activities to directory
tock ical 2026-01-07 --path ./export    # Export single day activities to directory
```

**Flags:**

- `--path string`: Output directory for files
- `--open`: Open generated file in system calendar
