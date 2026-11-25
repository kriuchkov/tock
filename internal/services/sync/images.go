package sync

import (
	"cmp"
	"context"
	"maps"
	"slices"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// images holds the last-sync images a run works with, loaded from SyncState on demand, and what the
// run changed of them and of the local reminder numbers.
type images struct {
	state ports.SyncState
	byRef map[ref]ports.SyncImage
	// asked holds the records whose image was asked for by uid, found or not.
	asked map[ref]bool
	// all holds the types whose every image is loaded.
	all     map[journal.RecordType]bool
	changes map[ref]bool
	dropped map[ref]bool

	reminderIDs map[string]int
	idsChanged  map[string]bool
}

func newImages(state ports.SyncState) *images {
	return &images{
		state:      state,
		byRef:      make(map[ref]ports.SyncImage),
		asked:      make(map[ref]bool),
		all:        make(map[journal.RecordType]bool),
		changes:    make(map[ref]bool),
		dropped:    make(map[ref]bool),
		idsChanged: make(map[string]bool),
	}
}

// loadAll loads every image of one type and returns them, as the run holds them now.
func (x *images) loadAll(ctx context.Context, typ journal.RecordType) ([]ports.SyncImage, error) {
	if !x.all[typ] {
		loaded, err := x.state.Images(ctx, typ)
		if err != nil {
			return nil, errors.Wrapf(err, "load the %s images", typ)
		}
		for _, img := range loaded {
			x.keep(img)
		}
		x.all[typ] = true
	}
	var out []ports.SyncImage
	for r, img := range x.byRef {
		if r.typ == typ && !x.dropped[r] {
			out = append(out, img)
		}
	}
	sortImages(out)
	return out, nil
}

// loadUIDs loads the images of the records named that were not asked for yet.
func (x *images) loadUIDs(ctx context.Context, typ journal.RecordType, uids []string) error {
	if x.all[typ] {
		return nil
	}
	var missing []string
	for _, uid := range uids {
		r := ref{typ: typ, uid: uid}
		if !x.asked[r] {
			x.asked[r] = true
			missing = append(missing, uid)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	loaded, err := x.state.ImagesByUID(ctx, typ, missing)
	if err != nil {
		return errors.Wrapf(err, "load %d %s images", len(missing), typ)
	}
	for _, img := range loaded {
		x.keep(img)
	}
	return nil
}

// loadKeys loads the activity images with the given keys and returns them, as the run holds them now.
// Only an activity's key is a start the backend may hold several records at.
func (x *images) loadKeys(ctx context.Context, keys []string) ([]ports.SyncImage, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	typ := journal.TypeActivity
	if !x.all[typ] {
		loaded, err := x.state.ImagesByKey(ctx, typ, keys)
		if err != nil {
			return nil, errors.Wrapf(err, "load the %s images of %d keys", typ, len(keys))
		}
		for _, img := range loaded {
			x.keep(img)
		}
	}
	wanted := make(map[string]bool, len(keys))
	for _, key := range keys {
		wanted[key] = true
	}
	var out []ports.SyncImage
	for r, img := range x.byRef {
		if r.typ == typ && wanted[img.Key] && !x.dropped[r] {
			out = append(out, img)
		}
	}
	sortImages(out)
	return out, nil
}

// keep adds an image as SyncState holds it, unless the run changed it already.
func (x *images) keep(img ports.SyncImage) {
	r := ref{typ: img.Type, uid: img.UID}
	x.asked[r] = true
	if x.changes[r] || x.dropped[r] {
		return
	}
	x.byRef[r] = img
}

// get returns the image of a record, if it has one; the caller loaded it first.
func (x *images) get(r ref) (ports.SyncImage, bool) {
	if x.dropped[r] {
		return ports.SyncImage{}, false
	}
	img, ok := x.byRef[r]
	return img, ok
}

// set replaces the image of a record.
func (x *images) set(img ports.SyncImage) {
	r := ref{typ: img.Type, uid: img.UID}
	if old, ok := x.byRef[r]; ok && !x.dropped[r] && old.Key == img.Key && fields(old.Fields).equal(img.Fields) {
		return
	}
	delete(x.dropped, r)
	x.byRef[r] = img
	x.changes[r] = true
}

// drop removes the image of a record the backend no longer holds.
func (x *images) drop(r ref) {
	delete(x.changes, r)
	x.dropped[r] = true
}

// changed returns the images the run set, by type and uid.
func (x *images) changed() []ports.SyncImage {
	out := make([]ports.SyncImage, 0, len(x.changes))
	for r := range x.changes {
		if !x.dropped[r] {
			out = append(out, x.byRef[r])
		}
	}
	sortImages(out)
	return out
}

// droppedRefs returns the records whose image the run dropped, by type and uid.
func (x *images) droppedRefs() []ports.SyncRecordRef {
	out := make([]ports.SyncRecordRef, 0, len(x.dropped))
	for r := range x.dropped {
		out = append(out, ports.SyncRecordRef{Type: r.typ, UID: r.uid})
	}
	slices.SortFunc(out, func(a, b ports.SyncRecordRef) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.UID, b.UID))
	})
	return out
}

// reminderNumbers returns this device's #N of every reminder it numbered.
func (x *images) reminderNumbers(ctx context.Context) (map[string]int, error) {
	if x.reminderIDs == nil {
		ids, err := x.state.ReminderIDs(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "read the reminder numbers")
		}
		x.reminderIDs = make(map[string]int, len(ids))
		maps.Copy(x.reminderIDs, ids)
	}
	return x.reminderIDs, nil
}

// setReminderNumber records the #N a reminder has in the backend.
func (x *images) setReminderNumber(ctx context.Context, uid string, id int) error {
	ids, err := x.reminderNumbers(ctx)
	if err != nil {
		return err
	}
	if old, ok := ids[uid]; ok && old == id {
		return nil
	}
	ids[uid] = id
	x.idsChanged[uid] = true
	return nil
}

// changedReminderIDs returns the reminder numbers the run set.
func (x *images) changedReminderIDs() map[string]int {
	if len(x.idsChanged) == 0 {
		return nil
	}
	out := make(map[string]int, len(x.idsChanged))
	for uid := range x.idsChanged {
		out[uid] = x.reminderIDs[uid]
	}
	return out
}

func sortImages(imgs []ports.SyncImage) {
	slices.SortFunc(imgs, func(a, b ports.SyncImage) int {
		return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.UID, b.UID))
	})
}
