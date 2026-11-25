package sync

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// writePlan is one record to write into the backend.
type writePlan struct {
	ref    ref
	img    ports.SyncImage
	hasImg bool
	// remove is set for a record a merged tombstone deletes; the rest are saved as view says.
	remove   bool
	activity models.Activity
	reminder models.Reminder
	view     fields
}

// writeBack writes into the backend every record the run holds whose merged value, with the ends
// rule 1 gives, differs from its image: other devices' changes, and the ends this device only shows.
// It removes a record only when the merged state deletes it, and never writes over an activity it
// cannot tell apart from another one at the same key.
func (r *run) writeBack(ctx context.Context) error {
	if err := r.writeActivities(ctx); err != nil {
		return err
	}
	return r.writeReminders(ctx)
}

// loadImagesOf loads the images of the records of one type the run holds.
func (r *run) loadImagesOf(ctx context.Context, typ journal.RecordType) ([]ref, error) {
	refs := r.store.refs(typ)
	if r.store.complete {
		_, err := r.images.loadAll(ctx, typ)
		return refs, err
	}
	uids := make([]string, len(refs))
	for i, recRef := range refs {
		uids[i] = recRef.uid
	}
	return refs, r.images.loadUIDs(ctx, typ, uids)
}

func (r *run) writeActivities(ctx context.Context) error {
	plans, keys, err := r.activityPlans(ctx)
	if err != nil {
		return err
	}
	for _, a := range r.extra {
		if err = r.removeActivity(ctx, a); err != nil {
			return err
		}
	}
	if len(plans) == 0 {
		return r.flushLines(ctx)
	}
	if err = r.ensureKeys(ctx, keys); err != nil {
		return err
	}
	// Removals free keys first, and an activity empty at the backend's precision comes after the
	// ones that keep time there. A write that waited for a key gets one more try once the others
	// were written.
	slices.SortStableFunc(plans, func(a, b writePlan) int { return cmp.Compare(r.writeOrder(a), r.writeOrder(b)) })
	var waiting []writePlan
	for _, plan := range plans {
		wrote, writeErr := r.writeActivity(ctx, plan)
		if writeErr != nil {
			return writeErr
		}
		if !wrote {
			waiting = append(waiting, plan)
		}
	}
	for _, plan := range waiting {
		if _, err = r.writeActivity(ctx, plan); err != nil {
			return err
		}
	}
	return r.flushLines(ctx)
}

// activityPlans returns the writes of the merged activities the backend holds otherwise than the
// images say, and the keys they touch.
func (r *run) activityPlans(ctx context.Context) ([]writePlan, map[string]bool, error) {
	refs, err := r.loadImagesOf(ctx, journal.TypeActivity)
	if err != nil {
		return nil, nil, err
	}
	var plans []writePlan
	keys := make(map[string]bool)
	for _, recRef := range refs {
		record, _ := r.store.get(recRef)
		img, hasImg := r.images.get(recRef)
		activity, visible := r.shown(record)
		if !visible {
			if hasImg && isDeleted(record) {
				plans = append(plans, writePlan{ref: recRef, img: img, hasImg: true, remove: true})
				keys[img.Key] = true
			}
			continue
		}
		view := activityFields(r.normalize(activity), imageForm(r.precision))
		if hasImg && sameActivity(img.Fields, view) && !r.force[recRef] {
			continue
		}
		plans = append(plans, writePlan{ref: recRef, img: img, hasImg: hasImg, activity: activity, view: view})
		keys[r.keys.key(activity.StartTime)] = true
		if hasImg {
			keys[img.Key] = true
		}
	}
	return plans, keys, nil
}

// writeOrder sorts the writes into the backend: removals, then activities, then activities empty at
// the backend's precision.
func (r *run) writeOrder(plan writePlan) int {
	switch {
	case plan.remove:
		return 0
	case emptyAt(plan.activity, r.precision):
		return 2
	default:
		return 1
	}
}

