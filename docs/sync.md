# Sync

**Status: draft, format version 1. Not implemented yet.**

This document defines how the tock CLI on a Mac and the tock iOS app share activities and reminders. It is the
contract between the two implementations: the Go code in this repository and the Swift code in tock-ios. Both run the
test vectors in `testdata/sync/`, and tock-ios pins them to a tock release.

## Goals and limits

- Devices share data through a folder in iCloud Drive. There is no tock server.
- Each device writes only its own files. No file ever has two writers, so iCloud never makes conflicted copies.
- Changes arrive as fast as iCloud Drive delivers them: seconds to minutes.
- Sync is off by default.
- Not in version 1: Linux, real-time delivery, sharing between people, syncing settings, compacting old changes.

## Folder

```text
<sync dir>/
  format.json                        {"format": "tock-sync", "version": 1}
  devices/<device>.json              who the device is
  journal/<device>/<YYYY-MM>.jsonl   the device's changes, append-only
```

On macOS the default sync dir is `~/Library/Mobile Documents/iCloud~com~kriuchkov~tock/Documents/sync`. This is the
iOS app's iCloud container, which shows up as a `tock` folder in iCloud Drive and in the Files app.

```yaml
sync:
  enabled: false
  dir: ~/Library/Mobile Documents/iCloud~com~kriuchkov~tock/Documents/sync
```

- **Device ID.** Each device makes a random ID once: 16 lowercase hex characters. It is stored outside the sync dir
  (`~/.tock/sync/` on macOS, the Keychain with a this-device-only attribute on iOS) and is never reused.
- **Writers.** A device writes only `devices/<own id>.json` and the files under `journal/<own id>/`. It also creates
  `format.json` if it is missing. Everything else is read-only to it.
