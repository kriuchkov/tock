# Folders tock-ios writes

Sync folders the tock iOS app wrote through its `JournalSync`, acting as its model does
(`Tests/TockStoreTests/Interop/InteropWriteTests.swift` in tock-ios), each with the `expected.json` the app read
from it. `TestInteropSwiftFolders` reads them with `syncfolder` and `journal`, expects the same, and has a Mac with
the file backend join each one. `TestInteropMixedFolder` starts the mixed folder it writes for tock-ios from
`mixed-seed`. `interop_support_test.go` describes the keys of `expected.json`.

- `swift/`: a phone and a tablet over two weeks across a month. Activities with notes, tags and a description in
  other scripts, a switch, a start the tablet moved on the phone's activity, a deletion, reminders with done,
  snoozed, skipped and tracked occurrences, both devices' October compressed, and both cases of owner-only stops:
  the phone's real end at 10:30 that the tablet had not read when it started another activity at 11:00, and an end
  only shown, which the phone still has to write.
- `mixed-seed/`: one phone with an activity that ran, one that runs and a daily reminder.

Regenerate them from a tock-ios checkout with `make interop TOCK=<this checkout>`, which also writes tock's folders
into tock-ios. Times, device IDs and uids are fixed, so a run writes the same bytes. Never edited by hand.