// writeActivity removes, saves or moves one activity in the backend. It reports false when the
// record waits because another activity holds its key.
func (r *run) writeActivity(ctx context.Context, plan writePlan) (bool, error) {
	var (
		current    models.Activity
		hasCurrent bool
	)
	// The image as the run holds it now: one dropped since the plan was made no longer names an
	// activity in the backend.
	plan.img, plan.hasImg = r.images.get(plan.ref)
	if plan.hasImg {
		current, hasCurrent = r.backendActivity(plan.img, plan.ref)
	}
	if hasCurrent && r.ambiguous(current) {
		// A save or a removal by start would hit the other activities at the key too: the record waits,
		// its image kept.
		return plan.remove, nil
	}
	if plan.remove {
		if !plan.hasImg {
			return true, nil
		}
		r.images.drop(plan.ref)
		// Only the activity the image describes is removed, never another one at its key.
		if !hasCurrent || (!r.keepsUIDs && !sameTitle(plan.img.Fields, current)) {
			return true, nil
		}
		return true, r.removeActivity(ctx, current)
	}

	// While the merged times are the instants of the image, the backend's times stay as they are: a
	// wall-clock backend keeps the wall time an activity was written at, whatever the zone is now.
	keep := hasCurrent && sameTime(plan.img.Fields[fieldStart], plan.view[fieldStart], false) &&
		sameTime(plan.img.Fields[fieldEnd], plan.view[fieldEnd], false)
	target := r.keys.key(plan.activity.StartTime)
	if keep {
		target = r.keys.key(current.StartTime)
	}
	moving := !hasCurrent || r.keys.key(current.StartTime) != target
	if moving && r.blocked(target, plan.ref, current, hasCurrent) {
		// Another activity holds the key: writing would replace it. Unless that one is empty at the
		// backend's precision and this one is not, the record waits.
		freed, err := r.makeRoom(ctx, target, plan, current, hasCurrent)
		if err != nil || !freed {
			return false, err
		}
	}

	saved := r.savedActivity(plan, current, keep, moving)
	var old *models.Activity
	if hasCurrent {
		old = &current
		r.index.remove(current)
	}
	if err := r.replaceActivity(ctx, old, &saved); err != nil {
		return false, err
	}
	r.index.add(r.backend.Normalize(saved))
	if r.sidecar {
		if err := r.writeNotes(ctx, plan, saved, current, hasCurrent && moving); err != nil {
			return false, err
		}
	}
	image := plan.view.clone()
	if keep {
		times := plan.img.Fields
		if r.sidecar && !r.wall {
			// The notes were written under the start as it reads in this zone: the image keeps its
			// offset, so a run in another zone tells the notes filed here from an edit.
			times = activityFields(current, imageForm(r.precision))
		}
		image[fieldStart], image[fieldEnd] = times[fieldStart], times[fieldEnd]
	}
	r.images.set(ports.SyncImage{Type: journal.TypeActivity, UID: plan.ref.uid, Key: target, Fields: image})
	r.report.Applied++
	return true, nil
}

// savedActivity is the merged activity of a plan as it is saved: with the backend's times when keep
// is set, else with the merged ones at the backend's precision in local time, the start staying the
// backend's while the key does not move.
func (r *run) savedActivity(plan writePlan, current models.Activity, keep, moving bool) models.Activity {
	saved := plan.activity
	saved.UID = ""
	if r.keepsUIDs {
		saved.UID = plan.ref.uid
	}
	switch {
	case keep:
		saved.StartTime, saved.EndTime = current.StartTime, copyActivity(&current).EndTime
	case moving:
		saved.StartTime = saved.StartTime.Truncate(r.precision).In(time.Local)
	default:
		saved.StartTime = current.StartTime
	}
	if !keep && saved.EndTime != nil {
		end := saved.EndTime.Truncate(r.precision).In(time.Local)
		saved.EndTime = &end
	}
	saved.Tags = normalTags(saved.Tags)
	return saved
}

// ambiguous reports whether a write of an activity would hit another one too: without batches a save
// or a removal addresses an activity of a backend without uids by its start alone.
func (r *run) ambiguous(current models.Activity) bool {
	return !r.lines.batched() && !r.keepsUIDs && len(r.index.at(r.keys.key(current.StartTime))) > 1
}

