# Sync

**Status: draft, format version 1, not released yet.** The clock, the merge and the rules after it are in
`internal/core/journal`. Reading and writing the sync folder is implemented: on the Mac by `tock sync` (see
[Mac: the CLI's backends](#mac-the-clis-backends)), on iOS by the tock app.

This document defines how the tock CLI on a Mac and the tock iOS app share activities and reminders. It is the
contract between the two implementations: the Go code in this repository and the Swift code in tock-ios. Both run the
same test vectors, and tock-ios pins them to a tock release.

## Goals and limits

- Devices share data through a folder in iCloud Drive. There is no tock server.
- Each device writes only its own files. No file ever has two writers, so iCloud never makes conflicted copies.
- Changes arrive as fast as iCloud Drive delivers them: seconds to minutes.
- Sync is off by default.
- Not in version 1: Linux, real-time delivery, sharing between people, syncing settings, compacting old changes.

## Folder

```text
<sync dir>/
  format.json                          {"format": "tock-sync", "version": 1}
  devices/<device>.json                who the device is
  journal/<device>/<YYYY-MM>.jsonl     the device's changes this month, append-only
  journal/<device>/<YYYY-MM>.jsonl.gz  a closed month, compressed
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
- **Other files.** A reader ignores every name the layout above does not give, and every name that starts with `.`.
  A writer may keep such files of its own in its journal folder, such as a lock file or a temporary file it renames
  into place.
- **Segments.** A device appends each change to the segment named after the UTC month of the change's HLC wall time
  (see [Clock](#clock)). Older segments are never written again, only compressed (see
  [Reading incrementally](#reading-incrementally)). A month of changes is roughly 150 KB.
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

- **Invalid lines.** A reader skips a complete line that is not a valid change and reports it. That is a line that
  is not a JSON object, lacks a key or has one of the wrong type, has `v` other than 1, a `dev` other than the
  journal's device, a `type` it does not know, a `uid` that is not a lowercase UUIDv7, or a `set` that is empty or
  has an empty path. Keys are matched exactly, and keys a reader does not know are ignored. Every device skips the
  same lines, so they still agree.

### Clock

`hlc` is a hybrid logical clock written as `<wall>.<counter>`. `<wall>` is milliseconds since the Unix epoch,
zero-padded to 16 digits; `<counter>` is zero-padded to 6 digits. Since both parts have a fixed width, comparing
the strings compares the clocks.

- **Order.** Changes are ordered by `hlc`, and by `dev` when two `hlc` are equal. Two changes with the same `hlc`
  and `dev` come only from a cloned device; of these the later line in the journal wins.
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
| `end` | Instant, or `null` while nobody has stopped the activity |
| `auto_end` | Instant: the end the one-running rule gives the activity, as the devices that do not own it write it (see [rule 1](#rules-after-the-merge)); or `null` |
| `project` | Non-empty string. Required |
| `description` | String |
| `notes` | String; `""` means no notes |
| `tags/<tag>` | `true` while the tag is on the activity, `false` once removed |
| `deleted` | `true` once the activity is removed |

Activity times are instants: each device shows them in its own time zone. An activity's **shown end** is its `end`
when that holds an instant, otherwise its `auto_end` when that holds an instant that is not before its `start`; with
neither, the activity runs. An `auto_end` only ever holds the start of an activity that came after this one, so one
before the `start` is stale, as when the owner moved the start past it before reading it, and counts as missing: the
activity runs from its new start, and rule 1 orders it again. Every device shows and counts an activity by its shown
end.

### Reminder fields

| Path | Value |
|---|---|
| `title` | Non-empty string. Required |
| `project` | String |
| `due` | Local wall time of the first occurrence: `YYYY-MM-DDTHH:MM`, or `YYYY-MM-DD` when `all_day`. Required |
| `all_day` | Boolean |
| `every` | Repeat rule (see [Repeat rules](#repeat-rules)); `""` for a one-off reminder |
| `until` | `YYYY-MM-DD`, the last day an occurrence can fall on, or `""` |
| `count` | Maximum number of occurrences, a whole number; `0` means no limit |
| `notify` | Seconds before an occurrence the notification goes out, a whole number; `0` means none |
| `notes` | String |
| `tags/<tag>` | `true` or `false`, as for activities |
| `created` | Instant |
| `occ/<YYYY-MM-DD>/done` | Instant the occurrence on that day was done, or `null` |
| `occ/<YYYY-MM-DD>/snoozed` | Instant the occurrence is snoozed until, or `null` |
| `occ/<YYYY-MM-DD>/activity` | Start of the activity tracked from the occurrence, or `null` |
| `occ/<YYYY-MM-DD>/skipped` | `true` if the occurrence was skipped |
| `deleted` | `true` once the reminder is removed |

`due` is wall time, so a reminder at 17:30 stays at 17:30 in any time zone. Occurrence dates are local dates of the
same wall clock. A `due` without a time makes the reminder all day, and an all-day reminder ignores the time of its
`due`.

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
- **Values of the wrong type** count as missing when a record is read: a `start` that is not an RFC 3339 instant
  with whole seconds, an `end` or `auto_end` that is not one, a `project` that is a number, a `count` that is not a
  whole number, an `every` not in the canonical form. The merged state keeps them as they are, so devices still agree
  on it.
- **Tags** of a record are the `tags/<tag>` fields set to `true`.

## Rules after the merge

Every implementation applies these to the merged state:

1. **One activity runs at a time.** Two devices can each start an activity before they see each other's change. In
   that case the running activities (visible, with no [shown end](#activity-fields)) are sorted by `start`, ties broken
   by `uid`. Every device shows each of them except the last as ending at the `start` value of the next one, as if that
   activity had been started on the same device, and writes that end.
   - **An `auto_end` gives way to an earlier start.** An activity whose shown end is its `auto_end` ends at the
     `start` of the first running activity after it in that order, when that start is before its `auto_end`. Every
     device shows that end and writes it, as above. Two devices that each start an activity while a third device's
     runs, before they read each other's start, both write that activity's `auto_end`, and the later clock wins
     whatever the instants: without this the activity would overlap the earlier of the two new ones, which runs. The
     same goes for a backdated start read later. Only a running activity cuts so: one that has stopped leaves the
     `auto_end` as it is (see [Open questions](#open-questions)). A device needs only the running activities and those
     an `auto_end` stops later than the earliest start among them to apply the rule.
   - **Who writes what.** An activity's owner is the device whose change holds the merged value of its `start`, so a
     device that edits the start becomes the owner. The owner writes the end the rule gives as the activity's `end`.
     Every other device that sees the stop writes the same value as the activity's `auto_end`: when it applies the
     rule after a merge, and when comparing the CLI's backend finds that end (see
     [Mac: the CLI's backends](#mac-the-clis-backends)). Once either field holds it, the activity shows that end, so
     the rule gives it no other and nothing more is written for it.
   - **When.** After a merge, a device writes what the rule gives it only in a sync that read every segment, with no
     file waiting for a download or failing to read: a change it has not read could hold a real end, a moved start or
     an activity that runs in between. A device that sees a stop only in such a sync writes it in the next complete
     one, if the rule still gives it.
   - **Starting an activity** on a device ends the activity that runs as shown after the rules, if one does, in
     changes the device writes together with the new activity's:
     - one another device owns, when the new activity comes after it in the rule's order (a later `start`, or the same
       instant and a larger `uid`), gets `auto_end` set to the new activity's `start`, the same text the new record
       holds. That is the end the rule gives it once the new activity runs; written at once, it holds even when the new
       activity stops before the owner syncs.
     - any other, one this device owns or one the new start comes before, gets `end`: the new start, or, when the new
       start is before the running activity's own, the current time in whole seconds, and never before that start.

     Only the activity that runs as shown is stopped. Another one the rule shows stopped, whose end nobody has written
     yet because no complete sync has run, is left as it is; once the start stops the one that ran after it, the
     rule shows it running until the new start (see [Open questions](#open-questions)).
   - **Stops made on purpose** (`tock stop`, Stop in the app, an edit of the end) write `end`, on the device they are
     made on, whoever owns the activity. The end of a Mac's working day stops only the activities that Mac owns;
     another device's activity is left to its owner.
   - **Reopening** an activity on purpose, as an undo of a stop or an edit that clears its end, writes `end: null` and
     `auto_end: null` together in one change. A change that cleared only `end` would leave the activity stopped at its
     `auto_end`.
   - **Why the owner's end wins.** An end the owner wrote must never lose to an end that another device computed
     before it read it. Say the owner stops an activity at 10:30, and another device, which has not read that stop
     yet, starts one at 11:00. That device writes 11:00 as the first activity's `auto_end` and shows it until the stop
     arrives, then 10:30: the shown end takes `end` first, whatever the clocks. Had it written 11:00 as `end`, its
     later change would have won over the real end. For the same reason a stop made on purpose, on any device, shows
     over every `auto_end`.
   - **Why the others write too.** An end that is only shown runs out. Say the Mac starts an activity while the
     phone's runs, then stops it before the phone syncs. Without the `auto_end` the phone's activity would run again
     on every device from its start, and for good once the phone syncs after that: the rule only stops activities that
     run.
   - **Older readers.** A reader that does not know `auto_end` keeps it in the merged state like any field it does not
     know (see [Versions](#versions)) and shows the activity as running, as before.
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
  the whole day. A wall time that a daylight saving change skips moves forward by the length of the gap: when the
  clocks jump from 02:00 to 03:00, 02:30 becomes 03:30.
- **Midnight gaps.** A day starts at its first instant. Where a daylight saving change skips midnight (the clocks jump
  from 00:00 to 01:00 in America/Santiago, America/Havana, Africa/Cairo and Asia/Beirut), that day starts at 01:00.
  Rules count calendar dates, so the day keeps its weekday and its day of the month. An all-day occurrence lasts
  until the next day starts.
- **`until` and `count`.** No occurrence falls after `until`. `count` limits the number of occurrences, skipped ones
  included.
- **Status.** An occurrence is done once `done` is set. It is snoozed while `snoozed` is in the future. It is
  overdue once its time has passed, or once its day has ended for an all-day reminder. Only the latest overdue
  occurrence of a reminder is overdue; older ones that were not done are missed.

The Go implementation is `internal/core/models/reminder_repeat.go` and `reminder_occurrence.go`. Where this text and
the code disagree, the test vectors decide.

## Reading incrementally

A device does not reread every journal on each sync. Measured on a Mac with synthetic data, 100,000 activities and
25,000 reminder changes make 45 MB of journals. Reading all of them takes 0.5 s in Go and 3.3 s in Swift. Applying 20
new changes to a kept state takes about 50 µs.

- **Cache.** A device keeps the merged state in a local database: every record, deleted ones included, with every
  field's value, `hlc` and `dev`. On macOS this is `~/.tock/sync/state.db` (SQLite); on iOS it is SQLite in the app
  container. Screens read from it by date range.
- **Cursors.** For every segment, the device stores how many bytes of it it has applied. A sync reads each segment
  from its cursor to the last complete line, and skips a segment whose size has not changed. Changes are applied to
  the cache by the merge rule, so the result is the same as a full merge.
- **Closed months are compressed.** A month's segment is closed once its device writes a change in a later month;
  the clock never goes back, so the device never writes to it again. The device then writes the segment gzipped as
  `<YYYY-MM>.jsonl.gz` (to a temporary file, then renamed) and removes `<YYYY-MM>.jsonl`. Cursors count bytes of the
  uncompressed content, so a device that had not finished a segment carries on at the same place in the `.gz`. A
  reader that sees both files for a month uses either; they hold the same changes. On the data above, gzip makes the
  journals 9 times smaller: 4.9 MB instead of 45 MB.
- **Nothing is deleted.** Segments stay, and so do deleted records in the cache, with all their fields. Without them a
  device that was offline could bring a deleted record back, and an undo would have nothing to restore.
- **First sync.** A new device reads the whole history once: on the data above, a 4.9 MB download and about 3 s of
  reading in Swift.

## Mac: the CLI's backends

The CLI keeps its backend (file, todotxt, timewarrior or sqlite) as the place you read and edit. The bridge between
the backend and the journals goes through the same ports as the activity service (`ActivityRepository`,
`NotesRepository`, `ReminderRepository`), so it does not depend on any file format.

- **Changes made through tock** (the CLI, the TUI, the MCP server, the tray) go to the journal as they happen. `runtime`
  wraps the repositories in a decorator that records every save and removal. A recorded write only names the records to
  compare, so changes made through tock and outside it are found the same way, at the backend's precision. An edit that
  moves an activity's start saves it at the new start and then removes it at the old one, marked as the end of that
  move: only such a removal is taken for the record moving, and any other is a deletion, even one right after a save of
  another activity. The end the activity service writes when a start stops an activity another device owns is
  recorded with the start that made it, and goes to the journal as rule 1 says for a start (see
  [rule 1](#rules-after-the-merge)): as its `auto_end`, set to the new activity's `start` as the merged record holds
  it, when the new start comes after it, and as its `end` when it comes before. It does even when the new activity
  has stopped too by the time the sync runs, and in a sync that has not read every segment. When the merged state
  shows the activity stopped at an earlier start, of an activity this Mac had not read when it made the stop, that
  earlier end goes to the journal instead, as the field rule 1 gives this Mac. Nothing is journaled when the merged
  state shows the activity stopped by an `end`, or by an `auto_end` no later than the stop, which another device
  wrote meanwhile: the sync writes the merged end back. Such a stop is marked in `state.db` with its start until a
  sync goes through, so a sync in another process, or after the command's own sync failed, still tells it from a
  stop made on purpose, which goes to the journal as `end`. A backend holds one end
  per activity, its shown end; an end cleared there, by hand or with `tock edit --clear-end`, is a reopen and goes to
  the journal as `end: null` with `auto_end: null` in one change. The end of the working day does not stop an activity
  another device owns at all: the activity service asks sync who owns the running activity, and leaves it running, as
  sync would put it back as running after every command otherwise. When sync cannot tell, as when `state.db` cannot be
  read, the activity is left running too, and the command warns and goes on. Should it stop one all the same, as
  when the owner changed in between, that stop is not journaled: the sync writes the merged state back. A recorded write waits at most 10 seconds
  for a sync that holds the lock, then goes on unrecorded and is found by the next compare; an auto-stop waits as long
  as it takes.
- **Changes made outside tock** (by hand in `tock.txt`, with `timew`, with `sqlite3`) are found by comparing the
  backend with the last-sync state. A sync only compares when the backend's change token differs from the one saved
  after tock's own last write. A full compare of 100,000 activities takes about 0.1 s, the same as a `tock report`
  over that data. An end the compare finds that is the one the rule gives an activity this device does not own goes
  to the journal as its `auto_end`, as one tock wrote would.
- **Changes from other devices** are written to the backend where it differs, and the result becomes the new
  last-sync state: what the backend gives back, not the merged value. A value the backend cannot hold as it is, such
  as a `|` or a line break in a `tock.txt` description, or a quote in a timewarrior annotation, is written as the
  backend keeps it, and that form is not journaled back over the merged value. The writes of one sync go into the
  file in one pass (the repositories' `ActivityBatchWriter`), each addressing exactly the activity it read, so two
  activities in one minute of `tock.txt` stay apart. A pass writes all or nothing: timewarrior locks every month file
  it touches in a fixed order, checks every activity it addresses in all of them, and only then writes them, the
  files that gain activities before those that only lose some, so a crash in between leaves at worst a duplicate,
  never a gap. The next sync takes such a duplicate, the activity exactly as it was at its old start while the same
  activity sits where the merged state moved it, in another month, for the line left behind: it removes it and
  journals nothing. It does so only on timewarrior, and only when no other record was at the new start, so a line
  that may be another record's is never taken. tock files a timewarrior activity under the month of its local start,
  so a pass, and every save or removal of tock, looks for it in that month in the zone the Mac is in, and in the
  months a day before and after its start, which cover any other zone. The backend's change token is read before and
  after each pass: a change made outside tock in between makes the next sync compare.

| Backend | Where `uid` lives | Time precision | Change token |
|---|---|---|---|
| file (`tock.txt`) | `state.db`, by start time | minute, local wall time | Size and modification time of `tock.txt` |
| todotxt | `state.db`, by start time | second | Size and modification time of the file |
| timewarrior | `state.db`, by start time | second, UTC | Size and modification time of each `YYYY-MM.data`; only the months that changed are reread |
| sqlite | `uid` column in `activities` and `reminders` | as stored | Triggers log the changed rows, so nothing is compared |

- **Sidecar files.** The file, todotxt and timewarrior backends keep reminders in `.tock/reminders.yaml`, which gets
  a `uid` per reminder, and notes in `.tock/notes/<date>/`. Editing a note in place does not change its folder, so a
  sync checks the note files of the last 7 days. `tock tray` checks all of them once an hour, and so does
  `tock sync --full`. tock clears notes by saving them empty, so a note file that is not there is never taken for
  cleared notes: it is one a sync has not written yet, or one under another start. The merged notes stand, and the
  sync writes them where the activity is. Notes go by the local date and time of the start, which for timewarrior's
  UTC times change with the Mac's time zone: a note file read in another zone than the activity's last-sync state is
  no edit either, and the sync writes the merged notes under the new start; once it has read them there, a note file
  added under the new start is an edit. Deleting a note file by hand does not clear the notes; `tock` does. A
  todotxt or timewarrior line holds the activity's tags too, so removing the last tag from a line without a note file
  (`timew untag`) is an edit; a `tock.txt` line holds none. A sync writes another device's notes into a note file
  only while the file holds what the sync read last: one edited by hand meanwhile is left as it is, and the next sync
  that reads it journals the edit. The same goes for a reminder in `.tock/reminders.yaml` changed after the sync
  read it.
- **Matching activities.** An activity in the backend matches its last-sync state by start time, at the backend's
  precision. An activity that disappeared and a new one with the same project and description count as one activity
  whose start moved, and so does one found at the start the merged state moved its record to.
- **Precision.** Comparisons use the backend's precision, so writing the merged state back does not create new
  changes.
- **Wall times.** `tock.txt` holds local wall times with no offset. Its activities are matched and compared by wall
  time, and the last-sync state keeps the offset each line was written at, so a change of the Mac's time zone, or the
  hour repeated when daylight saving time ends, is no edit: nothing is journaled, and the lines keep their wall times
  until their record changes. A line written again gets the wall time of the zone the Mac is in then.
- **One activity per start.** A backend holds one activity per start at its precision: `tock.txt` one per minute.
  When two merged activities start within the same minute, as when the phone starts one and switches to another a
  few seconds later, the backend holds the one that takes time at its precision. One that ends within the minute it
  starts (the first of the two, once rule 1 ends it where the second starts) gives way: it leaves the backend with its
  notes and waits outside it, and nothing is journaled for it. Of two that both take time, the one in the backend
  stays and the other waits. The same holds when `tock start` replaces, in `tock.txt`, the line of an activity
  another device started in the same minute: that activity keeps its uid, project and description, and waits. A
  waiting activity is written once its minute is free: by the sync that moves or removes the activity at that minute,
  by the next sync that reads it, and at the latest by the next full one (`tock sync --full`, and `tock tray` once an
  hour), which reads every record.
- **Switching backends.** `state.db` records which backend and path it was built for. Only `tock sync init` switches
  them: while tock runs on another backend or file, as with `--backend` or `-f` for one command, sync leaves both
  alone, records none of its writes, and says so once. After `tock sync init` on the new backend, the bridge matches
  its records to the cache by start time, project and description, and writes no changes for the ones that match.
  Otherwise moving from file to sqlite would reach every device as 100,000 deletions and creations. A `tock.txt` line
  matches by the wall time of the zone the Mac is in, or else by the wall time its start has in the offset it was
  journaled with, which is the zone the line was written in.
- **Which file.** The backend's path is stored absolute, with the links of the part that exists resolved, so a
  relative `file.path` read from another directory, as by the menu bar icon that launchd starts in `/`, is another
  backend, which sync leaves alone, and never the file sync was set up with.
- **Missing data.** A sync never compares a backend whose data file (or timewarrior's data directory) is not there
  while it holds images of it, as when the file was moved or sits on a volume that is not mounted: it stops, writes
  nothing, and says so. `tock sync init` writes the file again from the merged state.
- **Deletion brake.** A sync that would journal the deletion of more than one record of a type, and of more than 50
  of them or more than one in five of those the backend held, journals nothing, writes nothing and says so, as when a
  data file was replaced by an empty or older one, or a sqlite database was deleted and created again empty. Removals
  made through tock in the same process are not counted. No sync can tell an edit made in the backend since the last
  sync from a stale value an older copy brings back, so the way out is the user's choice, and `tock sync init` stops at
  the brake as well, writing nothing:
  - `tock sync init --restore` lets the folder win: it writes the backend again from the merged state, removes the
    records the folder does not hold, and journals nothing it finds there. It first copies each file it rewrites next
    to it, named
    `<name>.before-restore-<YYYYMMDD-HHMMSS>`, and prints their paths: the data file, the timewarrior month files that
    hold what it rewrites, `.tock/notes` and `.tock/reminders.yaml` for the file backends, and the database for sqlite,
    copied with `VACUUM INTO` so the copy is consistent whatever its journal mode. Edits made in the backend since the
    last sync, through tock while syncs stopped at the brake or by hand, stay only in those copies. Restore refuses,
    writing nothing, not even `format.json`, in three cases. When this Mac has not synced this backend with this folder
    yet (a first `init`, another `--dir`, another backend or data path, or no last-sync state left): the backend's
    history may be in no journal, so a plain `tock sync init` has to send it first. When journal files of the folder
    are still downloading: they may hold records the restore would remove; it says to try again once they are down.
    And when the folder holds no activity or reminder of any device, this one's included: it would empty the backend.
  - `tock sync --allow-deletions` lets the backend win: it journals the deletions, and every device loses the records.

  `tock sync init` gets past the brake on its own for a type of records (activities, reminders) the brake stopped
  when none of the backend's records of that type pairs with the last sync (the data file started over or emptied,
  the sqlite database created again): it matches that type's records as after a backend switch, writes the records
  gone back into the backend, and journals the records the backend holds that the folder does not, as new. The
  records it writes back take the folder's values over what the backend kept of them, such as a notes file edited by
  hand since the last sync, so it first copies each existing file it rewrites, as a restore does, named
  `<name>.before-match-<YYYYMMDD-HHMMSS>`, and prints their paths; when a copy fails it stops before writing. It
  decides per type: the other type's records are compared with the last sync as in any sync, so their edits are
  kept, as with `reminders.yaml` left in place while `tock start` started `tock.txt` over. The brake does not stop
  fewer deletions: up to 50 at once, or more while they are at most one in five of the records, go through without a
  prompt and delete those records on every device. Restoring a `tock.txt` from about a week ago, or archiving one
  timewarrior month file, does that.
- **Where it runs.** After every command that changed data and succeeded (a sync that fails there only warns), after
  each tool call of the MCP server, on `tock sync`, and in `tock tray` once a minute and a few seconds after another
  device's journal changes. A sync waits for the others: every tock process takes one lock for a sync or a recorded
  write. With `sync.enabled` off nothing of this is built, and nothing is written outside the backend. A sqlite
  database that synced before loses its change log then: the triggers go, and one row marks the gap, so the next
  sync compares everything.

The rest of this section is how the CLI does what this document asks.

- **Setup.** `tock sync init [--dir <folder>] [--restore]` checks `format.json` or writes it, makes the device ID, and
  runs a first sync that matches the backend's records to the merged ones as after a backend switch: the matches are
  not journaled again, the rest is journaled, and the other devices' records are written into the backend. Every sync
  that matches the backend's records so (a first `init`, another backend or data path, a state rebuilt from the
  journals, `init` past the deletion brake) first copies each existing file it rewrites next to it as
  `<name>.before-match-<YYYYMMDD-HHMMSS>`, `--restore` as `<name>.before-restore-<YYYYMMDD-HHMMSS>`, and prints their
  paths: the folder's values replace what the backend held for a match, such as its notes. A backend without data has
  nothing to copy; when a copy fails the sync stops before it writes. Then it sets `sync.enabled` (and `sync.dir`) in
  the config. Run again on the same folder, it is a full sync, and stops at the deletion brake as one, unless nothing
  of the braked type the backend holds pairs with the last sync (see the deletion brake); with `--restore` it writes
  the backend again from the folder, once this Mac has synced the backend with that folder. On a backend whose data is
  gone it matches as on a new folder. On another folder, the merged state of the old one is dropped and the new one is
  read from the start. Before `init`, `tock sync` stops and says to run it, and so does every command that changed
  data while `sync.enabled` is on. `tock tray` reads `sync.enabled` again before each sync, so turning it off stops
  the menu bar icon's syncs.
- **`state.db`.** `~/.tock/sync/state.db` holds the device ID, `last`, the configured folder, the backend and path
  it was built for, the backend's change token, the cursors, every merged record with one row per field, the image of
  each record as the backend held it after the last sync, the local reminder numbers and the auto-stop marks. Two lock
  files sit next to it. When a new tock changes its layout, the file is rebuilt: the device ID, `last`, the folder and
  the backend and path are kept, and the first sync after it on that backend reads the journals again and matches the
  backend as after a backend switch. A command on another backend or file is left alone as before the rebuild.
  An older tock leaves a file a newer one made alone and says to update. The layout changed with `auto_end` (version
  3), so a menu bar icon still running the tock from before it after an upgrade stops there, instead of writing the
  activities an `auto_end` stops back into the backend as running. A sync that reads the journals again from
  the start compares the whole backend, and journals again, whole, the records the merged state lost with lines of a
  journal that went away or shrank. A record that survives keeps this Mac's later changes all the same: every field
  whose value before the rebuild was this Mac's, and that the rebuilt state holds older or not at all, is journaled
  again with a new clock, so a stop in the lost tail does not come back as a running activity. The changes other
  devices lost are theirs to journal again.
- **Records interrupted runs wrote.** A record a run wrote into the backend before it could save its image is taken
  for that record by the next run, by start, project and description, whether or not that run loaded it, and by the
  start the merged state moved it to when its last-sync state is at the old one. The line is the merged view as that
  run wrote it, which other devices' changes may have moved on since, so it is no edit: nothing of it is journaled
  but an end tock recorded for it since, and the sync writes the merged view over it. Its missing note files are
  written, not journaled as cleared. A reminder without a `uid` gets one stored in the backend before it is journaled.
- **Device ID and clones.** The ID is made at the first sync and kept in `state.db`. A change in the device's own
  journal with a clock later than `last` makes it take a new ID, and `tock sync` says so.
- **What rule 1 gives.** After a merge the CLI journals what rule 1 gives it to write, the `end` of its own
  activities and the `auto_end` of the others, only in a sync that read every segment, with no file waiting for a
  download or failing to read (see [rule 1](#rules-after-the-merge)). The stops of a start are recorded writes like
  the start itself.
- **iCloud.** The CLI asks for files iCloud has not downloaded yet with `brctl download`. Since macOS 14 iCloud Drive
  mostly keeps files without their contents instead of placeholders; those are read like any other file, which
  downloads them. `tock sync status` shows how many files wait.
- **Its own files.** The CLI, the menu bar icon and the MCP server can append to one journal at the same time, so they
  hold a lock on `journal/<own id>/.writer.lock` while they write there. `devices/<own id>.json` names the Mac by its
  host name and the app as `tock <version>`; it is rewritten when that changes, and at most once an hour to move `seen`
  on.

## iOS

The app's bundle ID is `com.kriuchkov.tock`, and its iCloud container is `iCloud.com.kriuchkov.tock`, with its
documents scope public. That makes its folder visible in iCloud Drive on the Mac, where the CLI reads and writes it.
The app watches the folder with `NSMetadataQuery`, reads and writes through `NSFileCoordinator`, keeps the merged
state in SQLite (see [Reading incrementally](#reading-incrementally)) and writes only its own journal. After a refresh
that read every segment it writes what [rule 1](#rules-after-the-merge) gives it: the `end` of the activities it owns
and the `auto_end` of the others. Starting an activity stops the running one as rule 1 says, with `auto_end` when
another device owns it and the new start comes after it.

The iCloud container is not the only place. The app keeps its journal where the user chooses: only on the device, in
the iCloud container, or in another folder picked in Files, such as one that Syncthing, Dropbox or any other Files
provider keeps in sync, or one elsewhere in iCloud Drive. A picked folder is the sync folder itself, with the layout
of [Folder](#folder). The app keeps a bookmark to it, reads and writes it through `NSFileCoordinator` as well, and
watches it with `NSFilePresenter`. A Mac uses the same folder with `tock sync init --dir <folder>`, wherever the
provider keeps it on the Mac.

## Versions

- **`format.json`.** A reader that finds a version it does not know stops syncing and asks for an update. It writes
  nothing to the folder.
- **`v` on each line.** It is the same version. Version 1 can only grow by new optional field paths, which readers
  keep (see above). `auto_end` is one: a reader from before it keeps it, shows the activity as running, and still
  writes the `end` of only its own activities.

## Test vectors

The vectors are Go values in the tests of `internal/core/journal` (`*_vectors_test.go`), which run them.
`make sync-vectors OUT=<dir>` writes them as JSON files, one per vector, into `<dir>/hlc`, `<dir>/merge`,
`<dir>/start`, `<dir>/reopen` and `<dir>/repeat`. tock-ios commits the files generated from a tock release and runs
all of them.

- `hlc/*.json`: `{"name", "valid", "invalid", "order", "last", "steps"}`, each key but `name` optional. `valid`
  and `invalid` are clocks that parse or not; a valid one is written back unchanged. `order` lists `[hlc, dev]`
  pairs in increasing order. `steps` run on a clock that starts at `last` (`""` is the zero clock): `{"op": "new",
  "now", "expect"}` makes a change at `now` (ms since the epoch) and expects its clock; `{"op": "read", "hlc", "now",
  "expect"}` reads a clock and expects the device's latest clock.
- `merge/*.json`: `{"name", "journals": {"<dev>": [line, ...]}, "expect": {"activities", "reminders", "skipped",
  "writes"}}`, `writes` optional. An object line is written as compact JSON and `\n`, a string line exactly as it
  is, so a vector can hold a malformed line or a last line cut off before its `\n`. `expect` lists the visible
  records in `uid` order as `{"uid", "fields"}`, with every merged field as devices show it after the rules, and
  counts the skipped lines. There `end` is the activity's `end` when that holds an instant, otherwise the end the
  one-running rule gives it, otherwise its shown end; it keeps its merged value when there is none. `auto_end` is as merged. `writes` maps device ids to the
  changes each of those devices writes after reading all the journals and applying the rules, in `uid` order, as
  `{"type", "uid", "set"}` (a change without `v`, `hlc` and `dev`, which depend on the device), or `[]` when it writes
  none. Each change sets one field: `{"end": ...}` for an activity the device owns, `{"auto_end": ...}` for any other.
  A device it lists need not have a journal; one without a journal owns no activity, so it writes `auto_end` only. The
  result must be the same whatever order the journals are read in, when they are read twice, and when they are read a
  few bytes at a time. Once every device of `journals` has written what the rules give it to write, from the same
  state, the rules must give no more ends, and the visible records must stay the same but for `auto_end`: an activity
  a device wrote it for shows that value there.
- `start/*.json`: `{"name", "journals", "device", "now", "tz", "start": {"uid", "start"}, "expect"}`. After reading
  `journals` as in a merge vector, device `device` starts an activity with that `uid` and `start` (the text its record
  holds) at `now`, an RFC 3339 instant that may have a fraction of a second, in time zone `tz`. `expect` lists the
  changes the start writes besides the new activity's own, as in the merge vectors' `writes`: the stop of the activity
  that runs as shown after the rules, if any, or `[]`. An `end` written at the current time has whole seconds and the
  offset `tz` has at that instant.
- `reopen/*.json`: `{"name", "journals", "device", "uid", "expect": {"edits", "activities"}}`. After reading
  `journals`, device `device` reopens activity `uid` on purpose. `expect.edits` lists the changes it writes, as in
  `writes`, and `expect.activities` the visible activities once that change is merged, as in a merge vector.
- `repeat/*.json`: `{"name", "tz", "reminder", "from", "to", "now", "expect"}`. `reminder` holds the fields of a
  reminder, read in time zone `tz`. `expect` lists the occurrences on the days from `from` up to `to` (not
  included; a day starts at its first instant, see [Midnight gaps](#repeat-rules)) as `{"due", "status"}`, at `now`
  in local time; `status` is `pending`, `snoozed`, `overdue`, `missed` or `done`, and skipped occurrences are left
  out.

### Interop folders

Besides the vectors, each implementation writes whole sync folders through its own code for the other to read,
each with an `expected.json` of what every device reads from it: the devices, the visible records after the rules,
what rule 1 still gives each device with a journal to write (`end` or `auto_end`, as in the merge vectors' `writes`),
and the activities and occurrences they read as. tock's are written by `make sync-interop OUT=<dir>`
(`internal/services/sync/interop_test.go`: two Macs with the file backend, and a Mac that joins a folder of the app);
tock-ios commits them in `Tests/Interop` and reads them with its `JournalSync`. tock-ios writes its own (a phone and
a tablet, and the phone the mixed folder starts from) into `internal/services/sync/testdata/ios`, which tock reads
with `syncfolder` and `journal`.

## Privacy

The data sits in your iCloud Drive. Apple encrypts it end to end only with Advanced Data Protection turned on.
Nothing else leaves the device.

## Open questions

- **Stops written in turns.** A device writes the ends rule 1 gives from what it has read. Two devices that each
  start an activity while a third one's runs, before they read each other's start, both write that activity's
  `auto_end`, and the later clock wins; rule 1 then stops it at the earlier start while that activity still runs (see
  [rule 1](#rules-after-the-merge)). When the earlier of the two new activities is stopped by an `end` before any
  device has read both starts, it no longer runs and does not cut, so the third activity keeps the later `auto_end`
  and overlaps it. The same
  goes for an activity whose owner reads the second activity's end before it has seen the first one run with it, and
  writes the later end as `end`, which shows over every `auto_end`. Version 1 accepts this: it takes two starts made
  before the devices read each other, and a stop before any device read both.
- **A start after an unwritten stop.** A device that shows two activities of other devices running, the earlier one
  stopped by rule 1 at the later one's start, has not written that stop until a complete sync. A start then stops only
  the later one (see [rule 1](#rules-after-the-merge)), so the earlier one runs until the new start and overlaps the
  later one. Writing the rule's ends with the start would mean writing them from a sync that has not read every
  segment. Version 1 accepts this: it takes an activity started while a sync is incomplete and two other activities
  ran concurrently.
- **Snapshots.** Version 1 has none. On tock's data a snapshot of the merged state is larger than the journals it
  replaces (49 MB against 45 MB), because most records are written once or twice and a snapshot keeps an `hlc` per
  field. If records start being rewritten often, a later version can add snapshots and delete the segments they
  cover.
- **Real time.** Starting a Live Activity on the iPhone when tracking starts on the Mac needs push notifications
  (APNs), so it waits for a relay server in version 2.
