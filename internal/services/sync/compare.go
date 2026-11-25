package sync

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/go-faster/errors"
	"github.com/google/uuid"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// found is what comparing the backend with its image found about one record: that it differs from
// its image, that it is new, or that it went away.
type found struct {
	ref ref
	// key is the activity's key in the backend.
	key string
	// old is the record's image; nil for a record without one.
	old fields
	// current and written are what the backend holds, in image and in journal form; nil when it went
	// away.
	current  fields
	written  fields
	vanished bool
	// noNotes is set for an activity the backend holds without a notes file, and noTags for one that
	// holds no tags either; notesMoved for one whose notes file is under another key than when its
	// image was read. See keepMergedNotes.
	noNotes    bool
	noTags     bool
	notesMoved bool
}

// keepMergedNotes leaves out of a set the notes and tags read from a notes file that cannot tell what
// the user changed. Notes cleared or changed through tock in this process are a change all the same.
//
//   - An activity without a notes file has not lost its notes: tock clears notes by saving empty ones,
//     so the file is one sync has not written yet (a run interrupted between the line and its notes) or
//     one under another key (a line a hand edit moved). Its clears are left out, and so are the tag
//     clears of a tock.txt line, which holds no tags; timewarrior and todotxt lines hold them.
//   - An activity whose start is the instant its image holds, read in another zone than the image was,
//     has its notes filed under another date and time of day (timewarrior keeps UTC, and notes go by
//     the local start): whatever is there, or not there, is no edit. Its notes and the tags of its
//     notes file are left out.
//
// The image keeps what the backend holds, so the write-back that follows writes the merged notes and
// tags where the activity is now.
func (f found) keepMergedNotes(set fields, scope compareScope) {
	if scope.notes[f.key] {
		return
	}
	for path, value := range set {
		isNotes, isTag := path == fieldNotes, isTagPath(path)
		switch {
		case f.notesMoved && (isNotes || (isTag && (!f.noNotes || f.noTags))):
			delete(set, path)
		case f.noNotes && isNotes && (bytes.Equal(value, jsonNull()) || bytes.Equal(value, jsonString(""))):
			delete(set, path)
		case f.noTags && isTag && bytes.Equal(value, jsonBool(false)):
			delete(set, path)
		}
	}
}

// notesKeyMoved reports whether an activity whose start is the instant its image holds has its notes
// filed under another key than when the image was read: notes go by the date and time of day of the
// start in the zone it is read in, and an image keeps the offset its start was read at.
func (r *run) notesKeyMoved(imageStart json.RawMessage, start time.Time) bool {
	was, ok := parseInstant(imageStart)
	if !ok || !was.Truncate(r.precision).Equal(start.Truncate(r.precision)) {
		return false
	}
	const notesKeyLayout = "2006-01-02 150405"
	return was.Format(notesKeyLayout) != start.Format(notesKeyLayout)
}

// compareScope is the part of the backend a run compares.
type compareScope struct {
	// all compares every activity; spans and keys compare the activities with keys in them, finding
	// the ones that went away too.
	all   bool
	spans []keySpan
	keys  map[string]bool
	// notes are the keys whose sidecar notes are read; notesFrom reads them for activities from then
	// on, and allNotes for every activity.
	notes     map[string]bool
	notesFrom time.Time
	allNotes  bool
	reminders bool
	// moves, stops, removed and removedReminders are what the recorded writes tell; see
	// recordedScope.
	moves            map[string]string
	stops            map[string]stopCause
	removed          map[string]bool
	removedReminders map[string]bool
}

// recordedRemoval reports whether a record found gone was removed through tock in this process.
func (s compareScope) recordedRemoval(f found) bool {
	if f.ref.typ == journal.TypeReminder {
		return s.removedReminders[f.key]
	}
	return s.removed[f.key]
}

// compare takes the backend's token, works out what to compare and compares it.
func (r *run) compare(ctx context.Context) ([]found, compareScope, error) {
	token, err := r.backend.Token(ctx)
	if err != nil {
		return nil, compareScope{}, errors.Wrap(err, "read the backend's change token")
	}
	r.token = token
	if !r.matches(journal.TypeActivity) {
		if err = r.checkData(ctx); err != nil {
			return nil, compareScope{}, err
		}
	}

	recorded := scopeOf(r.taken.writes, r.keys)
	for _, mark := range r.meta.AutoStops {
		recorded.keys[r.keys.key(mark.Start)] = true
	}
	scope := compareScope{
		keys:             recorded.keys,
		notes:            recorded.notes,
		reminders:        recorded.reminders,
		moves:            recorded.moves,
		stops:            recorded.stops,
		removed:          recorded.removed,
		removedReminders: recorded.removedReminders,
	}
	switch {
	case r.opts.full || r.opts.match || r.opts.reset:
		// After the journals were read again from the start, the merged state may lack records this
		// device journaled in lines that are gone: comparing everything sends them again.
		scope.all, scope.allNotes, scope.reminders = true, true, true
	case r.tokenMoved():
		changed, changedErr := r.backend.Changed(ctx, r.meta.Token, token)
		if changedErr != nil {
			return nil, compareScope{}, errors.Wrap(changedErr, "find what changed in the backend")
		}
		scope.all = changed.AllActivities
		for _, span := range changed.Activities {
			scope.spans = append(scope.spans, keySpan{from: span.From, to: span.To})
		}
		scope.reminders = scope.reminders || changed.Reminders
	}
	if len(scope.keys) > findAllAbove {
		scope.all = true
	}
	r.report.Compared = scope.all || len(scope.spans) > 0 || len(scope.keys) > 0 || scope.reminders
	if r.sidecar && !scope.allNotes {
		scope.notesFrom = r.now.Add(-recentNotes)
	}

	out, err := r.compareActivities(ctx, scope)
	if err != nil {
		return nil, compareScope{}, err
	}
	if scope.reminders {
		reminders, remindersErr := r.compareReminders(ctx)
		if remindersErr != nil {
			return nil, compareScope{}, remindersErr
		}
		out = append(out, reminders...)
	}
	return out, scope, nil
}