- **Segments.** A device appends each change to the segment named after the UTC month of the change's HLC wall time
  (see [Clock](#clock)). Older segments are never written again. A month of changes is roughly 150 KB.
- **Whole lines.** A writer appends whole lines, each ending in `\n`, with one write call, and syncs the file to disk.
  A reader skips a last line that has no `\n`: its writer may still be writing it.
- **Files not yet downloaded.** iCloud can leave a placeholder (`.<name>.icloud`) in place of a file. A reader asks
  iCloud to download it and treats the file as missing until it arrives. A missing file only delays changes; it
  never makes a merge wrong (see [Merge](#merge)).
- **Cloned devices.** A restored backup or a migrated Mac can bring another device's ID along. If a device finds
  changes in its own journal with an HLC later than the last one it stored, someone else is writing under its ID.
  It then makes a new ID.

`devices/<id>.json` only describes the device for the settings screen:

```json
{"v": 1, "id": "7f3a9c2e5b1d4a60", "name": "MacBook", "platform": "macos", "app": "tock 2.4.0", "seen": "2026-11-18T19:00:00Z"}
```

## Changes

Each line of a journal is one change to one record:

```json
{"v":1,"hlc":"0001795025074932.000000","dev":"7f3a9c2e5b1d4a60","type":"activity","uid":"019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a10","set":{"start":"2026-11-18T10:00:00+03:00","end":null,"project":"Backend","description":"API implementation"}}
```

| Key | Value |
|---|---|
| `v` | Format version, `1` |
| `hlc` | When the change was made, on the hybrid logical clock below |
| `dev` | ID of the device that wrote the change |
| `type` | `activity` or `reminder` |
| `uid` | ID of the record: a lowercase UUIDv7, made by the device that created the record |
| `set` | Fields the change sets, as `path: value` pairs. Must contain at least one field |

No change is marked as "create" or "delete". A record exists once any change names its `uid`. Deleting a record
sets its `deleted` field. A device writes all of a new record's fields in its first change.

### Clock

`hlc` is a hybrid logical clock written as `<wall>.<counter>`. `<wall>` is milliseconds since the Unix epoch,
zero-padded to 16 digits; `<counter>` is zero-padded to 6 digits. Since both parts have a fixed width, comparing
the strings compares the clocks.

- **Order.** Changes are ordered by `hlc`, and by `dev` when two `hlc` are equal.
- **New change.** Let `last` be the latest clock the device has written or read. The new wall is the larger of the
  current time and `last.wall`. The counter is `last.counter + 1` if the wall equals `last.wall`, else 0. If the
  counter would pass 999999, the wall moves on by 1 ms instead.
- **Reading.** Every clock a device reads advances `last`, so a change made after seeing another one always comes
  after it, even on a device whose clock is behind. The exception is a clock more than 24 hours ahead of the
  device's own time: the device still merges that change but does not advance `last` from it. One wrong clock should
  not drag every device into the future.
- **Storage.** A device stores `last` outside the sync dir. On start it takes the later of that value and the last
  clock in its own journal.

### Activity fields

| Path | Value |
|---|---|
| `start` | Instant in RFC 3339 with offset, whole seconds. Required |
| `end` | Instant, or `null` while the activity runs |
| `project` | Non-empty string. Required |
| `description` | String |
| `notes` | String; `""` means no notes |
| `tags/<tag>` | `true` while the tag is on the activity, `false` once removed |
| `deleted` | `true` once the activity is removed |

Activity times are instants: each device shows them in its own time zone.

### Reminder fields

| Path | Value |
|---|---|
| `title` | Non-empty string. Required |
| `project` | String |
| `due` | Local wall time of the first occurrence: `YYYY-MM-DDTHH:MM`, or `YYYY-MM-DD` when `all_day`. Required |
| `all_day` | Boolean |
| `every` | Repeat rule (see [Repeat rules](#repeat-rules)); `""` for a one-off reminder |
| `until` | `YYYY-MM-DD`, the last day an occurrence can fall on, or `""` |
| `count` | Maximum number of occurrences; `0` means no limit |
| `notify` | Seconds before an occurrence the notification goes out; `0` means none |
| `notes` | String |
| `tags/<tag>` | `true` or `false`, as for activities |
| `created` | Instant |
| `occ/<YYYY-MM-DD>/done` | Instant the occurrence on that day was done, or `null` |
| `occ/<YYYY-MM-DD>/snoozed` | Instant the occurrence is snoozed until, or `null` |
| `occ/<YYYY-MM-DD>/activity` | Start of the activity tracked from the occurrence, or `null` |
| `occ/<YYYY-MM-DD>/skipped` | `true` if the occurrence was skipped |
| `deleted` | `true` once the reminder is removed |

`due` is wall time, so a reminder at 17:30 stays at 17:30 in any time zone. Occurrence dates are local dates of the
same wall clock.

A tag is the rest of the path after the first `/`, so a tag may contain `/`. A field path a reader does not know is
kept in the merged state and passed on, so newer apps can add fields within version 1.

## Merge

For every field of a record, the merged value is the value set by the latest change (by `hlc`, then `dev`) that
sets that field. This makes the merge commutative, associative and idempotent. Devices that have read the same
changes get the same state, whatever order they read them in and however many times. A duplicated line does no harm.

- **Deleted records** are hidden. A later change to another field does not bring a record back; only
  `deleted: false` does, which is how an undo works.
- **Incomplete records** are hidden while a required field is missing: an activity without `start` or `project`,
  a reminder without `title` or `due`. Writers never create them, but a reader can see one while a segment is still
  downloading.
- **Tags** of a record are the `tags/<tag>` fields set to `true`.

## Rules after the merge

Every implementation applies these to the merged state:

1. **One activity runs at a time.** Two devices can each start an activity before they see each other's change. In
   that case the running activities (`end` is `null`, not deleted) are sorted by `start`, ties broken by `uid`.
   Each of them except the last stops at the `start` of the next one, as if that activity had been started on the
   same device. A device that applies this writes the new `end` values as changes. Several devices may write the
   same values, which is harmless.
2. **Short reminder numbers are local.** The CLI's `#N` numbers are not synced. Each device keeps a map from `uid` to
   its own numbers and gives a reminder that arrives from another device the next free number. The same reminder can
   therefore be `#3` on one Mac and `#5` on another.

## Repeat rules

`every` uses the grammar of `tock remind add --every`. Writers write it in the canonical form:

| Rule | Occurs on |
|---|---|
| `daily`, `<n>d` | Every n-th day, counted from the first day |
| `weekdays` | Monday to Friday |
| `weekly`, `<n>w`, with an optional `:mon,thu` | The given weekdays (default: the first day's weekday) of every n-th week, counting weeks from the Monday of the first day's week |
| `monthly`, `<n>m`, with an optional `:<day>`, `:last` or `:<1-4\|last>-<weekday>` | One day of every n-th month: the given day (default: the first day's day), the last day, or the n-th or last given weekday. A day past the end of the month falls on its last day |
| `yearly`, `<n>y` | The first day's month and day in every n-th year; 29 February falls on 28 February in other years |

- **Canonical form.** It is lowercase. Interval 1 is written by name (`weekly`, not `1w`), and weekdays are
  three-letter names in order from Monday. This is what `Repeat.Spec()` writes.
- **Days.** The first day is the date of `due`. Occurrences fall on local dates, at the wall time of `due`, or for
  the whole day.
- **`until` and `count`.** No occurrence falls after `until`. `count` limits the number of occurrences, skipped ones
  included.
- **Status.** An occurrence is done once `done` is set. It is snoozed while `snoozed` is in the future. It is
  overdue once its time has passed, or once its day has ended for an all-day reminder. Only the latest overdue
  occurrence of a reminder is overdue; older ones that were not done are missed.

The Go implementation is `internal/core/models/reminder_repeat.go` and `reminder_occurrence.go`. Where this text and
the code disagree, the test vectors decide.

## Mac: the CLI's backends

The CLI keeps its backend (file, todotxt, timewarrior or sqlite) as the place you read and edit. Sync connects the
backend to the journals:

1. Read the backend.
2. Compare it with the state saved after the last sync, and write a change for every field that differs. This also
   picks up edits made by hand in `tock.txt`.
3. Read all journals, merge them and apply the rules above.
4. Write the merged state to the backend where it differs, and save it as the new last-sync state.

- **Where it runs.** After every command that changes data, on `tock sync`, and in `tock tray` once a minute and when
  the sync folder changes.
- **Where IDs live.** The file-based backends have nowhere to keep a `uid`. `~/.tock/sync/state.json` maps each `uid`
  to the activity's start time, alongside the last-sync state and the clock. The reminder file and sqlite get a `uid`
  field.
- **Matching activities.** An activity in the backend matches its last-sync state by start time. An activity that
  disappeared and a new one with the same project and description count as one activity whose start moved.
- **Precision.** The file and todotxt backends store minutes. Comparisons use the backend's precision, so writing
  the merged state back does not create new changes.

## iOS

The app's bundle ID is `com.kriuchkov.tock`, and its iCloud container is `iCloud.com.kriuchkov.tock`, with its
documents scope public. That makes its folder
visible in iCloud Drive on the Mac, where the CLI reads and writes it. The app watches the folder with
`NSMetadataQuery`, reads and writes through `NSFileCoordinator`, keeps a local cache of the merged state and writes
only its own journal.

## Versions

- **`format.json`.** A reader that finds a version it does not know stops syncing and asks for an update. It writes
  nothing to the folder.
- **`v` on each line.** It is the same version. Version 1 can only grow by new optional field paths, which readers
  keep (see above).

## Test vectors

Each test vector is one JSON file in `testdata/sync/`. The Go tests and the tock-ios tests run all of them.

- `hlc/*.json`: clock order and the rules for new changes.
- `merge/*.json`: `{"name", "journals": {"<dev>": ["<raw line>", ...]}, "now", "expect": {"activities": [...], "reminders": [...]}}`.
  Journals are raw lines, so parsing, cut-off lines and order are tested too. `expect` lists the visible records in
  `uid` order with their merged fields, after the rules.
- `repeat/*.json`: `{"name", "tz", "reminder": {...fields...}, "from", "to", "now", "expect": [{"due", "status"}]}`.

## Privacy

The data sits in your iCloud Drive. Apple encrypts it end to end only with Advanced Data Protection turned on.
Nothing else leaves the device.

## Open questions

- **Compaction.** Old segments are kept forever in version 1. A later version can add snapshots.
- **Real time.** Starting a Live Activity on the iPhone when tracking starts on the Mac needs push notifications
  (APNs), so it waits for a relay server in version 2.