// normalize returns a merged activity in local time as the backend gives it back once saved.
func (r *run) normalize(a models.Activity) models.Activity {
	a.StartTime = a.StartTime.In(time.Local)
	if a.EndTime != nil {
		end := a.EndTime.In(time.Local)
		a.EndTime = &end
	}
	return r.backend.Normalize(a)
}

// removeActivity removes an activity a merged tombstone deletes, with its sidecar notes unless another
// activity at its key shares them.
func (r *run) removeActivity(ctx context.Context, current models.Activity) error {
	if err := r.replaceActivity(ctx, &current, nil); err != nil {
		return err
	}
	r.index.remove(current)
	if r.sidecar && len(r.index.at(r.keys.key(current.StartTime))) == 0 {
		if err := r.afterLines(ctx, func(ctx context.Context) error {
			return wrap(r.cfg.Notes.Delete(ctx, current.ID(), current.StartTime), "remove the notes of a removed activity")
		}); err != nil {
			return err
		}
	}
	r.report.Applied++
	return nil
}

// writeNotes writes the sidecar notes and tags of an activity saved, and removes those left at the
// start it moved from. Notes another activity at the same key shares are never removed. A notes file
// that holds other notes or tags than the image, edited by hand since a run read it, is left as it is:
// the next run that reads it journals the edit.
func (r *run) writeNotes(ctx context.Context, plan writePlan, saved, moved models.Activity, wasMoved bool) error {
	notes := strings.TrimSpace(saved.Notes)
	changed := !plan.hasImg || wasMoved || !sameNotes(plan.img.Fields, plan.view)
	shared := len(r.index.at(r.keys.key(saved.StartTime))) > 1
	unedited := func(ctx context.Context) (bool, error) {
		if !plan.hasImg || wasMoved {
			return true, nil
		}
		return r.notesAsImaged(ctx, moved, plan.img.Fields)
	}
	var writes []func(ctx context.Context) error
	switch {
	case !changed:
	case notes == "" && len(saved.Tags) == 0:
		if plan.hasImg && !shared {
			writes = append(writes, func(ctx context.Context) error {
				if ok, err := unedited(ctx); !ok || err != nil {
					return err
				}
				return wrap(r.cfg.Notes.Delete(ctx, saved.ID(), saved.StartTime), "remove the notes of an activity")
			})
		}
	default:
		writes = append(writes, func(ctx context.Context) error {
			if ok, err := unedited(ctx); !ok || err != nil {
				return err
			}
			return wrap(r.cfg.Notes.Save(ctx, saved.ID(), saved.StartTime, notes, saved.Tags), "write the notes of an activity")
		})
	}
	if wasMoved && (moved.ID() != saved.ID() || !sameDay(moved.StartTime, saved.StartTime)) &&
		len(r.index.at(r.keys.key(moved.StartTime))) == 0 {
		writes = append(writes, func(ctx context.Context) error {
			return wrap(r.cfg.Notes.Delete(ctx, moved.ID(), moved.StartTime), "remove the notes of a moved activity")
		})
	}
	for _, write := range writes {
		if err := r.afterLines(ctx, write); err != nil {
			return err
		}
	}
	return nil
}

// notesAsImaged reports whether the notes file of an activity as the backend held it holds the notes
// and tags of its image, read the way a compare reads them. A missing file is no edit: tock clears notes
// by saving empty ones.
func (r *run) notesAsImaged(ctx context.Context, held models.Activity, image fields) (bool, error) {
	notes, tags, found, err := r.lookupNotes(ctx, held)
	if err != nil {
		return false, errors.Wrapf(err, "read the notes of the activity at %s", r.keys.key(held.StartTime))
	}
	if !found {
		return true, nil
	}
	if notes != "" {
		held.Notes = notes
	}
	if len(tags) > 0 {
		held.Tags = tags
	}
	return sameNotes(image, activityFields(held, imageForm(r.precision))), nil
}

