# MCP Server

`tock mcp` runs a local [Model Context Protocol](https://modelcontextprotocol.io) server on stdin/stdout. AI
clients such as Claude Code or opencode start it as a subprocess and use its tools to read and change your
tracked time and reminders. It uses the same backend, config and rules as the CLI, so nothing is synced or
sent anywhere by tock itself.

## Enable

The server is off by default. Turn it on in `~/.config/tock/tock.yaml`:

```yaml
mcp:
  enabled: true
  # Leave out every tool that changes data.
  read_only: false
  # Leave out single tools by name.
  disabled_tools: []
```

or with `TOCK_MCP_ENABLED=true` / `TOCK_MCP_READ_ONLY=true`. While it is off, `tock mcp` exits with an error.

## Register it in a client

```bash
tock mcp install --client claude-code            # user scope: every project
tock mcp install --client claude-code --scope project
tock mcp install --client opencode               # global opencode config
tock mcp install --client opencode --scope project
```

`install` calls the client's own CLI (`claude mcp add`, `opencode mcp add`), so tock never rewrites the
client's config files. The registered command is the current `tock` binary plus the `--config`, `--file`,
`--backend` and `--lang` flags you passed, so the server reads the same data:

```bash
tock --config ~/work/tock.yaml mcp install --client claude-code
```

If the client's CLI is not in `PATH`, or with `--print`, tock prints the command and the config entry to add by
hand instead. Install from a stable binary (Homebrew or `go install`): a path inside a build or source
directory breaks once it is rebuilt or cleaned, and tock warns about it.

Remove it again with:

```bash
tock mcp uninstall --client claude-code
```

opencode has no command to remove a server; `uninstall` tells you which entry to delete from its config.

### Manual setup

Claude Code (`.mcp.json` in a project, or `claude mcp add`):

```json
{
  "mcpServers": {
    "tock": { "type": "stdio", "command": "/opt/homebrew/bin/tock", "args": ["mcp"] }
  }
}
```

opencode (`~/.config/opencode/opencode.json` or `opencode.json` in a project):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "tock": { "type": "local", "command": ["/opt/homebrew/bin/tock", "mcp"] }
    }
  }
}
```

## Tools

Tool names carry no prefix: clients add the server name themselves (`mcp__tock__start` in Claude Code,
`tock_start` in opencode).

| Tool            | Changes data | What it does                                                                 |
| --------------- | ------------ | ---------------------------------------------------------------------------- |
| `current`       | no           | The running activity, or the latest one when nothing runs                   |
| `list`          | no           | Activities of a period (`today`, `yesterday`, `week`, …, a date or a range) |
| `report`        | no           | Time per project for a period                                                |
| `recent`        | no           | Distinct recent activities with the index `continue` takes                  |
| `analyze`       | no           | Deep work, context switches, peak hour over the last days                   |
| `reminders`     | no           | Overdue reminder occurrences, then the ones due in the next days            |
| `start`         | yes          | Start an activity; the running one stops at the new start                   |
| `stop`          | yes          | Stop the running activity                                                    |
| `continue`      | yes          | Start a recent activity again                                                |
| `add`           | yes          | Record a finished activity                                                   |
| `edit`          | yes          | Change project, description, times, notes or tags                            |
| `note`          | yes          | Append a note                                                                |
| `tag`           | yes          | Add tags                                                                     |
| `remind_add`    | yes          | Create a reminder, optionally repeating                                      |
| `remind_done`   | yes          | Mark an occurrence done (or not done)                                        |
| `remind_snooze` | yes          | Snooze an occurrence                                                         |
| `remind_track`  | yes          | Start an activity from an occurrence and mark it done                       |
| `remove`        | destructive  | Delete an activity                                                           |
| `remind_remove` | destructive  | Delete a reminder                                                            |

Tools that change data are marked as such in their MCP annotations, and `remove`/`remind_remove` as
destructive, so clients can ask before running them. `read_only: true` leaves out everything but the first
six; `disabled_tools` leaves out the ones you name.

Every result carries `now` and `timezone`, so the model reads "today" or "yesterday" the way you do. Times are
accepted as RFC 3339, `YYYY-MM-DD HH:MM` or `HH:MM` (today). Activities are addressed by their `key`
(`YYYY-MM-DD-NN`, as in `tock edit`), which `list` returns.

## Prompts

- `standup`: a standup draft from the previous working day, today and overdue reminders.
- `weekly-review`: a review of this week (or `week: last`) with time per project and focus statistics.

## Behaviour

- Calls run one at a time, so parallel tool calls from a client can't interleave a start and a stop.
- With `working_hours` set, every call first stops an activity left running past the end of the working day,
  as each CLI command does.
- With `tray.auto_start`, starting an activity through the server brings up the menu bar icon.
- Data files are written atomically under a lock, so the server, the CLI and the menu bar icon can write at
  the same time without losing each other's changes.