// checkData stops a run whose backend's data is gone while it holds images of it: a data file moved
// or renamed, a volume not mounted. Compared, every activity would look deleted. A backend that never
// held an activity may start without its file. tock sync init writes a backend whose data is gone again
// from the merged state, as a new one (Service.Init).
func (r *run) checkData(ctx context.Context) error {
	exists, err := r.backend.Exists(ctx)
	if err != nil {
		return errors.Wrap(err, "look for the backend's data")
	}
	if exists {
		return nil
	}
	held, err := r.images.loadAll(ctx, journal.TypeActivity)
	if err != nil {
		return err
	}
	if len(held) == 0 {
		return nil
	}
	return errors.Wrapf(coreErrors.ErrSyncBackendMissing, "%s", r.backend.Identity().Path)
}

// tokenMoved reports whether something changed the backend since the token saved last, besides the
// writes recorded in this process.
func (r *run) tokenMoved() bool {
	switch {
	case r.taken.outside:
		return true
	case r.taken.hasToken:
		return !r.token.Equal(r.taken.token)
	default:
		return !r.token.Equal(r.meta.Token)
	}
}

// entry is an activity read from the backend for a compare.
type entry struct {
	act       models.Activity
	key       string
	notesRead bool
	// noNotes and noTags are what found keeps of them.
	noNotes bool
	noTags  bool
}

// compareActivities compares the activities in scope with their images.
func (r *run) compareActivities(ctx context.Context, scope compareScope) ([]found, error) {
	activities, err := r.scopeActivities(ctx, scope)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(activities))
	for _, a := range activities {
		e := entry{act: a, key: r.keys.key(a.StartTime), notesRead: !r.sidecar}
		if r.sidecar && (scope.allNotes || scope.notes[e.key] || (!scope.notesFrom.IsZero() && !a.StartTime.Before(scope.notesFrom))) {
			if err = r.readNotes(ctx, &e); err != nil {
				return nil, err
			}
		}
		entries = append(entries, e)
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return a.act.StartTime.Compare(b.act.StartTime) })

	if r.matches(journal.TypeActivity) {
		return r.matchActivities(ctx, entries)
	}
	candidates, vanishable, err := r.scopeImages(ctx, scope, entries)
	if err != nil {
		return nil, err
	}
	return r.pairActivities(ctx, entries, candidates, vanishable, scope)
}

