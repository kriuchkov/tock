# Tock

[Features](#features) • [Quick Start](#quick-start) • [Commands](#commands) • [File Format](#file-format) • [Architecture](#architecture) • [Inspiration](#inspiration) • [License](#license)

## Features

<img src="assets/demo.png" width="820px" />

**Tock** is a powerful time tracking tool for the command line. It saves activity logs as plaintext files and provides an interactive terminal UI for viewing your time.

Built with **Go** using Clean Architecture principles.

- 📝 **Simple plaintext format** - Activities stored in human-readable files
- 🎨 **Interactive TUI** - Beautiful terminal calendar view using Bubble Tea
- 🏗️ **Clean Architecture** - Ports & Adapters pattern for maintainability
- ⚡ **Fast & Lightweight** - Single binary, no dependencies
- 🔄 **Compatible** - Reads/writes Bartib file format and TimeWarrior data files

<hr clear="right"/>

## Quick Start

### Installation

**Homebrew (macOS)**

```bash
brew tap kriuchkov/tap
brew install tock
```

**Go Install**

```bash
go install github.com/kriuchkov/tock/cmd/tock@latest
```

**Build from source**

```bash
go build -o tock ./cmd/tock
```

**Download Binary**

Download the latest release from the [Releases](https://github.com/kriuchkov/tock/releases) page.

### Basic Usage

Start tracking time:

```bash
tock start -d "Implementing features" -p "My Project"
```

Stop the current activity:

```bash
tock stop
```

View activities in interactive calendar:

```bash
tock list
```

### Configuration

#### Storage Backends

Tock supports multiple storage backends.

**1. Flat File (Default)**

Stores activities in a simple plaintext file.

```bash
export TOCK_FILE="$HOME/.tock.txt"
```

**2. TimeWarrior**

Integrates with [TimeWarrior](https://timewarrior.net/) data files.

```bash
# Enable TimeWarrior backend
export TOCK_BACKEND="timewarrior"

# Optional: Specify custom data directory (default: ~/.timewarrior/data)
export TIMEWARRIORDB="/path/to/timewarrior/data"
```

Or use flags:

```bash
tock --backend timewarrior list
```

#### Shell Completion

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

## Commands

### Start tracking

Start a new activity. Description and project are required.

```bash
tock start -p "Project Name" -d "Task description"
tock start -p "Project" -d "Task" -t 14:30  # Start at specific time
```

**Flags:**

- `-d, --description`: Activity description (required)
- `-p, --project`: Project name (required)
- `-t, --time`: Start time in HH:MM format (optional, defaults to now)

### Stop tracking

Stop the currently running activity.

```bash
tock stop
tock stop -t 17:00  # Stop at specific time
```

**Flags:**

- `-t, --time`: End time in HH:MM format (optional, defaults to now)

### Add past activity

Add a completed activity manually. Useful for logging time retroactively.

```bash
tock add -p "Project" -d "Task" -s 10:00 -e 11:00
tock add -p "Project" -d "Task" -s 14:00 --duration 1h30m
```

**Flags:**

- `-d, --description`: Activity description (required)
- `-p, --project`: Project name (required)
- `-s, --start`: Start time (HH:MM or YYYY-MM-DD HH:MM)
- `-e, --end`: End time (HH:MM or YYYY-MM-DD HH:MM)
- `--duration`: Duration (e.g. 1h, 30m). Used if end time is not specified.

### Continue activity

Continue a previously tracked activity. Useful for resuming work on a recent task.

```bash
tock continue          # Continue the last activity
tock continue 1        # Continue the 2nd to last activity
tock continue -d "New" # Continue last activity but with new description
```

**Flags:**

- `-d, --description`: Override description
- `-p, --project`: Override project
- `-t, --time`: Start time (HH:MM)

### Current activity

Show the currently running activity and its duration.

```bash
tock current
```

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

**Controls:**

- `Arrow Keys` / `h,j,k,l`: Navigate days
- `n`: Next month
- `p`: Previous month
- `q` / `Esc`: Quit

### Text Report

Generate a simple text report for a specific day.

```bash
tock report --today
tock report --yesterday
tock report --date 2025-12-01
```

**Flags:**

- `--today`: Report for today
- `--yesterday`: Report for yesterday
- `--date`: Report for specific date (YYYY-MM-DD)

## File Format

Activities are stored in plaintext format (compatible with Bartib):

```
  2025-12-10 09:00 - 2025-12-10 11:30 | Project Name | Task description
  2025-12-10 13:00 | Another Project | Ongoing task
```

You can edit this file manually with any text editor.

## Architecture

Tock follows Clean Architecture principles with clear separation of concerns:

```
cmd/tock/              # Application entry point
internal/
  core/                 # Domain layer
    models/             # Business entities
    ports/              # Interface definitions
    dto/                # Data transfer objects
    errors/             # Domain errors
  services/             # Application layer
    activity/           # Business logic implementation
  adapters/             # Infrastructure layer
    file/               # File repository (plaintext)
    cli/                # CLI commands & TUI views
```

### Technology Stack

- **CLI Framework**: [Cobra](https://github.com/spf13/cobra)
- **TUI Components**: [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), [Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Go Version**: 1.24+

### Project Structure

- `cmd/tock/main.go` - Entry point with DI setup
- `internal/core/` - Domain layer (models, interfaces, DTOs)
- `internal/services/` - Business logic
- `internal/adapters/file/` - File storage implementation
- `internal/adapters/cli/` - CLI commands and TUI

## Inspiration

Tock is inspired by and compatible with [Bartib](https://github.com/nikolassv/bartib) - an excellent time tracking tool written in Rust by Nikolas Schmidt-Voigt. It's saved me countless hours and helped me stay organized, so I wanted to create a similar tool in Go with a clean architecture approach and an interactive terminal UI.

## License

GPL-3.0-or-later