// backendActivity returns the activity in the backend an image describes: the one with its uid
// when the backend keeps uids, else the one at its key, or of several the one with its project and
// description.
func (r *run) backendActivity(img ports.SyncImage, recRef ref) (models.Activity, bool) {
	at := r.index.at(img.Key)
	if r.keepsUIDs {
		for _, a := range at {
			if a.UID == recRef.uid {
				return a, true
			}
		}
		// A record that gets its uid stored: the activity it was matched to.
		if r.force[recRef] && len(at) == 1 {
			return at[0], true
		}
		return models.Activity{}, false
	}
	if len(at) == 1 {
		return at[0], true
	}
	for _, a := range at {
		if sameTitle(img.Fields, a) {
			return a, true
		}
	}
	return models.Activity{}, false
}

// blocked reports whether another activity than the record's own holds key in the backend.
func (r *run) blocked(key string, recRef ref, current models.Activity, hasCurrent bool) bool {
	for _, a := range r.index.at(key) {
		if hasCurrent && identity(a) == identity(current) {
			continue
		}
		if r.keepsUIDs && a.UID == recRef.uid {
			continue
		}
		return true
	}
	return false
}

func (r *run) writeReminders(ctx context.Context) error {
	for _, id := range r.extraReminders {
		if err := r.backendWrite(ctx, func() error { return r.cfg.Reminders.Remove(ctx, id) }); err != nil &&
			!errors.Is(err, coreErrors.ErrReminderNotFound) {
			return errors.Wrapf(err, "remove reminder #%d", id)
		}
		r.report.Applied++
	}
	plans, err := r.reminderPlans(ctx)
	if err != nil || len(plans) == 0 {
		return err
	}

	list, err := r.cfg.Reminders.List(ctx)
	if err != nil {
		return errors.Wrap(err, "read the backend's reminders")
	}
	backend := newReminderIndex(list)
	for _, plan := range plans {
		if err = r.writeReminder(ctx, plan, backend); err != nil {
			return err
		}
	}
	return nil
}

// reminderPlans returns the writes of the merged reminders the backend holds otherwise than the images
// say.
func (r *run) reminderPlans(ctx context.Context) ([]writePlan, error) {
	refs, err := r.loadImagesOf(ctx, journal.TypeReminder)
	if err != nil {
		return nil, err
	}
	var plans []writePlan
	for _, recRef := range refs {
		record, _ := r.store.get(recRef)
		img, hasImg := r.images.get(recRef)
		reminder, visible := record.Reminder()
		if !visible {
			if hasImg && isDeleted(record) {
				plans = append(plans, writePlan{ref: recRef, img: img, hasImg: true, remove: true})
			}
			continue
		}
		view := reminderFields(reminder, imageForm(r.precision))
		if hasImg && fields(img.Fields).equal(view) && !r.force[recRef] {
			continue
		}
		plans = append(plans, writePlan{ref: recRef, img: img, hasImg: hasImg, reminder: reminder, view: view})
	}
	return plans, nil
}

// backUp copies, before a run that matches the backend's records writes it from the merged state (a
// first init, a backend switch or rebuild, Init past the brake, a restore), the files that exist and
// hold what the write-back rewrites (ports.SyncBackend.Backup), next to them, named with
// .before-match- (.before-restore- on a restore) and the local time: what the backend held that no
// image covers, such as notes the folder has other values for, is not journaled. The report lists the
// copies.
func (r *run) backUp(ctx context.Context) error {
	if !r.opts.match && len(r.opts.matchTypes) == 0 {
		return nil
	}
	suffix := ".before-restore-"
	if !r.opts.restore {
		suffix = ".before-match-"
	}
	activities, _, err := r.activityPlans(ctx)
	if err != nil {
		return err
	}
	reminders, err := r.reminderPlans(ctx)
	if err != nil {
		return err
	}
	var scope ports.SyncScope
	at := func(start time.Time) {
		scope.Activities = append(scope.Activities, ports.SyncSpan{From: start, To: start.Add(r.precision)})
	}
	for _, plan := range activities {
		if start, ok := r.keys.parse(plan.img.Key); plan.hasImg && ok {
			at(start)
		}
		if !plan.remove {
			at(plan.activity.StartTime)
		}
	}
	for _, a := range r.extra {
		at(a.StartTime)
	}
	scope.Reminders = len(reminders) > 0 || len(r.extraReminders) > 0
	if scope.IsEmpty() {
		return nil
	}
	backups, err := r.backend.Backup(ctx, scope, suffix+r.now.Local().Format("20060102-150405"))
	r.report.Backups = backups
	if err != nil {
		return errors.Wrap(err, "back up the backend before it is written again")
	}
	return nil
}

