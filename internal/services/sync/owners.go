package sync

import (
	"context"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

// OwnedElsewhere implements ports.ActivityOwners from what the last sync saved: the image of the
// backend's activity, found by its key and title (and uid, where the backend keeps one), and the owner
// of the merged record it names. It is false before sync is set up, on another backend than the one it
// was set up with, and for an activity no sync has seen yet.
func (s *Service) OwnedElsewhere(ctx context.Context, activity models.Activity) (bool, error) {
	meta, err := s.cfg.State.Meta(ctx)
	if err != nil {
		return false, errors.Wrap(err, "read the sync state")
	}
	backend := s.cfg.Backend
	if meta.Device == "" || meta.Backend != backend.Identity() {
		return false, nil
	}
	keys := keying{precision: max(backend.Precision(), time.Second), wall: backend.WallClock()}
	images, err := s.cfg.State.ImagesByKey(ctx, journal.TypeActivity, []string{keys.key(activity.StartTime)})
	if err != nil {
		return false, errors.Wrap(err, "read the images of the running activity")
	}
	for _, img := range images {
		if !sameTitle(img.Fields, activity) || (backend.KeepsUIDs() && activity.UID != "" && activity.UID != img.UID) {
			continue
		}
		records, recordsErr := s.cfg.State.Records(ctx, journal.TypeActivity, []string{img.UID})
		if recordsErr != nil {
			return false, errors.Wrap(recordsErr, "read the running activity's record")
		}
		for _, record := range records {
			owner := record.Owner()
			return owner != "" && owner != meta.Device, nil
		}
	}
	return false, nil
}
