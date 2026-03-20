# OpenClaw Integration

Tock ships a workspace-ready OpenClaw skill in `skills/tock`.

The intended integration model is simple: OpenClaw uses the local `tock` CLI as a tool instead of trying to replace Tock's storage backend.

## What the skill does

- Starts and stops timers with explicit flags.
- Reads the current timer with `tock current --json`.
- Reads recent activity templates with `tock last --json`.
- Exports daily activity data with `tock export --format json --stdout`.
- Adds finished activities retroactively with `tock add ... --json`.
- Continues and removes activities with `tock continue --json` and `tock remove --yes --json`.

## Why this approach

- It keeps Tock as the source of truth.
- It avoids coupling Tock to OpenClaw internals.
- It uses machine-readable JSON for agent workflows where possible.

## Install the skill into OpenClaw

Shared install for all agents on the machine:

```bash
mkdir -p ~/.openclaw/skills
cp -R ./skills/tock ~/.openclaw/skills/
```

Workspace install for a specific OpenClaw workspace:

```bash
mkdir -p ~/.openclaw/workspace/skills
cp -R ./skills/tock ~/.openclaw/workspace/skills/
```

Then start a new OpenClaw session or refresh skills.

## Verify

```bash
openclaw skills check
openclaw skills info tock
```

The skill becomes eligible when the `tock` binary is available on `PATH`.

## Suggested prompts

- "Start tracking Backend / Implement OpenClaw skill in Tock"
- "What am I tracking right now?"
- "Show me today's tracked activities as JSON"
- "Stop the current timer and add note finished MVP"