// reminderIndex holds the backend's reminders by uid and by #N.
type reminderIndex struct {
	byUID map[string]models.Reminder
	byID  map[int]models.Reminder
}

func newReminderIndex(list []models.Reminder) *reminderIndex {
	x := &reminderIndex{byUID: make(map[string]models.Reminder, len(list)), byID: make(map[int]models.Reminder, len(list))}
	for _, reminder := range list {
		x.byID[reminder.ID] = reminder
		if reminder.UID != "" {
			x.byUID[reminder.UID] = reminder
		}
	}
	return x
}

// writeReminder removes or saves one reminder in the backend. A reminder new to this device gets the
// #N it had here before, when no other reminder took it since, else the next free one. A reminder the
// backend no longer holds as its image says was changed after the run read it, by hand: it is left as
// it is, for the next run to compare.
func (r *run) writeReminder(ctx context.Context, plan writePlan, backend *reminderIndex) error {
	current, hasCurrent := backend.byUID[plan.ref.uid]
	if !hasCurrent && plan.hasImg && r.force[plan.ref] {
		// A reminder that gets its uid stored: the one its image numbers.
		if id, err := strconv.Atoi(plan.img.Key); err == nil {
			current, hasCurrent = backend.byID[id]
		}
	}
	if hasCurrent && plan.hasImg && !reminderFields(current, imageForm(r.precision)).equal(plan.img.Fields) {
		r.outside = true
		return nil
	}
	if plan.remove {
		r.images.drop(plan.ref)
		if !hasCurrent {
			return nil
		}
		if err := r.backendWrite(ctx, func() error { return r.cfg.Reminders.Remove(ctx, current.ID) }); err != nil &&
			!errors.Is(err, coreErrors.ErrReminderNotFound) {
			return errors.Wrapf(err, "remove reminder #%d", current.ID)
		}
		r.report.Applied++
		return nil
	}

	numbers, err := r.images.reminderNumbers(ctx)
	if err != nil {
		return err
	}
	reminder := plan.reminder
	reminder.UID = plan.ref.uid
	switch {
	case hasCurrent:
		reminder.ID = current.ID
	case numbers[plan.ref.uid] > 0:
		if _, taken := backend.byID[numbers[plan.ref.uid]]; !taken {
			reminder.ID = numbers[plan.ref.uid]
		}
	}

	var saved *models.Reminder
	err = r.backendWrite(ctx, func() error {
		var saveErr error
		saved, saveErr = r.cfg.Reminders.Save(ctx, reminder)
		return saveErr
	})
	if err != nil {
		return errors.Wrapf(err, "write reminder %q", reminder.Title)
	}
	if saved == nil {
		return errors.Errorf("write reminder %q: the backend returned none", reminder.Title)
	}
	backend.byID[saved.ID] = *saved
	backend.byUID[plan.ref.uid] = *saved
	if err = r.images.setReminderNumber(ctx, plan.ref.uid, saved.ID); err != nil {
		return err
	}
	r.images.set(ports.SyncImage{Type: journal.TypeReminder, UID: plan.ref.uid, Key: strconv.Itoa(saved.ID), Fields: plan.view})
	r.report.Applied++
	return nil
}

// sameNotes reports whether two activity images hold the same notes and tags.
func sameNotes(a, b fields) bool {
	if !bytes.Equal(a[fieldNotes], b[fieldNotes]) {
		return false
	}
	for path := range a {
		if isTagPath(path) && !bytes.Equal(a[path], b[path]) {
			return false
		}
	}
	for path := range b {
		if isTagPath(path) && !bytes.Equal(a[path], b[path]) {
			return false
		}
	}
	return true
}

func sameDay(a, b time.Time) bool {
	return a.Format(time.DateOnly) == b.Format(time.DateOnly)
}
