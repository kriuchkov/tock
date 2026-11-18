# Tock

<p align="center">
  <a href="https://github.com/kriuchkov/tock/releases">
    <img src="https://img.shields.io/github/v/release/kriuchkov/tock?style=flat-square" alt="Release" />
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/kriuchkov/tock?style=flat-square" alt="License" />
  </a>
  <a href="https://github.com/kriuchkov/tock/stargazers">
    <img src="https://img.shields.io/github/stars/kriuchkov/tock?style=flat-square" alt="Stars" />
  </a>
  <a href="go.mod">
    <img src="https://img.shields.io/github/go-mod/go-version/kriuchkov/tock?style=flat-square&logo=go&label=Go" alt="Go Version" />
  </a>
  <a href="https://github.com/kriuchkov/tock/actions">
    <img src="https://img.shields.io/github/actions/workflow/status/kriuchkov/tock/ci.yml?style=flat-square&label=tests" alt="CI" />
  </a>
  <a href="docs/openclaw.md">
    <img src="https://img.shields.io/badge/OpenClaw-ready-0A84FF?style=flat-square" alt="OpenClaw" />
  </a>
</p>

<p align="center">
  <a href="#features"><strong>Features</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#quick-start"><strong>Quick Start</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#commands"><strong>Commands</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#openclaw"><strong>OpenClaw</strong></a>
  <br />
  <a href="#configuration"><strong>Configuration</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#theming"><strong>Theme</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#file-format"><strong>File Format</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#shell-completion"><strong>Shell</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#inspiration"><strong>Inspiration</strong></a>
  <span>&nbsp;•&nbsp;</span>
  <a href="#license"><strong>License</strong></a>
</p>

## Features

<img src="assets/demo.svg" width="100%" alt="tock in the terminal: the calendar, a reminder marked done, then the reminders, list and analyze tabs" />

**Tock** is a powerful time tracking tool for the command line. It saves activity logs as plaintext files and provides an interactive terminal UI for viewing your time.

Features include:

- **Simple plaintext format** - Activities stored in human-readable files (default)
- **Multiple Backends** - Support for flat files, TodoTXT, TimeWarrior, and SQLite databases
- **Notes & Tags** - Attach detailed notes and tags to activities
- **Fix mistakes fast** - Correct any entry with `tock edit`, by flags or in an interactive editor
- **Reminders** - One-off or repeating things to do on a day or at a time, shown in the calendar and the macOS menu bar, and tracked as activities in one keypress
- **Interactive TUI** - Beautiful terminal calendar view using Bubble Tea
- **Fast & Lightweight** - Single binary, no dependencies
- **Compatible** - Reads/writes Bartib file format, TodoTXT-compatible lines, and TimeWarrior data files
- **Customizable Themes** - Multiple color themes and custom color support
- **Report Export** - Export report data as text, CSV, or JSON
- **iCal Export** - Generate .ics files for calendar integration, or sync with system calendars (macOS only)

<hr clear="right"/>

## Quick Start

### Installation

#### Homebrew (macOS)

```bash
# https://formulae.brew.sh/formula/tock
# Install from official repository (build from source)
brew install tock

# Install pre-built binary via latest release (Goreleaser)
brew install kriuchkov/tap/tock
```

#### Go Install

```bash
go install github.com/kriuchkov/tock/cmd/tock@latest
```

#### Build from source

```bash
go build -o tock ./cmd/tock
```

#### Install Script (Linux & macOS)

```bash
curl -sS https://raw.githubusercontent.com/kriuchkov/tock/master/install.sh | sh
```

#### Download Binary