// scopeActivities reads the activities in scope from the backend.
func (r *run) scopeActivities(ctx context.Context, scope compareScope) ([]models.Activity, error) {
	if scope.all {
		return r.findAll(ctx)
	}
	var (
		out  []models.Activity
		seen = make(map[string]bool)
	)
	read := func(span keySpan) error {
		activities, err := r.findSpan(ctx, span)
		for _, a := range activities {
			if id := identity(a); !seen[id] {
				seen[id] = true
				out = append(out, a)
			}
		}
		return err
	}
	for _, span := range scope.spans {
		if err := read(span); err != nil {
			return nil, err
		}
	}
	for _, key := range sortedKeys(scope.keys) {
		if span, ok := r.keySpanOf(key); ok {
			if err := read(span); err != nil {
				return nil, err
			}
		}
	}
	if !scope.notesFrom.IsZero() {
		if err := read(keySpan{from: scope.notesFrom}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// scopeImages loads the images to compare with: those in scope, which a record that went away
// leaves behind (vanishable), and those of the activities read.
func (r *run) scopeImages(
	ctx context.Context,
	scope compareScope,
	entries []entry,
) (map[ref]ports.SyncImage, map[ref]bool, error) {
	candidates := make(map[ref]ports.SyncImage)
	vanishable := make(map[ref]bool)
	add := func(imgs []ports.SyncImage, inScope bool) {
		for _, img := range imgs {
			r := ref{typ: img.Type, uid: img.UID}
			candidates[r] = img
			vanishable[r] = vanishable[r] || inScope
		}
	}

	if scope.all || len(scope.spans) > 0 {
		all, err := r.images.loadAll(ctx, journal.TypeActivity)
		if err != nil {
			return nil, nil, err
		}
		for _, img := range all {
			add([]ports.SyncImage{img}, scope.all || r.inSpans(img.Key, scope.spans))
		}
	}
	inKeys, err := r.images.loadKeys(ctx, sortedKeys(scope.keys))
	if err != nil {
		return nil, nil, err
	}
	add(inKeys, true)

	keys := make(map[string]bool, len(entries))
	uids := make([]string, 0, len(entries))
	for _, e := range entries {
		keys[e.key] = true
		if r.keepsUIDs && validUID(e.act.UID) {
			uids = append(uids, e.act.UID)
		}
	}
	atKeys, err := r.images.loadKeys(ctx, sortedKeys(keys))
	if err != nil {
		return nil, nil, err
	}
	add(atKeys, false)
	if err = r.images.loadUIDs(ctx, journal.TypeActivity, uids); err != nil {
		return nil, nil, err
	}
	for _, uid := range uids {
		if img, ok := r.images.get(ref{typ: journal.TypeActivity, uid: uid}); ok {
			add([]ports.SyncImage{img}, false)
		}
	}
	return candidates, vanishable, nil
}

// pairing is the state of pairing the activities read with their images.
type pairing struct {
	entries []entry
	paired  []bool
	used    map[ref]bool
	out     []found
}

// pairActivities pairs the activities read with their images: by uid when the backend keeps them,
// else by key; an image left over that a recorded move or the same project and description points
// at an activity left over is that activity, moved. What is still left over went away or is new.
func (r *run) pairActivities(
	ctx context.Context,
	entries []entry,
	candidates map[ref]ports.SyncImage,
	vanishable map[ref]bool,
	scope compareScope,
) ([]found, error) {
	p := &pairing{entries: entries, paired: make([]bool, len(entries)), used: make(map[ref]bool)}
	if r.keepsUIDs {
		for i, e := range entries {
			if img, ok := candidates[ref{typ: journal.TypeActivity, uid: e.act.UID}]; ok && validUID(e.act.UID) {
				r.pair(p, i, img)
			}
		}
	} else {
		if err := r.findLeftBehind(ctx, p, candidates); err != nil {
			return nil, err
		}
		r.pairByKey(p, candidates)
	}

	vanished := leftOver(candidates, vanishable, p.used)
	for _, img := range vanished {
		target, moved := scope.moves[img.Key]
		for i, e := range entries {
			if p.paired[i] {
				continue
			}
			if (moved && e.key == target) || (!moved && !r.keepsUIDs && sameTitle(img.Fields, e.act)) {
				r.pair(p, i, img)
				break
			}
		}
	}
	if err := r.findMovedOut(ctx, p, candidates, vanishable); err != nil {
		return nil, err
	}
	if err := r.findMovedTo(ctx, p, candidates, vanishable); err != nil {
		return nil, err
	}

	for _, img := range leftOver(candidates, vanishable, p.used) {
		imgRef := ref{typ: journal.TypeActivity, uid: img.UID}
		r.images.drop(imgRef)
		if r.gaveWay(img, nil) {
			// Not gone: it waits outside the backend for its key (slot.go).
			continue
		}
		p.out = append(p.out, found{ref: imgRef, key: img.Key, old: img.Fields, vanished: true})
	}
	r.paired[journal.TypeActivity] += len(p.used)
	adopted := make(map[string]bool, len(p.used))
	for used := range p.used {
		adopted[used.uid] = true
	}
	for i := range p.entries {
		if p.paired[i] {
			continue
		}
		f, err := r.newFound(ctx, p.entries[i], adopted)
		if err != nil {
			return nil, err
		}
		p.out = append(p.out, f)
	}
	return p.out, nil
}

// pairByKey pairs activities and images with the same key; of several images with one key, the one
// with the same project and description comes first.
func (r *run) pairByKey(p *pairing, candidates map[ref]ports.SyncImage) {
	byKey := make(map[string][]ports.SyncImage)
	for _, img := range candidates {
		byKey[img.Key] = append(byKey[img.Key], img)
	}
	for key := range byKey {
		sortImages(byKey[key])
	}
	for i, e := range p.entries {
		if p.paired[i] {
			continue
		}
		var pick *ports.SyncImage
		for j, img := range byKey[e.key] {
			if p.used[ref{typ: journal.TypeActivity, uid: img.UID}] || r.gaveWay(img, &e.act) {
				continue
			}
			if pick == nil || (sameTitle(img.Fields, e.act) && !sameTitle(pick.Fields, e.act)) {
				pick = &byKey[e.key][j]
			}
		}
		if pick != nil {
			r.pair(p, i, *pick)
		}
	}
}

// findLeftBehind pairs an image with the activity at the key the merged state moved its record to
// while the activity the image describes is still at the image's key: a batch that moved it from one
// timewarrior month file to another, and stopped between the two renames, left it in both. Both lines
// are exactly what the run wrote and what it replaced, so the one at the image's key is left behind:
// the write-back removes it, and neither is journaled. It never takes a line another record may hold:
//
//   - only on a backend that keeps no uids and keys by instant, so a key moves only with its record:
//     tock.txt keys by wall time, which every record changes with the Mac's time zone, and a sqlite
//     row keeps its uid;
//   - only for a move that may cross a month in the zone the Mac wrote it in, any zone, as one batch
//     writes into two files then;
//   - only when no other image is at the key moved to: a line there may be that image's record, alike
//     in title and times.
func (r *run) findLeftBehind(ctx context.Context, p *pairing, candidates map[ref]ports.SyncImage) error {
	if r.wall || r.keepsUIDs {
		return nil
	}
	imgs := slices.Collect(maps.Values(candidates))
	sortImages(imgs)
	for _, img := range imgs {
		record, known := r.store.get(ref{typ: journal.TypeActivity, uid: img.UID})
		merged, visible := record.Activity()
		was, parsed := parseInstant(img.Fields[fieldStart])
		if !known || !visible || !parsed {
			continue
		}
		merged = r.normalize(merged)
		key := r.keys.key(merged.StartTime)
		if key == img.Key || !otherMonth(was, merged.StartTime) {
			continue
		}
		old := slices.IndexFunc(p.entries, func(e entry) bool {
			return e.key == img.Key && sameTitle(img.Fields, e.act) && r.sameTimes(img.Fields, e.act)
		})
		if old < 0 || p.paired[old] {
			continue
		}
		held, err := r.images.loadKeys(ctx, []string{key})
		if err != nil {
			return err
		}
		if slices.ContainsFunc(held, func(h ports.SyncImage) bool { return h.UID != img.UID }) {
			continue
		}
		moved, err := r.unpairedAt(ctx, p, key, img, merged)
		if err != nil {
			return err
		}
		if moved < 0 || !r.sameTimes(activityFields(merged, imageForm(r.precision)), p.entries[moved].act) ||
			p.entries[moved].act.Project != merged.Project || p.entries[moved].act.Description != merged.Description {
			continue
		}
		p.paired[old] = true
		r.extra = append(r.extra, p.entries[old].act)
		r.pair(p, moved, img)
	}
	return nil
}

// otherMonth reports whether two starts may fall in different months in some zone: tock files an
// activity under the month of its start in the zone the Mac was in when it wrote it, which may be any
// from UTC-12 to UTC+14.
func otherMonth(a, b time.Time) bool {
	const (
		month    = "2006-01"
		earliest = -12 * time.Hour
		latest   = 14 * time.Hour
	)
	if b.Before(a) {
		a, b = b, a
	}
	return a.Add(earliest).UTC().Format(month) != b.Add(latest).UTC().Format(month)
}

// sameTimes reports whether an activity's start and end are those of image fields.
func (r *run) sameTimes(image fields, a models.Activity) bool {
	current := activityFields(a, imageForm(r.precision))
	return sameTime(image[fieldStart], current[fieldStart], r.wall) && sameTime(image[fieldEnd], current[fieldEnd], r.wall)
}

// findMovedOut pairs, for a backend that keeps uids, an image left over with the activity that
// holds its uid outside the scope: it moved there.
func (r *run) findMovedOut(ctx context.Context, p *pairing, candidates map[ref]ports.SyncImage, vanishable map[ref]bool) error {
	vanished := leftOver(candidates, vanishable, p.used)
	if !r.keepsUIDs || len(vanished) == 0 {
		return nil
	}
	all, err := r.findAll(ctx)
	if err != nil {
		return err
	}
	byUID := make(map[string]models.Activity, len(all))
	for _, a := range all {
		byUID[a.UID] = a
	}
	for _, img := range vanished {
		a, ok := byUID[img.UID]
		if !ok {
			continue
		}
		p.entries = append(p.entries, entry{act: a, key: r.keys.key(a.StartTime), notesRead: true})
		p.paired = append(p.paired, false)
		r.pair(p, len(p.entries)-1, img)
	}
	return nil
}

// findMovedTo pairs, on a backend that keeps no uids, an image left over with the activity at the key
// the merged state moved its record to: what a run that wrote another device's move into the backend,
// and stopped before it saved, left behind, which the titles do not pair when the move renamed it too.
// The record is not gone, and the activity there is not new.
func (r *run) findMovedTo(ctx context.Context, p *pairing, candidates map[ref]ports.SyncImage, vanishable map[ref]bool) error {
	if r.keepsUIDs {
		return nil
	}
	for _, img := range leftOver(candidates, vanishable, p.used) {
		record, known := r.store.get(ref{typ: journal.TypeActivity, uid: img.UID})
		merged, visible := record.Activity()
		if !known || !visible {
			continue
		}
		merged = r.normalize(merged)
		key := r.keys.key(merged.StartTime)
		if key == img.Key {
			continue
		}
		i, err := r.unpairedAt(ctx, p, key, img, merged)
		if err != nil {
			return err
		}
		if i >= 0 {
			r.pair(p, i, img)
		}
	}
	return nil
}

// unpairedAt returns the index in p of an activity at key with the title of an image or of its merged
// record that no other image holds, reading the key from the backend when the run did not compare it;
// -1 when there is none.
func (r *run) unpairedAt(ctx context.Context, p *pairing, key string, img ports.SyncImage, merged models.Activity) (int, error) {
	fits := func(a models.Activity) bool {
		return sameTitle(img.Fields, a) || (a.Project == merged.Project && a.Description == merged.Description)
	}
	compared := make(map[string]bool, len(p.entries))
	for i, e := range p.entries {
		compared[identity(e.act)] = true
		if !p.paired[i] && e.key == key && fits(e.act) {
			return i, nil
		}
	}
	if !r.index.covered(key) {
		span, ok := r.keySpanOf(key)
		if !ok {
			return -1, nil
		}
		if _, err := r.findSpan(ctx, span); err != nil {
			return -1, err
		}
	}
	held, err := r.images.loadKeys(ctx, []string{key})
	if err != nil {
		return -1, err
	}
	for _, a := range r.index.at(key) {
		if compared[identity(a)] || !fits(a) ||
			slices.ContainsFunc(held, func(h ports.SyncImage) bool { return h.UID != img.UID && sameTitle(h.Fields, a) }) {
			continue
		}
		p.entries = append(p.entries, entry{act: a, key: key, notesRead: !r.sidecar})
		p.paired = append(p.paired, false)
		return len(p.entries) - 1, nil
	}
	return -1, nil
}

// pair records that an activity read is the record of an image.
func (r *run) pair(p *pairing, i int, img ports.SyncImage) {
	e := p.entries[i]
	imgRef := ref{typ: journal.TypeActivity, uid: img.UID}
	p.paired[i] = true
	p.used[imgRef] = true

	current := activityFields(e.act, imageForm(r.precision))
	written := activityFields(e.act, journalForm())
	notesMoved := r.sidecar && e.notesRead && r.notesKeyMoved(img.Fields[fieldStart], e.act.StartTime)
	image := current.clone()
	for _, path := range []string{fieldStart, fieldEnd} {
		// A time that reads as the image's keeps the image's value: the same wall time for a wall-clock
		// backend read in another zone or in the repeated hour, the same instant for the others.
		if old, ok := img.Fields[path]; ok && sameTime(old, current[path], r.wall) {
			current[path] = old
			if !notesMoved {
				image[path] = old
			}
		}
	}
	if !e.notesRead {
		// The notes were not read: the image's notes and tags stand.
		keepNotes(current, img.Fields)
		keepNotes(written, img.Fields)
		keepNotes(image, img.Fields)
	}
	// The notes were read under the start as it reads in this zone: once they are, the image takes its
	// offset, so the next run reads a notes file added there as an edit, not as one filed in another zone.
	r.images.set(ports.SyncImage{Type: journal.TypeActivity, UID: img.UID, Key: e.key, Fields: image})
	p.out = append(p.out, found{
		ref: imgRef, key: e.key, old: img.Fields, current: current, written: written,
		noNotes: e.noNotes, noTags: e.noTags, notesMoved: notesMoved,
	})
}

// newFound describes an activity read that has no image: the record that holds its uid when the
// backend keeps uids, else a record of the merged state at the same key with the same project and
// description and no image (what an interrupted run wrote), else a new record.
func (r *run) newFound(ctx context.Context, e entry, adopted map[string]bool) (found, error) {
	if !e.notesRead {
		if err := r.readNotes(ctx, &e); err != nil {
			return found{}, err
		}
	}
	uid := ""
	switch {
	case r.keepsUIDs && validUID(e.act.UID) && !adopted[e.act.UID]:
		uid = e.act.UID
	case !r.keepsUIDs:
		var err error
		if uid, err = r.adopt(ctx, e, adopted); err != nil {
			return found{}, err
		}
	}
	recRef := ref{typ: journal.TypeActivity, uid: uid}
	current := activityFields(e.act, imageForm(r.precision))
	if uid == "" {
		var err error
		if uid, err = newUID(); err != nil {
			return found{}, err
		}
		recRef.uid = uid
		if r.keepsUIDs {
			r.force[recRef] = true
		}
	} else if record, known := r.store.get(recRef); known {
		r.keepWrittenTimes(current, record)
	}
	adopted[uid] = true

	r.images.set(ports.SyncImage{Type: journal.TypeActivity, UID: uid, Key: e.key, Fields: current})
	return found{
		ref: recRef, key: e.key, current: current, written: activityFields(e.act, journalForm()), noNotes: e.noNotes, noTags: e.noTags,
	}, nil
}

// adopt returns the uid of a merged record at the activity's key with the same project and
// description, as the backend keeps them, that has no image, or "": what a run interrupted before it
// saved wrote into the backend. It looks at the records the run holds and at those state.db holds with
// a start at the key, which a run that compares only what changed has not loaded: first at the key
// in the zone the Mac is in, then, for a wall-clock backend, at the wall time of the zone each start
// was journaled in.
func (r *run) adopt(ctx context.Context, e entry, adopted map[string]bool) (string, error) {
	if err := r.loadHeld(ctx, e.key); err != nil {
		return "", err
	}
	for _, held := range [][]string{r.held[e.key], r.heldWritten[e.key]} {
		uid, err := r.adoptable(ctx, e, held, adopted)
		if uid != "" || err != nil {
			return uid, err
		}
	}
	return "", nil
}

// adoptable returns the first of the merged records held with the activity's project and description
// and no image, or "".
func (r *run) adoptable(ctx context.Context, e entry, held []string, adopted map[string]bool) (string, error) {
	var candidates []string
	for _, uid := range held {
		record, _ := r.store.get(ref{typ: journal.TypeActivity, uid: uid})
		a, ok := record.Activity()
		if !ok || adopted[uid] {
			continue
		}
		if n := r.normalize(a); n.Project == e.act.Project && n.Description == e.act.Description {
			candidates = append(candidates, uid)
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	if err := r.images.loadUIDs(ctx, journal.TypeActivity, candidates); err != nil {
		return "", err
	}
	for _, uid := range candidates {
		if _, ok := r.images.get(ref{typ: journal.TypeActivity, uid: uid}); !ok {
			return uid, nil
		}
	}
	return "", nil
}

// loadHeld indexes the merged activities the run holds by key, once a run, and loads those state.db
// holds with a start near key: within an hour of it, or for a wall-clock backend as far as two zones
// lie apart, since a line's wall time may be that of another zone.
func (r *run) loadHeld(ctx context.Context, key string) error {
	if r.held == nil {
		r.held, r.heldWritten, r.heldKeys = make(map[string][]string), make(map[string][]string), make(map[string]bool)
		for _, recRef := range r.store.refs(journal.TypeActivity) {
			r.indexHeld(recRef)
		}
	}
	if r.store.complete || r.heldKeys[key] {
		return nil
	}
	r.heldKeys[key] = true
	span, ok := r.keySpanOf(key)
	if !ok {
		return nil
	}
	slack := max(r.keys.slack(), r.keys.zoneSlack())
	loaded, err := r.store.loadStartingIn(ctx, ports.SyncSpan{From: span.from.Add(-slack), To: span.to.Add(slack)})
	if err != nil {
		return err
	}
	for _, recRef := range loaded {
		r.indexHeld(recRef)
	}
	return nil
}

func (r *run) indexHeld(recRef ref) {
	record, _ := r.store.get(recRef)
	if a, ok := record.Activity(); ok {
		key := r.keys.key(a.StartTime)
		r.held[key] = append(r.held[key], recRef.uid)
		if written := r.keys.writtenKey(a.StartTime); written != "" {
			r.heldWritten[written] = append(r.heldWritten[written], recRef.uid)
		}
	}
}

// gaveWay reports whether an image belongs to an activity tock stopped because another one started in
// the same step of the backend's precision, with another project or description, which then holds the
// key: the file backend's save of the new one replaced the old one's line. That record is not gone and
// not renamed; it waits for its key (slot.go). e is the activity read at the key, nil for any.
func (r *run) gaveWay(img ports.SyncImage, e *models.Activity) bool {
	if e != nil && sameTitle(img.Fields, *e) {
		return false
	}
	if e == nil && !slices.ContainsFunc(r.index.at(img.Key), func(a models.Activity) bool { return !sameTitle(img.Fields, a) }) {
		return false
	}
	for _, mark := range r.meta.AutoStops {
		if r.keys.key(mark.Start) == img.Key &&
			sameTitle(img.Fields, models.Activity{Project: mark.Project, Description: mark.Description}) {
			return true
		}
	}
	return false
}

// matchActivities matches, after a backend switch or on a new folder, the backend's activities to
// the merged ones: by uid when the backend keeps them, else by start, project and description. A
// wall-clock backend holds a line at the wall time of the zone it was written in, so an activity left
// over is then matched by the wall time each merged start has in the zone it was journaled in. A
// match journals nothing; the merged values are written back where they differ. The rest are new.
func (r *run) matchActivities(ctx context.Context, entries []entry) ([]found, error) {
	byTitle, byWritten := make(map[string][]string), make(map[string][]string)
	for _, recRef := range r.store.refs(journal.TypeActivity) {
		record, _ := r.store.get(recRef)
		if a, ok := record.Activity(); ok {
			n := r.normalize(a)
			t := titleKey(r.keys.key(a.StartTime), n.Project, n.Description)
			byTitle[t] = append(byTitle[t], recRef.uid)
			if written := r.keys.writtenKey(a.StartTime); written != "" {
				t = titleKey(written, n.Project, n.Description)
				byWritten[t] = append(byWritten[t], recRef.uid)
			}
		}
	}
	old, err := r.images.loadAll(ctx, journal.TypeActivity)
	if err != nil {
		return nil, err
	}

	// Matches in the zone the Mac is in come first, so a line never takes a record another line holds
	// at its wall time here.
	used := make(map[string]bool)
	matched := make([]string, len(entries))
	for _, index := range []map[string][]string{byTitle, byWritten} {
		for i, e := range entries {
			if matched[i] == "" {
				matched[i] = r.matchUID(e, index, used)
				used[matched[i]] = matched[i] != ""
			}
		}
	}
	var out []found
	for i, e := range entries {
		uid := matched[i]
		if uid == "" && r.opts.restore {
			// No merged record holds it: a restore removes it from the backend, journaling nothing.
			r.extra = append(r.extra, e.act)
			continue
		}
		if uid == "" {
			f, newErr := r.newFound(ctx, e, used)
			if newErr != nil {
				return nil, newErr
			}
			out = append(out, f)
			continue
		}
		recRef := ref{typ: journal.TypeActivity, uid: uid}
		if r.keepsUIDs && e.act.UID != uid {
			r.force[recRef] = true
		}
		current := activityFields(e.act, imageForm(r.precision))
		record, _ := r.store.get(recRef)
		r.keepWrittenTimes(current, record)
		r.images.set(ports.SyncImage{Type: journal.TypeActivity, UID: uid, Key: e.key, Fields: current})
	}
	for _, img := range old {
		if !used[img.UID] {
			r.images.drop(ref{typ: journal.TypeActivity, uid: img.UID})
		}
	}
	return out, nil
}

// matchUID returns the merged record an activity read matches in an index by title, or "".
func (r *run) matchUID(e entry, byTitle map[string][]string, used map[string]bool) string {
	if r.keepsUIDs && validUID(e.act.UID) && !used[e.act.UID] {
		if _, ok := r.store.get(ref{typ: journal.TypeActivity, uid: e.act.UID}); ok {
			return e.act.UID
		}
	}
	for _, uid := range byTitle[titleKey(e.key, e.act.Project, e.act.Description)] {
		if !used[uid] {
			return uid
		}
	}
	return ""
}

// compareReminders compares every reminder with its image. After a backend switch or on a new
// folder, it matches them to the merged ones by uid, then by title and due date, as for activities.
func (r *run) compareReminders(ctx context.Context) ([]found, error) {
	list, err := r.cfg.Reminders.List(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "read the backend's reminders")
	}
	slices.SortFunc(list, func(a, b models.Reminder) int { return cmp.Compare(a.ID, b.ID) })
	old, err := r.images.loadAll(ctx, journal.TypeReminder)
	if err != nil {
		return nil, err
	}
	byUID := make(map[string]ports.SyncImage, len(old))
	for _, img := range old {
		byUID[img.UID] = img
	}

	byTitle := r.remindersByTitle()
	seen := make(map[string]bool, len(list))
	var out []found
	for _, reminder := range list {
		uid, matched, uidErr := r.reminderUID(reminder, seen, byTitle)
		if uidErr != nil {
			return nil, uidErr
		}
		if r.opts.restore && !matched {
			// No merged record holds it: a restore removes it from the backend, journaling nothing.
			r.extraReminders = append(r.extraReminders, reminder.ID)
			continue
		}
		seen[uid] = true
		recRef := ref{typ: journal.TypeReminder, uid: uid}
		if !matched && uid != reminder.UID {
			// A new uid is stored before it is journaled, so a run interrupted in between does not make
			// another one for the same reminder.
			if reminder, err = r.storeReminderUID(ctx, reminder, uid); err != nil {
				return nil, err
			}
			delete(r.force, recRef)
		}
		current := reminderFields(reminder, imageForm(r.precision))
		key := strconv.Itoa(reminder.ID)
		if err = r.images.setReminderNumber(ctx, uid, reminder.ID); err != nil {
			return nil, err
		}
		r.images.set(ports.SyncImage{Type: journal.TypeReminder, UID: uid, Key: key, Fields: current})
		if matched {
			continue
		}
		f := found{ref: recRef, key: key, current: current, written: reminderFields(reminder, journalForm())}
		if img, ok := byUID[uid]; ok {
			f.old = img.Fields
			r.paired[journal.TypeReminder]++
		}
		out = append(out, f)
	}

	for _, img := range old {
		if seen[img.UID] {
			continue
		}
		imgRef := ref{typ: journal.TypeReminder, uid: img.UID}
		if !r.matches(journal.TypeReminder) {
			out = append(out, found{ref: imgRef, key: img.Key, old: img.Fields, vanished: true})
		}
		r.images.drop(imgRef)
	}
	return out, nil
}

// remindersByTitle indexes the visible merged reminders by title and due date, for matching.
func (r *run) remindersByTitle() map[string][]string {
	byTitle := make(map[string][]string)
	if !r.matches(journal.TypeReminder) {
		return byTitle
	}
	for _, recRef := range r.store.refs(journal.TypeReminder) {
		record, _ := r.store.get(recRef)
		if reminder, ok := record.Reminder(); ok {
			t := reminderTitleKey(reminder)
			byTitle[t] = append(byTitle[t], recRef.uid)
		}
	}
	return byTitle
}

// reminderUID returns the uid of a reminder read from the backend and whether it matched a merged
// reminder after a backend switch. A reminder without a valid uid, or with one another reminder
// has, gets a new one, which the write into the backend stores.
func (r *run) reminderUID(reminder models.Reminder, seen map[string]bool, byTitle map[string][]string) (string, bool, error) {
	uid := reminder.UID
	if r.matches(journal.TypeReminder) {
		if _, ok := r.store.get(ref{typ: journal.TypeReminder, uid: uid}); ok && validUID(uid) && !seen[uid] {
			return uid, true, nil
		}
		for _, candidate := range byTitle[reminderTitleKey(reminder)] {
			if !seen[candidate] {
				r.force[ref{typ: journal.TypeReminder, uid: candidate}] = true
				return candidate, true, nil
			}
		}
	}
	if validUID(uid) && !seen[uid] {
		return uid, false, nil
	}
	uid, err := newUID()
	if err != nil {
		return "", false, err
	}
	r.force[ref{typ: journal.TypeReminder, uid: uid}] = true
	return uid, false, nil
}

// storeReminderUID saves a reminder read from the backend with a new uid.
func (r *run) storeReminderUID(ctx context.Context, reminder models.Reminder, uid string) (models.Reminder, error) {
	reminder.UID = uid
	var saved *models.Reminder
	if err := r.backendWrite(ctx, func() error {
		var saveErr error
		saved, saveErr = r.cfg.Reminders.Save(ctx, reminder)
		return saveErr
	}); err != nil {
		return models.Reminder{}, errors.Wrapf(err, "store the uid of reminder #%d", reminder.ID)
	}
	if saved != nil {
		return *saved, nil
	}
	return reminder, nil
}

// readNotes reads an activity's sidecar notes and tags the way the activity service does: stored
// ones win over what the activity carries. It notes an activity without a notes file, and one that has
// no tags either: only on a backend whose lines do not hold tags (tock.txt) can a line without a notes
// file not tell whether its activity has any. A notes store that cannot tell a missing file from empty
// notes counts empty notes as none.
func (r *run) readNotes(ctx context.Context, e *entry) error {
	notes, tags, found, err := r.lookupNotes(ctx, e.act)
	if err != nil {
		return errors.Wrapf(err, "read the notes of the activity at %s", e.key)
	}
	e.noNotes, e.noTags = !found, !found && !r.lineTags && len(e.act.Tags) == 0
	if notes != "" {
		e.act.Notes = notes
	}
	if len(tags) > 0 {
		e.act.Tags = tags
	}
	e.notesRead = true
	return nil
}

// lookupNotes reads the notes file of an activity, and whether there is one.
func (r *run) lookupNotes(ctx context.Context, a models.Activity) (string, []string, bool, error) {
	if lookup, ok := r.cfg.Notes.(ports.NotesLookup); ok {
		return lookup.Lookup(ctx, a.ID(), a.StartTime)
	}
	notes, tags, err := r.cfg.Notes.Get(ctx, a.ID(), a.StartTime)
	return notes, tags, notes != "" || len(tags) > 0, err
}

// keepNotes replaces the notes and tags of to with those of from.
func keepNotes(to, from fields) {
	for path := range to {
		if isTagPath(path) {
			delete(to, path)
		}
	}
	for path, value := range from {
		if path == fieldNotes || isTagPath(path) {
			to[path] = value
		}
	}
}

func isTagPath(path string) bool {
	return len(path) > len(tagPrefix) && path[:len(tagPrefix)] == tagPrefix
}

// leftOver returns the vanishable images not paired, by key and uid.
func leftOver(candidates map[ref]ports.SyncImage, vanishable, used map[ref]bool) []ports.SyncImage {
	var out []ports.SyncImage
	for r, img := range candidates {
		if vanishable[r] && !used[r] {
			out = append(out, img)
		}
	}
	sortImages(out)
	return out
}

// sameTitle reports whether an image has the activity's project and description.
func sameTitle(image fields, a models.Activity) bool {
	return bytes.Equal(image[fieldProject], jsonString(a.Project)) && bytes.Equal(image[fieldDescription], jsonString(a.Description))
}

func titleKey(key, project, description string) string {
	return key + "\x00" + project + "\x00" + description
}

func reminderTitleKey(reminder models.Reminder) string {
	f := reminderFields(reminder, imageForm(time.Second))
	return string(f[fieldTitle]) + "\x00" + string(f[fieldDue])
}

func (r *run) inSpans(key string, spans []keySpan) bool {
	t, ok := r.keys.parse(key)
	if !ok {
		return false
	}
	for _, span := range spans {
		if span.holds(t) {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

// newUID makes a record uid: a lowercase UUIDv7.
func newUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", errors.Wrap(err, "make a record uid")
	}
	return id.String(), nil
}

// validUID reports whether s is a uid a journal line can carry: a lowercase UUIDv7.
func validUID(s string) bool {
	id, err := uuid.Parse(s)
	return err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == s
}
