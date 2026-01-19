# Commands Reference

| Command | Description | Key Flags | Example |
|---------|-------------|-----------|---------|
| `start` | Start a new activity | `-p` project (required)<br>`-d` description (required)<br>`-t` start time (HH:MM) | `tock start -p "Project" -d "Task"` |
| `stop` | Stop the current activity | `-t` end time (HH:MM) | `tock stop -t 17:00` |
| `add` | Add a completed activity | `-p` project (required)<br>`-d` description (required)<br>`-s` start time<br>`-e` end time<br>`--duration` duration | `tock add -p "Project" -d "Task" -s 10:00 -e 11:00` |
| `continue` | Continue a previous activity | `-d` override description<br>`-p` override project<br>`-t` start time | `tock continue 1` |
| `current` | Show currently running activity | `--format` output template | `tock current`<br>`tock current --format "{{.DurationHMS}}"` |
| `watch` | Display full-screen stopwatch | - | `tock watch` |
| `last` | List recent unique activities | `-n` number of activities (default 10) | `tock last -n 20` |
| `list` | Interactive calendar view (TUI) | - | `tock list` |
| `calendar` | Show interactive calendar view | - | `tock calendar` |
| `report` | Generate text report | `--today`<br>`--yesterday`<br>`--date` YYYY-MM-DD<br>`--project` Filter and aggregate<br>`--summary` Project summaries only | `tock report --today`<br>`tock report -p "My Project"` |
| `ical` | Generate iCal (.ics) files | `--path` output directory<br>`--open` open in calendar app<br>(no args) export all | `tock ical --path ./out`<br>`tock ical 2026-01-07-01` |
| `analyze` | Analyze productivity patterns | `-n` days to analyze (default 30) | `tock analyze --days 7` |

## Navigation (Calendar View)

| Key | Action |
|-----|--------|
| `Arrow Keys` / `h,j,k,l` | Navigate days |
| `n` | Next month |
| `p` | Previous month |
| `q` / `Esc` | Quit |

## Global Flags

| Flag | Description | Example |
|------|-------------|---------|
| `-b, --backend` | Storage backend (`file` or `timewarrior`) | `tock --backend timewarrior list` |
| `--config` | Config file path | `tock --config ~/tock.yaml list` |
| `-f, --file` | Path to activity log file | `tock -f ~/tock.txt list` |
| `-h, --help` | Show help | `tock --help` |
| `-v, --version` | Show version | `tock --version` |