Download the latest release from the [Releases](https://github.com/kriuchkov/tock/releases) page.

### Basic Usage

<img src="assets/demo.gif" width="820px" />
<hr clear="right"/>

Start tracking time:

```bash
tock start # Interactive mode (requires no arguments)
tock start "My Project" "Implementing features" # Quick start with arguments
tock start "My Project" "Implementing features" "Some notes" "tag1,tag2"  # Quick start with arguments + notes/tags
tock start -d "Implementing features" -p "My Project" # Start with flags
tock start -d "Implementing features" -p "My Project" -t 14:30 --note "Some notes" --tag "afternoon" # Start with flags + time + notes/tags
```

Stop the current activity:

```bash
tock stop # Stop now
tock stop -t 17:00 # Stop at specific time
tock stop --note "Finished for the day" # Stop with a note
tock stop --tag "end-of-day" # Stop with a tag
```

Continue a previous activity (Create a new entry with the same project/description):

```bash
tock continue  # Continue the last activity
tock continue 1   # Continue the 2nd to last activity
tock continue -d "New" # Continue last activity but with new description
tock continue -p "New Project" # Continue last activity but with new project
tock continue -t 14:00 # Continue last activity but with specific start time
tock continue --note "Resuming work" --tag "resumed" # Continue last activity with a note and tag
```

List activities for a day, or as a weekly/monthly/yearly summary:

```bash
tock list          # The activities of a single day (default)
tock list weekly   # Time per project for each day of the week
tock list monthly  # Time per project for each day of the month
tock list yearly   # Time per project for each month of the year
```

List controls:

- `Left` / `h`, `Right` / `l`: Previous / next day (or week, month, year)
- `Up` / `k`, `Down` / `j`: Move through the rows; `PgUp` / `PgDn` a screen, `Ctrl+U` / `Ctrl+D` half
  a screen, `Home` / `g` and `End` / `G` to the first and last row
- `Tab` / `Shift+Tab`: Switch between the day, week, month and year views, keeping the date
- `e` / `Enter`: Edit the selected activity in a form (daily view)
- `c`: Continue the selected activity, as `tock continue` does (daily view)
- `x` / `Delete`: Delete the selected activity after a `y` confirmation (daily view)
- `q` / `Ctrl+C`: Quit (`Esc` also works in period views)

`tock calendar`, `tock remind` and `tock list` open one TUI, each on its own tab:
calendar, reminders, list and analyze. Keys `1`–`4` switch tabs, and a tab keeps where you
were in it; reminders done on one tab show on the others. The analyze tab shows what
`tock analyze` prints, next to the time per project and per weekday, over the last 7, 30, 90
or 365 days (`Tab` switches them). `tock analyze` itself prints the same text as before.

View activities in the interactive calendar:

```bash
tock calendar
```

The calendar fits the terminal: from 140 columns it shows the month with its totals, the day,
and upcoming reminders with the last 7 days and top projects; narrower, the totals move into the
line under the header, then the month column goes and `m` opens the month over the day.

Calendar controls:

- `Left` / `Right` (`h` / `l`): Previous / next day; `Up` / `Down`: previous / next week
- `n` / `p`: Next / previous month (`m` shows the month on narrow terminals)
- `j` / `k`: Select a reminder or an activity of the day; then `Up` / `Down` move the selection too,
  and the notes and tags of the selected one show at the bottom
- `Tab`: Jump between the day's reminders and its tracked time; `Esc` clears the selection
- On a selected reminder: `Space` done, `s` start tracking, `z` snooze, `e` edit, `x` delete
- `a`: Add a reminder on the selected day
- `Ctrl+D` / `Ctrl+U`: Scroll the day
- `1`–`4`: Switch tabs
- `q` / `Esc`: Quit

Keep track of things to do on a day or at a time:

```bash
tock remind add "Team sync" -p Work --on mon --at 17:30 --every weekly --notify 10m
tock remind list    # overdue reminders and the next 7 days
tock remind         # interactive list: the reminders tab
```

The reminders tab lists them by day, overdue ones first, with the selected one's ID, notes and next
occurrence in a box at the bottom: `Space` done, `s` start tracking, `z` snooze, `e` edit, `a` add,
`x` delete, `Left` / `Right` the day or week, `Tab` the view (day, week, overdue, all); the rows
move as in the list.


## Configuration

Tock supports a flexible configuration system using YAML files and environment variables.

**Priority Order:**

1. Command-line flags
2. Environment variables
3. Configuration file (`~/.config/tock/tock.yaml` or `./tock.yaml`)
4. Default values

### Configuration File

Example `tock.yaml`:

```yaml
backend: file
file:
    path: /Users/user/tock.txt
todotxt:
  path: /Users/user/todo.txt
sqlite:
    path: /Users/user/tock.db
time_format: "24"
theme:
    faint: '#404040'
    highlight: '#FFFF00'
    name: custom
    primary: '#FF00FF'
    secondary: '#00FFFF'
    sub_text: '#B0B0B0'
    text: '#FFFFFF'
timewarrior:
    data_path: /Users/user/.timewarrior/data
  use_tock_tag_colors: true
  use_tock_tag_colors_calendar: true
  use_tock_tag_colors_weekly_activity: true
  use_tock_tag_colors_top_projects: true
working_hours:
  enabled: true
  stop_at: "17:30"
  weekdays: "mon,tue,wed,thu,fri"
calendar:
    time_spent_format: "15:04"
    time_start_format: "15:04"
    time_end_format: " • 15:04"
export:
    ical:
        file_name: "tock_export.ics"
weekly_target: "40h"
check_updates: true
```

When `working_hours.enabled` is `true`, tock will automatically stop the latest running activity at `working_hours.stop_at` the next time you run a command after that cutoff. The feature is disabled by default.

You can specify a custom config file path with the `--config` flag:

```bash
tock --config /path/to/tock.yaml list
```

Example a config file [tock.yaml.example](tock.yaml.example).

### Environment Variables

All settings can be overridden with environment variables (prefix `TOCK_`).

- `TOCK_BACKEND`: `file`, `todotxt`, `timewarrior`, or `sqlite`
- `TOCK_EXPORT_ICAL_FILE_NAME`: Custom filename for bulk iCal export (default: `tock_export.ics`)
- `TOCK_FILE_PATH`: Path to activity log
- `TOCK_TODOTXT_PATH`: Path to TodoTXT activity log
- `TOCK_TIME_FORMAT`: Time display format (`12` or `24`)
- `TOCK_REMINDERS_NOTIFICATIONS`: Reminder notifications from the menu bar icon (default: `true`)
- `TOCK_REMINDERS_SNOOZE`: Default snooze for reminders (default: `10m`)
Notes are stored as individual files in `~/.tock/notes/` (or relative to your configured file path).

- `TOCK_THEME_NAME`: Theme name (`dark`, `light`, `ansi_dark`, `ansi_light`, `mocha`, `custom`)
- `TOCK_WEEKLY_TARGET`: Weekly workload target as a duration (e.g., `40h`, `37h30m`)
- `TOCK_CHECK_UPDATES`: Check for updates (default: `true`)

### Storage Backends

Tock supports multiple storage backends.

## OpenClaw

Tock includes a workspace-ready OpenClaw skill in `skills/tock`.

This integration model uses the local `tock` CLI instead of coupling Tock to OpenClaw internals. For agent-safe flows, use machine-readable commands where possible:

```bash
tock current --json
tock last --json -n 10
tock export --today --format json --stdout
tock start -p "Backend" -d "Implement OpenClaw skill" --json
tock stop --json
```

Setup details are documented in [docs/openclaw.md](docs/openclaw.md).

### 1. Flat File (Default)

Stores activities in a simple plaintext file.

```bash
export TOCK_FILE_PATH="$HOME/.tock.txt"
```

### 2. TodoTXT

Stores activities in a TodoTXT-compatible file. Tock keeps exact timestamps and full field fidelity in `tock_*` key:value extensions, so round-tripping remains lossless even though base TodoTXT has date-only task metadata.

```bash
# Enable TodoTXT backend
export TOCK_BACKEND="todotxt"

# Optional: Specify custom TodoTXT file path (default: ~/.tock.todo.txt)
export TOCK_TODOTXT_PATH="$HOME/.tock.todo.txt"
```

### 3. TimeWarrior

Integrates with [TimeWarrior](https://timewarrior.net/) data files.

```bash
# Enable TimeWarrior backend
export TOCK_BACKEND="timewarrior"

# Optional: Specify custom data directory (default: ~/.timewarrior/data)
export TOCK_TIMEWARRIOR_DATA_PATH="/path/to/timewarrior/data"

# Optional: Ignore tags.*.color from timewarrior.cfg and use tock theme.tag_colors
export TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS="true"

# Optional: Selective overrides for specific calendar sections
export TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_CALENDAR="true"
export TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_WEEKLY_ACTIVITY="true"
export TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_TOP_PROJECTS="true"
```

### 4. SQLite

Stores activities in an SQLite database file.

```bash
# Enable SQLite backend
export TOCK_BACKEND="sqlite"

# Optional: Specify custom database file path (default: ~/.tock.db)
export TOCK_SQLITE_PATH="/path/to/tock.db"
```

Or use flags:

```bash
tock --backend timewarrior list
```

### Calendar View

Customize the time format in the calendar view (`tock calendar`).

```yaml
calendar:
  # Format for duration display (Go time syntax, or "decimal" for decimal hours)
  # Default: unset, durations read "45m", "2h45m"
  time_spent_format: "15:04"
  # time_spent_format: "decimal" # Decimal hours: 2h15m → "2.25", 5h45m → "5.75"

  # Format for start time (Go time syntax)
  # Default: uses global time_format
  time_start_format: "15:04"

  # Format for end time, appended to the duration; the time column then shows only the start.
  # Includes separators or icons.
  # Default: unset, the end follows the start: "10:00–12:45"
  # Example: " 🏁 15:04"
  time_end_format: " • 15:04"

  # Format for total daily duration (Go time syntax, or "decimal").
  # Defaults to time_spent_format.
  time_total_format: "15:04"

  # Put the duration of each activity before its project instead of at the end of the row
  align_duration_left: false

  # Days the Upcoming block covers, today included (1-31): today's reminders a line each,
  # every later day one line with their titles
  upcoming_days: 7
```

## Theming

Tock supports customizable color themes for its interactive views (calendar, list, edit, reminders, watch) and the colored output of `analyze`.

You can configure the theme in `tock.yaml`:

```yaml
theme:
  name: custom
  primary: "#ff0000"
```

Or use environment variables:

### Theme Name

Set `TOCK_THEME_NAME` (or `theme.name` in config) to one of:

- `dark`: Standard 256-color dark theme
- `light`: Standard 256-color light theme
- `ansi_dark`: 16-color dark theme
- `ansi_light`: 16-color light theme
- `mocha`: [Catppuccin Mocha](https://catppuccin.com/palette) palette, one yellow accent on a dark terminal; terminals without true color get the nearest 256 or 16 colors
- `custom`: Custom colors on top of `theme.base` (`dark` unless set)

### Auto-detection

If `theme.name` is not set, Tock automatically selects the best theme:

1. Detects terminal capabilities (TrueColor/256 vs ANSI).
2. Detects background color (Light vs Dark).
3. Selects the appropriate theme (e.g. `light` for light background, `ansi_dark` for dark ANSI terminal).

### Custom Colors

When `theme.name` is `custom`, you can override specific colors (ANSI color codes or hex values). Colors you leave out come from `theme.base`, which is `dark` unless set:

| Key | Variable | Colors |
| --- | --- | --- |
| `base` | `TOCK_THEME_BASE` | the built-in theme a custom one starts from |
| `primary` | `TOCK_COLOR_PRIMARY` | bars and charts |
| `secondary` | `TOCK_COLOR_SECONDARY` | errors and delete prompts |
| `text` | `TOCK_COLOR_TEXT` | main text |
| `sub_text` | `TOCK_COLOR_SUBTEXT` | times, labels, hints |
| `faint` | `TOCK_COLOR_FAINT` | borders and lines |
| `highlight` | `TOCK_COLOR_HIGHLIGHT` | accent: today, overdue reminders, the selected row's bar, active field and view |
| `tag` | `TOCK_COLOR_TAG` | tags |
| `reminder` | `TOCK_COLOR_REMINDER` | reminders |
| `surface` | `TOCK_COLOR_SURFACE` | background of the selected row; empty reverses it |
| `ok` | `TOCK_COLOR_OK` | running activity, success messages, deep focus in `analyze` |
| `warn` | `TOCK_COLOR_WARN` | paused timer in `watch`, middling scores in `analyze` |

```bash
export TOCK_THEME_NAME="custom"
export TOCK_COLOR_PRIMARY="63"   # Blue
export TOCK_COLOR_SECONDARY="196" # Red
export TOCK_COLOR_TEXT="255"     # White
export TOCK_COLOR_SUBTEXT="248"  # Light Grey
export TOCK_COLOR_TAG="34"       # Green
export TOCK_COLOR_REMINDER="81"  # Sky Blue
```

```yaml
theme:
  name: custom
  base: mocha       # start from mocha instead of dark
  highlight: "212"
```

### Per-Tag Colors

You can assign individual foreground colors to specific tags. This works with all backends.

```yaml
theme:
  tag_colors:
    work: "2"        # ANSI green
    personal: "196"  # ANSI red
    urgent: "#FF5F00"
```

When using the **TimeWarrior** backend, colors defined in `timewarrior.cfg` via `color.tag.*` entries are automatically merged on top of the `tag_colors` values — so TimeWarrior's own palette takes precedence for tags that appear in both sources.

### Example: Cyberpunk / Fuchsia Theme

<img src="assets/demo_2.png" width="820px" />

```bash
export TOCK_THEME_NAME="custom"
export TOCK_COLOR_PRIMARY="#FF00FF"   # Fuchsia
export TOCK_COLOR_SECONDARY="#00FFFF" # Cyan
export TOCK_COLOR_TEXT="#FFFFFF"      # White
export TOCK_COLOR_SUBTEXT="#B0B0B0"   # Light Grey
export TOCK_COLOR_FAINT="#404040"     # Dark Grey
export TOCK_COLOR_HIGHLIGHT="#FFFF00" # Yellow
export TOCK_COLOR_TAG="#00FF00"       # GreenGrey
export TOCK_COLOR_FAINT="#404040"     # Dark Grey
export TOCK_COLOR_HIGHLIGHT="#FFFF00" # Yellow
```

### Time Format

Configure how times are displayed and input via config file or environment variable:

```yaml
# In tock.yaml
time_format: "12"  # Use 12-hour format with AM/PM
time_format: "24"  # Use 24-hour format (default)
```

```bash
# Or via environment variable
export TOCK_TIME_FORMAT="12"  # Use 12-hour format with AM/PM
export TOCK_TIME_FORMAT="24"  # Use 24-hour format (default)
```

#### 12-Hour Format Examples

```bash
tock start -p Project -d Task -t "3:04 PM"   # or "03:04 PM"
tock start -p Project -d Task -t "3PM"       # Minutes optional
tock add -s "9:00 AM" -e "5:00 PM"
tock stop -t "4:45pm"                        # Case insensitive

# Times display as: "03:04 PM" instead of "15:04"
# Input accepts both "3:04 PM" and "03:04 PM" formats
```

#### 24-Hour Format (Default)

```bash
tock start -p Project -d Task -t 15:04
tock stop -t 17:30

# Times display as: "15:04"
```

Notes:

- Times display with zero-padded hours (e.g., "08:12 AM") for better alignment
- Input accepts both zero-padded ("03:04 PM") and non-padded ("3:04 PM") formats
- 24-hour input (e.g., "15:04") is still accepted in 12-hour mode as a fallback

## Commands

```bash

A simple timetracker for the command line

Usage:
  tock [command]

Available Commands:
  add         Add a completed activity
  analyze     Analyze your productivity patterns
  calendar    Show interactive calendar view
  completion  Generate the autocompletion script for the specified shell
  continue    Continues a previous activity
  current     Lists all currently running activities
  edit        Edit an existing activity
  export      Export report data to file
  help        Help about any command
  ical        Generate iCal (.ics) file for a specific task, all tasks in a day, or all tasks.
  last        List recent unique activities
  list        List activities: daily calendar view, or a weekly/monthly/yearly summary
  note        Append a note to an existing activity
  tag         Append tags to an existing activity
  remind      Manage reminders: things to do on a day or at a time
  remove      Remove an activity
  report      Generate time tracking report
  start       Start a new activity
  stop        Stop the current activity
  tray        Run the macOS menu bar icon (timer, start last, stop)
  version     Print the version info
  watch       Display a full-screen stopwatch for the current activity

Flags:
  -b, --backend string   Storage backend: 'file' (default), 'todotxt', 'timewarrior', or 'sqlite'
      --config string    Config file path (default is $HOME/.config/tock/tock.yaml)
  -f, --file string      Path to the activity log file (or data directory for timewarrior)
  -h, --help             help for tock
  -v, --version          version for tock

Use "tock [command] --help" for more information about a command.
```

[**→ Commands Reference**](docs/commands.md)

### Start tracking

Start a new activity. You can provide project and task as arguments, use flags, or use the interactive mode.

```bash
tock start                                             # Interactive mode
tock start "Project Name" "Task description"           # Positional arguments
tock start "Project" "Desc" "My note" "tag1, tag2"     # Positional notes/tags
tock start -p "Project" -d "Task" -t 14:30             # Start at specific time
tock start --note "Meeting notes" --tag "meeting"      # Start with note & tag flags
```

**Flags:**

- `-d, --description`: Activity description
- `-p, --project`: Project name
- `-t, --time`: Start time (format depends on TOCK_TIME_FORMAT: HH:MM or "h:mm AM/PM", optional, defaults to now)
- `--note`: Activity notes
- `--tag`: Activity tags (can be used multiple times)

### Stop tracking

Stop the currently running activity.

```bash
tock stop
tock stop -t 17:00                          # Stop at specific time
tock stop --note "Done for today"           # Stop and append a note
tock stop --tag "coding,feature"            # Stop and add tags
```

**Flags:**

- `-t, --time`: End time (format depends on TOCK_TIME_FORMAT: HH:MM or "h:mm AM/PM", optional, defaults to now)
- `--note`: Activity notes
- `--tag`: Activity tags (can be used multiple times)

### Add past activity

Add a completed activity manually. You can use flags or the interactive wizard.

```bash
tock add                                         # Interactive wizard
tock add -p "Project" -d "Task" -s 10:00 -e 11:00
tock add -p "Project" -d "Task" --day 2026-04-21 -s 10:00 -e 11:00
tock add -p "Project" -d "Task" -s 14:00 --duration 1h30m
tock add -p "Project" -d "Task" -s "2026-04-21 10:00" -e "2026-04-21 11:00"
tock add -p "Project" -d "Task" -s 10:00 -e 11:00 --note "Fixed bug #123" --tag "bugfix"
```

**Flags:**

- `-d, --description`: Activity description
- `-p, --project`: Project name
- `--day`: Day for time-only `--start` / `--end` values (`YYYY-MM-DD`)
- `-s, --start`: Start time (format depends on TOCK_TIME_FORMAT: HH:MM/YYYY-MM-DD HH:MM or "h:mm AM/PM"/"YYYY-MM-DD h:mm AM/PM")
- `-e, --end`: End time (format depends on TOCK_TIME_FORMAT: HH:MM/YYYY-MM-DD HH:MM or "h:mm AM/PM"/"YYYY-MM-DD h:mm AM/PM")
- `--duration`: Duration (e.g. 1h, 30m). Used if end time is not specified.
- `--note`: Activity notes
- `--tag`: Activity tags (can be used multiple times)

### Edit activity

Correct an entry instead of editing the log file by hand. Without flags an interactive editor opens;
with flags the change is applied right away. Without a date-index the last activity is edited.

```bash
tock edit                                        # Browse days, pick an entry, edit it in the TUI
tock edit 2025-12-10-01                          # Open the editor for one entry
tock edit -e 17:30                               # Fix the end time of the last activity
tock edit 2025-12-10-01 -p Tock -d "Review"      # Fix project and description
tock edit 2025-12-10-01 --duration 90m           # Keep the start, set a new duration
tock edit 2025-12-10-01 --day 2025-12-11         # Move the entry to another day
tock edit 2025-12-10-01 --clear-end              # Drop the end time, the activity runs again
```

**Flags:**

- `-p, --project`, `-d, --description`: New project / description
- `--day`, `-s, --start`, `-e, --end`, `--duration`: New date, times, or duration
- `--note`, `--tag`: Replace notes / tags (`note` and `tag` append instead); untouched fields are kept
- `--clear-end`: Drop the end time so the activity is running again (refused while another activity runs)
- `--json`: Output the updated activity as JSON (needs at least one field flag)

In the editor: `left`/`right` change the day, `up`/`down` select an entry, `enter` opens the form and
saves it, `tab` moves between fields, `esc` goes back, `q` quits.

### Remove activity

Remove a previously tracked activity.

```bash
tock remove                      # Remove the last activity (asks for confirmation)
tock remove -y                   # Remove the last activity without confirmation
tock remove 2025-12-10-01        # Remove a specific activity by ID
```

**Flags:**

- `-y, --yes`: Skip confirmation

### Add note later

Append a note to an already logged activity. If no key is provided, Tock updates the last activity.

```bash
tock note "Added retro summary"
tock note 2026-01-07-01 "Confirmed follow-up with design"
tock note 2026-01-07-01 "Confirmed follow-up with design" --json
```

**Flags:**

- `--json`: Output the updated activity as JSON

### Add tags later

Append tags to an already logged activity. If no key is provided, Tock updates the last activity. Existing tags are preserved, and duplicates are skipped.

```bash
tock tag review urgent
tock tag 2026-01-07-01 review urgent
tock tag 2026-01-07-01 review urgent --json
```

**Flags:**

- `--json`: Output the updated activity as JSON

### Continue activity

Continue a previously tracked activity. Useful for resuming work on a recent task.

```bash
tock continue          # Continue the last activity
tock continue 1        # Continue the 2nd to last activity
tock continue -d "New" # Continue last activity but with new description
tock continue --note "Starting phase 2" --tag "phase-2" # Continue with notes/tags
```

**Flags:**

- `-d, --description`: Override description
- `-p, --project`: Override project
- `-t, --time`: Start time (format depends on TOCK_TIME_FORMAT: HH:MM or "h:mm AM/PM")
- `--note`: Activity notes
- `--tag`: Activity tags (can be used multiple times)

### Current activity

Show the currently running activity and its duration.

```bash
tock current
tock current --format "{{.Project}}: {{.Duration}}" # Custom format (1h30m)
tock current --format "{{.Project}}: {{.DurationHMS}}" # Custom format (01:30:00)
```

### Stopwatch (Watch Mode)

Display a full-screen stopwatch for the current activity.

```bash
tock watch
tock watch --stop  # Stop the activity when quitting watch mode
```

**Flags:**

- `-s, --stop`: Stop the activity when exiting watch mode (default false)

**Controls:**

- `Space`: Pause/Resume
- `q` / `Ctrl+C`: Quit

### Recent activities

List recent unique activities. Useful to find the index for `tock continue`.

```bash
tock last
tock last -n 20  # Show last 20 activities
```

**Flags:**

- `-n, --number`: Number of activities to show (default 10)

### Calendar View (TUI)

Open the interactive terminal calendar to view and analyze your time.

```bash
tock calendar
```

**Controls:** see [the calendar controls](#basic-usage) above: arrows move through days and weeks,
`n` / `p` through months, `j` / `k` select a row of the day, `1`–`4` switch tabs, `q` quits.

### Text Report

Generate a simple text report for a day or date range.

```bash
tock report --today
tock report --yesterday
tock report --week                             # This week (Monday to Sunday)
tock report --month                            # This calendar month
tock report --date 2025-12-01
tock report --from 2026-04-01 --to 2026-04-15  # Report an inclusive date range
tock report --from 2026-04-01                  # Report from date onward
tock report --to 2026-04-15                    # Report through date
tock report -p "My Project" -d "Fixing bugs" # Filter by project and description
tock report --summary        # Show project totals only
tock report --json           # Output in JSON format
tock report --summary --json # Project totals as JSON
```

**Flags:**

- `--today`: Report for today
- `--yesterday`: Report for yesterday
- `--week`: Report for this week (Monday to Sunday)
- `--month`: Report for this month
- `--quarter`: Report for this quarter
- `--year`: Report for this year
- `--date`: Report for specific date (YYYY-MM-DD)
- `--from`: Start date for report range (YYYY-MM-DD)
- `--to`: End date for report range, inclusive (YYYY-MM-DD)
- `-p, --project`: Filter by project and aggregate by description
- `-d, --description`: Filter by description (case-insensitive substring)
- `-s, --summary`: Show only project summaries
- `--json`: Output report as JSON; with `--summary`, an object with `projects` (`project`, `duration`) and `total`

### Report Export

Export report data as text, CSV, or JSON.

```bash
tock export --today                             # Export today's report as a text file
tock export --yesterday --format csv           # Export yesterday's report as CSV
tock export --month --format csv               # Export this month as CSV
tock export --date 2026-01-29 --fmt json       # Export a specific day as JSON
tock export --from 2026-04-01 --to 2026-04-15  # Export date range
tock export --from 2026-04-01                  # Export from date to present
tock export --to 2026-04-15                    # Export all activities up to date
tock export -p "Work" -d "meeting" -m csv      # Export filtered activities as CSV
tock export --today --stdout                   # Print the export to stdout
tock export --today -o ./exports               # Write the export file to a specific directory
```

**Flags:**

- `--today`: Export data for today
- `--yesterday`: Export data for yesterday
- `--week`: Export data for this week (Monday to Sunday)
- `--month`: Export data for this month
- `--quarter`: Export data for this quarter
- `--year`: Export data for this year
- `--date`: Export data for a specific date (YYYY-MM-DD)
- `--from`: Start date for export range (YYYY-MM-DD)
- `--to`: End date for export range (YYYY-MM-DD)
- `-p, --project`: Filter by project
- `-d, --description`: Filter by description
- `-m, --format`: Export format: `txt`, `csv`, or `json` (default `txt`)
- `--fmt`: Alias for `--format`
- `-o, --path`: Output directory
- `--stdout`: Print output to stdout instead of writing a file

### Calendar Integration (iCal)

Generate iCalendar (.ics) files for your tracked activities, compatible with Google Calendar, Apple Calendar, Outlook, etc.

**Single Activity Export:**

Use the unique key shown in `tock list` or `tock report` (format `YYYY-MM-DD-NN`).

```bash
tock ical 2026-01-07-01                    # Print ICS to stdout
tock ical 2026-01-07-01 > meeting.ics      # Save to file
tock ical 2026-01-07-01 --open             # Open in default calendar app (macOS)
```

**Bulk Export (All activities):**

```bash
tock ical --path ./export                # Export all activities to a single ICS file
tock ical --open                         # Export all and open in calendar app (macOS)
tock ical 2026-01-07 --path ./export     # Save all tasks for a specific day to a single ICS file
```

**Flags:**

- `--path`: Output directory for .ics files (required for bulk export unless --open is used)
- `--open`: Automatically open generated file(s) in system calendar (macOS only)

### Productivity Analysis

<img src="assets/demo_3.png" width="280px" align="left" style="margin-right: 20px; margin-bottom: 10px;"/>

Generate an analysis of your work habits, including Deep Work Score, Chronotype estimation, and Context Switching metrics.

**Metrics:**

- **Deep Work Score**: Percentage of time spent in focused sessions (>1h).
- **Chronotype**: Estimates your peak productivity time (Morning Lark, Night Owl, etc.).
- **Context Switching**: Measures fragmentation of your workday.
- **Session Distribution**: Breakdown of work sessions by duration.

<br>

```bash
tock analyze
tock analyze --days 7
```

The same analysis is on the analyze tab of the TUI, the last tab of `tock calendar`.

**Flags:**

- `-n, --days`: Number of days to analyze (default 30)

### Reminders

Reminders are things to do on a day or at a time, once or on a repeat. A reminder can be started as a tracked activity: its project and title become the activity's project and description, and the occurrence is marked done.

```bash
tock remind add "Send invoice" -p Admin --on 2026-12-16 --at 13:00 --every monthly
tock remind add "Standup" -p Work --at 09:30 --every weekdays --notify 5m
tock remind add "Renew domain" --on 2027-03-01 --every yearly   # no --at: all-day
tock remind add                       # interactive form
tock remind list                      # overdue + the next 7 days (--today, --overdue, --all, --json)
tock remind done 3                    # mark the current occurrence done (--undo, --date)
tock remind snooze 4 15m              # or a time of day: snooze 4 17:45
tock remind start 4                   # track it as an activity and mark it done
tock remind edit 4 --at 18:00         # no flags: interactive form
tock remind rm 4 --date 2026-12-21    # skip one occurrence; without --date remove the reminder
tock remind                           # interactive list: the reminders tab of the TUI
```

`--on` takes `YYYY-MM-DD`, `today`, `tomorrow` or a weekday (`mon`); `--every` takes `daily`, `weekdays`, `weekly[:mon,thu]`, `monthly[:16|:last|:2-tue|:last-fri]`, `yearly`, or an interval such as `3d`, `2w`, `6m`, `1y`, bounded by `--until DATE` or `--count N`.

Only the latest missed occurrence of a repeating reminder counts as overdue; older ones are shown as missed. An all-day reminder becomes overdue when its day ends, and a snooze only ever moves an alert later. `tock start` prints a one-line hint when reminders are overdue.

Reminders are stored in `.tock/reminders.yaml` next to the activity log for the file, TodoTXT and TimeWarrior backends, and in a `reminders` table for SQLite.

Notifications are plain macOS banners sent by the menu bar icon (below), so they only go out while it runs. Turn them off with `reminders.notifications: false`; the default snooze is `reminders.snooze` (`10m`).

### Menu Bar Icon (macOS)

Run a menu bar (status bar) icon that shows a live timer for the running activity, with menu actions to start the last activity, pick from the last 10 recent activities, or stop the current one. macOS only.

It also shows the next reminder and a **Reminders** submenu for today and tomorrow, where each reminder can be started, marked done or snoozed; the icon gets a badge while a reminder is overdue, and banners go out when reminders come due.

Enable it in your config, then run it:

```yaml
# tock.yaml
tray:
  enabled: true
```

```bash
tock tray            # run the icon (blocks; Ctrl-C or the Quit menu item to stop)
```

**Always-on (login agent):** register a launchd agent so the icon starts at every login and stays running:

```bash
tock tray install    # install & start the login agent
tock tray uninstall  # stop & remove it
```

**Auto-start on tracking:** alternatively, have `tock start` / `tock continue` bring up the icon in the background; it closes itself once the activity is stopped (redundant if you use the login agent):

```yaml
tray:
  enabled: true
  auto_start: true
```

The running-activity timer is shown compactly (e.g. `5m`, `1:05`). Both settings can also be set via `TOCK_TRAY_ENABLED` and `TOCK_TRAY_AUTO_START`.

## Update Check

Tock automatically checks for updates once a week. When a new version is available, you will see a notification after running commands like `start` or `stop`.

You can disable this check by setting `check_updates: false` in your configuration file or using the environment variable `TOCK_CHECK_UPDATES=false`.

## File Format

Activities are stored in plaintext format (compatible with Bartib):

```
  2025-12-10 09:00 - 2025-12-10 11:30 | Project Name | Task description
  2025-12-10 13:00 | Another Project | Ongoing task
```

You can edit this file manually with any text editor.

## Shell Completion

Tock supports shell completion for Bash, Zsh, Fish, and PowerShell. This includes basic command completion and **intelligent suggestions** for:

- **Projects**: Suggests existing project names when using `-p` / `--project`.
- **Descriptions**: Suggests recent activity descriptions when using `-d` / `--description`.
  - Suggestions are context-aware: if a project is selected, it only suggests descriptions previously used for that project.
- **Continue IDs**: Suggests valid activity IDs (with details) for the `tock continue` command.

### Installation

To enable shell completion (e.g. for Oh My Zsh):

1. Create a directory for the plugin:

```bash
mkdir -p ~/.oh-my-zsh/custom/plugins/tock
```

1. Generate the completion script:

```bash
tock completion zsh > ~/.oh-my-zsh/custom/plugins/tock/_tock
```

1. Add `tock` to your plugins list in `~/.zshrc`:

```bash
plugins=(... tock)
```

1. Restart your shell:

```bash
exec zsh
```

## Shell Integration

You can display the current tracked activity directly in your shell prompt.

### Zsh / Oh My Zsh

Add the following function to your `~/.zshrc`:

```zsh
function tock_info() {
    # Returns empty string if no activity is running
    tock current --format "{{.Project}}: {{.Duration}}" 2>/dev/null
}
```

Then add `$(tock_info)` to your prompt (PROMPT/PS1) or `RPROMPT`.

**Example:**

```zsh
PROMPT='$(tock_info) %~ > '
```

### Starship

If you use [Starship](https://starship.rs), add this to your `starship.toml`:

```toml
[custom.tock]
command = "tock current --format '{{.Project}}: {{.Duration}}'"
when = "tock current > /dev/null 2>&1"
format = "[$output]($style) "
style = "bold yellow"
shell = ["bash", "--noprofile", "--norc"]
```

### Technology Stack

- **CLI Framework**: [Cobra](https://github.com/spf13/cobra)
- **TUI Components**: [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Go Version**: 1.26+

### Project Structure

- `cmd/tock/main.go` - Entry point and runtime bootstrap
- `internal/core/` - Domain layer (models, ports, and domain errors)
- `internal/services/` - Business logic
- `internal/adapters/repositories/` - File, TodoTXT, TimeWarrior, SQLite, and notes storage adapters
- `internal/app/commands/` - Cobra commands and Bubble Tea TUIs
- `internal/app/export/` - Text, CSV, JSON, and iCal export generation
- `internal/app/insights/` - Productivity analysis and calendar aggregations
- `internal/app/localization/` - Embedded locale catalogs and localized formatting
- `internal/app/runtime/` - Shared command runtime/bootstrap wiring
- `internal/app/updatecheck/` - Release update checks
- `internal/app/watching/` - Watch mode and stopwatch helpers
- `internal/config/` - Configuration loading and management

## Inspiration

Tock is inspired by and compatible with [Bartib](https://github.com/nikolassv/bartib) - an excellent time tracking tool written in Rust by Nikolas Schmidt-Voigt. It's saved me countless hours and helped me stay organized, so I wanted to create a similar tool in Go with a clean architecture approach and an interactive terminal UI.

## License

GPL-3.0-or-later
