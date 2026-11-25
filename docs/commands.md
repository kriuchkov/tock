# Commands Reference

This document provides a comprehensive reference for all Tock commands, flags, and usage patterns.

- [Core Commands](#core-commands)
  - [`start`](#start)
  - [`stop`](#stop-alias-s)
  - [`add`](#add)
  - [`note`](#note-alias-annotate)
  - [`tag`](#tag-alias-tags)
  - [`edit`](#edit-alias-change)
  - [`remove`](#remove-alias-rm)
  - [`continue`](#continue-alias-c)
  - [`watch`](#watch)
- [Reminders](#reminders)
  - [`remind`](#remind-alias-reminders)
- [Viewing & Reporting](#viewing--reporting)
  - [`calendar`](#calendar)
  - [`list`](#list-alias-ls)
  - [`current`](#current)
  - [`last`](#last-alias-lt)
  - [`report`](#report)
- [Data & Analysis](#data--analysis)
  - [`analyze`](#analyze)
  - [`export`](#export-alias-e)
  - [`ical`](#ical)
- [Integrations](#integrations)
  - [`mcp`](#mcp)
  - [`sync`](#sync)

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
tock start -p "Backend" -d "API implementation" --json                             # Output created activity as JSON
```

**Flags:**

- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `-t, --time string`: Start time (HH:MM or "h:mm AM/PM")
- `--note string`: Activity notes
- `--tag strings`: Activity tags
- `--json`: Output the created activity as JSON

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
tock stop --json                                                  # Output stopped activity as JSON
```

**Flags:**

- `-t, --time string`: End time (HH:MM or "h:mm AM/PM")
- `--note string`: Activity notes
- `--tag strings`: Activity tags
- `--json`: Output the stopped activity as JSON

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
tock add -p "Meeting" -d "Daily Standup" --day 2026-04-21 -s 10:00 -e 10:15                                                       # Add time-only values for a specific day
tock add -p "Study" -d "Go Context" -s 14:00 --duration 1h30m                                                                      # Add using start time and duration
tock add -p "Work" -d "Report" -s "2023-10-01 09:00" -e "2023-10-01 12:00"                                                         # Add for a specific past date
tock add -p "Research" -d "Tock Features" -s 13:00 --duration 1h --note "New features" --tag "planning" --tag "tock"               # Add with notes and tags
tock add -p "Meeting" -d "Daily Standup" -s 10:00 -e 10:15 --json                                                                  # Output created activity as JSON
```

**Flags:**

- `-p, --project string`: Project name
- `-d, --description string`: Activity description
- `--day string`: Day for time-only `--start` / `--end` values (`YYYY-MM-DD`)
- `-s, --start string`: Start time (HH:MM or YYYY-MM-DD HH:MM)
- `-e, --end string`: End time (HH:MM or YYYY-MM-DD HH:MM)
- `--duration string`: Duration (e.g., "1h30m", "10m"). Used if end time is omitted.
- `--note string`: Activity notes
- `--tag strings`: Activity tags
- `--json`: Output the created activity as JSON

---

### `note` (alias: `annotate`)

Append a note to an existing activity.

**Usage:**

```bash
tock note [date-index] note [flags]
```

**Examples:**

```bash
tock note "Added follow-up summary"                  # Append note to the last activity
tock note 2026-03-14-01 "Confirmed decisions"       # Append note to a specific activity
tock note 2026-03-14-01 "Confirmed decisions" --json # Output updated activity as JSON
```

**Flags:**

- `--json`: Output the updated activity as JSON

---

### `tag` (alias: `tags`)

Append tags to an existing activity.

**Usage:**

```bash
tock tag [date-index] tag [tag...] [flags]
```

**Examples:**

```bash
tock tag review urgent                         # Append tags to the last activity
tock tag 2026-03-14-01 review urgent          # Append tags to a specific activity
tock tag 2026-03-14-01 review urgent --json   # Output updated activity as JSON
```

**Flags:**

- `--json`: Output the updated activity as JSON

---

### `edit` (alias: `change`)

Correct an existing activity without touching the log file by hand.

Without flags an interactive editor opens: with no date-index you browse the days and pick the entry
to change, with a date-index the form for that entry opens directly. With flags the change is applied
right away; with no date-index the last activity is edited.

**Usage:**

```bash
tock edit [date-index] [flags]
```

**Examples:**

```bash
tock edit                                        # Browse days, pick an entry, edit it in the TUI
tock edit 2026-03-14-02                          # Open the editor for one entry
tock edit -e 17:30                               # Fix the end time of the last activity
tock edit 2026-03-14-02 -p Tock -d "Review"      # Fix project and description
tock edit 2026-03-14-02 --start 09:15            # Fix the start time (same day)
tock edit 2026-03-14-02 --duration 90m           # Keep the start, set a new duration
tock edit 2026-03-14-02 --day 2026-03-15         # Move the entry (and its end time) to another day
tock edit 2026-03-14-02 --tag review,billable    # Replace the tags
tock edit 2026-03-14-02 --note "wrong end time"  # Replace the notes
tock edit 2026-03-14-02 --clear-end              # Drop the end time, the activity runs again
tock edit 2026-03-14-02 -e 17:30 --json          # Output the updated activity as JSON
```

**Flags:**

- `-p, --project`: New project name
- `-d, --description`: New activity description
- `--day`: Move the activity to this day (`YYYY-MM-DD`), keeping times and duration
- `-s, --start`: New start time (`HH:MM` or `YYYY-MM-DD HH:MM`)
- `-e, --end`: New end time (`HH:MM` or `YYYY-MM-DD HH:MM`)
- `--duration`: New duration measured from the start time (e.g. `1h`, `90m`)
- `--note`: Replace the activity notes
- `--tag`: Replace the activity tags
- `--clear-end`: Drop the end time so the activity is running again
- `--json`: Output the updated activity as JSON

**Editor keys:** `left`/`right` change the day, `up`/`down` select an entry, `enter` opens the form
and saves it, `tab` moves between fields, `ctrl+u` clears a field, `esc` goes back, `q` quits.
An empty **End** field keeps the activity running; `\n` in **Notes** is a line break.

`edit` only touches what it is given: notes, tags, and times the command does not mention are kept.
Unlike `note` and `tag`, which append, `edit` replaces the values it is given, and a blank `--project`
or `--description` is rejected. `--day` moves the whole entry, so the end time follows the start — also
when `--start` is given alongside it. `--json` needs at least one field flag; the interactive editor has
no JSON output. `--clear-end` is refused while another activity is running, since `tock stop` only ever
closes the newest open entry.

Every backend addresses an entry by its start time, so moving an entry into the minute another entry
starts in is rejected. Two entries with the same start time cannot be told apart, so `edit` refuses to
write instead of changing the wrong one, and the log file has to be fixed by hand. The default file
backend stores minutes only, so there two entries that start in the same minute share a start time.

---

### `remove` (alias: `rm`)

Remove an activity.

**Usage:**

```bash
tock remove [date-index] [flags]
```

**Examples:**

```bash
tock remove                                      # Remove the last activity (asks confirmation)
tock remove -y                                   # Remove the last activity without confirmation
tock remove 2023-10-15-01                        # Remove specific activity by ID
tock remove 2023-10-15-01 --yes                  # Remove specific activity without confirmation
tock remove 2023-10-15-01 --yes --json          # Remove and output the deleted activity as JSON
```

**Flags:**

- `-y, --yes`: Skip confirmation
- `--json`: Output the removed activity as JSON

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
tock continue --json                                 # Output the new activity as JSON
```

**Flags:**

- `-d, --description string`: Override activity description
- `-p, --project string`: Override project name
- `-t, --time string`: Start time (HH:MM or "h:mm AM/PM")
- `--note string`: Activity notes
- `--tag strings`: Activity tags
- `--json`: Output the created activity as JSON

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

## Reminders

### `remind` (alias: `reminders`)

Manage reminders: things to do on a day or at a time, once or repeatedly. Without a subcommand it opens the interactive list.

**Usage:**

```bash
tock remind [command]
```

**Interactive list controls:**

- `Tab` / `Shift+Tab`: Switch between Day, Week, Overdue and All
- `Left` / `Right`: Previous / next day or week
- `a`: Add, `e` / `Enter`: Edit
- `Space`: Mark done (again to undo)
- `s`: Start tracking as an activity
- `z`: Snooze (`reminders.snooze`, default 10m)
- `x` / `Delete`: Delete after `y`; for a repeating reminder `o` removes only the selected occurrence
- `q` / `Esc`: Quit

#### `remind add`

Add a reminder. Without a title the interactive form opens, filled in from the flags.

```bash
tock remind add "Team sync" -p Work --on mon --at 17:30 --every weekly:mon,thu --until 2026-12-31 --notify 10m
tock remind add "Water plants" --every 3d --count 10
```

**Flags** (shared with `remind edit`):

- `-p, --project`: Project used when the reminder is started as an activity
- `--on`: Day: `YYYY-MM-DD`, `today`, `tomorrow` or a weekday like `mon` (default today)
- `--at`: Time of day; without it the reminder is all-day
- `--every`: Repeat: `daily`, `weekdays`, `weekly[:mon,thu]`, `monthly[:16|:last|:2-tue|:last-fri]`, `yearly`, or an interval like `3d`, `2w`, `6m`, `1y`
- `--until`: Last day of the repeat (`none` removes it)
- `--count`: Stop the repeat after this many occurrences
- `--notify`: Notify this long before it is due (e.g. `10m`, `1h`; at most a week); all-day reminders notify at 09:00
- `--tag`, `--note`: Tags and notes
- `--json`: Output in JSON format

A date that does not fall on the repeat rule moves to the first day that does (`--on 2026-11-18 --every monthly:last-wed` starts on 25 Nov).

#### `remind list` (alias: `ls`)

List overdue reminders and the next 7 days.

- `--today`: Overdue and today's
- `--week`: Overdue and the next 7 days (default)
- `--overdue`: Only overdue
- `--all`: Every reminder with its next occurrence
- `--json`: Output in JSON format

#### `remind done`, `remind snooze`, `remind start`

Act on the current occurrence: the overdue one (or one from an earlier day still snoozed), else the next one from today. `--date YYYY-MM-DD` picks another.

```bash
tock remind done 3          # --undo marks it not done again
tock remind snooze 4        # reminders.snooze (default 10m)
tock remind snooze 4 1h     # or a time of day: 17:45
tock remind start 4         # -p overrides the project, -t the start time
```

`start` starts an activity from the reminder (stopping a running one) and marks the occurrence done.

#### `remind edit`

Edit a reminder. Without flags the interactive form opens; with flags the change applies at once. `--on` keeps the time of day, `--at` sets it, `--all-day` drops it, `--title` renames.

#### `remind remove` (alias: `rm`)

Remove a reminder after a confirmation (`-y` skips it), or with `--date` only that occurrence of a repeating one.

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
- `Tab`: Focus the selected day's reminders; then `Up`/`Down` select one, `Space` marks it done, `s` starts tracking it, `z` snoozes it, `e` edits it, `x` deletes it, `Tab`/`Esc` go back
- `a`: Add a reminder on the selected day
- `q` / `Esc`: Quit

Days with reminders get a dot (red while one is overdue). Daily and weekday repeats are left out, or every day would be marked. The sidebar lists overdue and upcoming reminders.

---

### `list` (alias: `ls`)

View activities for a single day, or an aggregated summary for a week, month, or year.

**Usage:**

```bash
tock list            # daily view (default)
tock list daily
tock list weekly
tock list monthly
tock list yearly
```

**Description:**
The optional `[period]` argument selects the view:

- `daily` (default): an interactive table focusing on the activities of a single day. It is useful when you
  want to see a clean, detailed list of tasks without the calendar grid. Activities with notes or tags will
  display indicators next to the description.
- `weekly` / `monthly`: one block per day with the time spent per project and a subtotal for days with more
  than one project, followed by the total for the whole period.
- `yearly`: the same breakdown with one block per month.

An unknown period (anything other than `daily`, `weekly`, `monthly`, `yearly`) exits with an error.

**Controls (daily view):**

- `Left` / `h`: Previous day
- `Right` / `l`: Next day
- `Up` / `k`, `Down` / `j`: Select an activity
- `x` / `Delete`: Delete the selected activity; `y` confirms, any other key cancels
- `q` / `Ctrl+C`: Quit

**Controls (weekly / monthly / yearly views):**

- `Left` / `h`: Previous week, month, or year
- `Right` / `l`: Next week, month, or year
- `Up` / `k`, `Down` / `j`: Scroll through the table
- `q` / `Esc` / `Ctrl+C`: Quit

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

Generate a text-based report of your time for a day or date range.

**Usage:**

```bash
tock report [flags]
```

**Examples:**

```bash
tock report --today                               # Report for today
tock report --yesterday                           # Report for yesterday
tock report --week                                # Report for this week (Monday to Sunday)
tock report --month                               # Report for this calendar month
tock report --date 2023-10-15                     # Report for a specific date
tock report --from 2023-10-01 --to 2023-10-15    # Report an inclusive date range
tock report --from 2023-10-01                     # Report from a date onward
tock report --to 2023-10-15                       # Report through a date
tock report -p "Work"                             # Filter by project "Work"
tock report -d "meeting"                          # Filter by description containing "meeting"
tock report --summary                             # Show summary statistics only
tock report -p "Work" --summary                   # Show summary for project "Work"
tock report --today --json                        # JSON output for today
tock report --date 2023-10-15 -p "Work" --json    # Filtered JSON output
tock report --today --summary --json              # Project totals as JSON
```

**Flags:**

- `--today`: Report for today
- `--yesterday`: Report for yesterday
- `--week`: Report for this week (Monday to Sunday)
- `--month`: Report for this month
- `--quarter`: Report for this quarter
- `--year`: Report for this year
- `--date string`: Report for a specific date (`YYYY-MM-DD`)
- `--from string`: Start date for a report range (`YYYY-MM-DD`)
- `--to string`: Inclusive end date for a report range (`YYYY-MM-DD`)
- `-p, --project string`: Filter by project and aggregate by description
- `-d, --description string`: Filter by description
- `-s, --summary`: Show only project summaries
- `--total-only`: Show only total duration
- `--json`: Output in JSON format; with `--summary`, an object with `projects` (`project`, `duration`) and `total`

The date selectors `--today`, `--yesterday`, `--week`, `--month`, `--quarter`, `--year`, `--date`, and `--from`/`--to` are mutually exclusive. Either range endpoint may be omitted.

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
tock analyze      # Analyze last 30 days (default)
tock analyze -n 7 # Analyze last 7 days
```

**Flags:**

- `-n, --days int`: Number of days to analyze (default 30)

---

### `export` (alias: `e`)

Export report data as text, CSV, or JSON.

**Usage:**

```bash
tock export [flags]
```

**Examples:**

```bash
tock export --today                             # Export today's report as a text file
tock export --yesterday --format csv           # Export yesterday's report as CSV
tock export --month --format csv               # Export this month as CSV
tock export --date 2026-01-29 --fmt json       # Export a specific day as JSON
tock export --from 2026-04-01 --to 2026-04-15  # Export an inclusive date range
tock export --from 2026-04-01                  # Export from a date onward
tock export --to 2026-04-15                    # Export through a date
tock export -p "Work" -d "meeting" -m csv      # Export filtered activities as CSV
tock export --today --stdout                   # Print the export to stdout instead of writing a file
tock export --today -o ./exports               # Write the export file to a specific directory
```

**Flags:**

- `--today`: Export data for today
- `--yesterday`: Export data for yesterday
- `--week`: Export data for this week (Monday to Sunday)
- `--month`: Export data for this month
- `--quarter`: Export data for this quarter
- `--year`: Export data for this year
- `--date string`: Export data for a specific date (`YYYY-MM-DD`)
- `--from string`: Start date for an export range (`YYYY-MM-DD`)
- `--to string`: Inclusive end date for an export range (`YYYY-MM-DD`)
- `-p, --project string`: Filter by project
- `-d, --description string`: Filter by description
- `-m, --format string`: Export format: `txt`, `csv`, or `json` (default `txt`)
- `--fmt string`: Alias for `--format`
- `-o, --path string`: Output directory
- `--stdout`: Print output to stdout instead of writing a file

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

## Integrations

### `mcp`

Run a local MCP server on stdin/stdout for AI clients. Off until `mcp.enabled: true` is set in the config.
See [MCP Server](mcp.md) for the tools and prompts.

**Usage:**

```bash
tock mcp
tock mcp install --client claude-code|opencode [--scope user|project] [--print]
tock mcp uninstall --client claude-code|opencode [--scope user|project]
```

**Flags (`install`, `uninstall`):**

- `--client string`: `claude-code` or `opencode` (required)
- `--scope string`: `user` (all projects, default) or `project` (the current directory)
- `--print`: Print the command and config entry instead of running the client's CLI (`install` only)

### `sync`

Sync activities and reminders with the tock iOS app and your other Macs through a folder in iCloud Drive. Off until
`tock sync init` turns it on. See [Sync](sync.md) for the format.

**Usage:**

```bash
tock sync init [--dir <folder>] [--restore] [--json]
tock sync [--full] [--allow-deletions] [--json]
tock sync status [--json]
```

- `init` sets the folder up (it writes `format.json` when it is missing), makes this Mac's device ID, sends what the
  backend holds, writes the other devices' records into the backend and sets `sync.enabled` (and `sync.dir` with
  `--dir`) in the config. When it matches data the backend holds with the folder's records (a first `init`, or another
  backend or file), the folder's values win for the records that match, so it first copies each file it rewrites next
  to it as `<name>.before-match-<YYYYMMDD-HHMMSS>` and prints the paths; a failed copy stops it before it writes.
  Running it again on the same folder is a full sync. With `--restore` it writes the backend's data again from the
  folder instead (see below). Sync stays with the backend and file `init` ran on: run `init` again to move it to
  another one. A command run with `--backend` or `-f` on another backend or file is not synced, and says so.
- `sync` syncs now. Once sync is on, every command that changes data syncs right after it succeeds, the MCP server
  syncs after each tool call, and `tock tray` syncs once a minute and when another device's journal changes. A sync
  stops without writing anything when the backend's data file is missing: put it back, or run `tock sync init` to
  write it again from the folder. It stops too when many records are gone from the backend at once (more than one in
  five, or more than 50), as when the file was moved, replaced by an older copy or emptied, and so does `init`, as
  neither can tell the changes made in the file since the last sync from stale values. Put the file back, or choose:
  `tock sync init --restore` writes the data again from the folder, which wins. It first copies each file it rewrites
  next to it as `<name>.before-restore-<YYYYMMDD-HHMMSS>` and prints the paths; the changes made in the data since
  the last sync, by hand or through tock while syncs stopped, stay only in those copies. It refuses, writing nothing,
  when this Mac has not synced this data with this folder yet (a first `init`, another `--dir`, backend or file): run
  `tock sync init` without `--restore` first, so the history this Mac holds reaches the folder. It refuses too while
  the folder is still downloading journals from other devices (try again once they are down), and when the folder
  holds no activity or reminder of any device yet (this Mac's own journal counts), since that would empty the data.
  `tock sync --allow-deletions` deletes the records gone on every device instead. When none of the activities, or
  none of the reminders, the backend holds is left from the last sync (a file started over or emptied, a database
  created again), `init` writes those records back on its own and syncs the other kind as usual, first copying each
  file it rewrites next to it as `<name>.before-match-<YYYYMMDD-HHMMSS>` (a notes file edited by hand since the last
  sync is replaced by the folder's notes) and printing the paths. Fewer deletions than that go through without a
  stop.
- `status` shows whether sync is on, the folder, this device, the last sync, the files iCloud has not downloaded yet,
  the invalid lines skipped and the devices in the folder; `--json` adds the backend the state was built for.

**Flags:**

- `--dir string`: The sync folder (`init` only; default: `sync.dir`, the tock app's iCloud Drive folder)
- `--restore`: Write the backend's data again from the sync folder, after copying each file it rewrites next to it
  (`init` only, on a folder this Mac has synced the backend with)
- `--full`: Compare the whole backend and every note file, not only what changed since the last sync
- `--allow-deletions`: Delete on every device the records gone from the backend even when many went at once
- `--json`: Output as JSON
