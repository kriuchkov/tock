package sync_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

// ownedElsewhere asks a device's sync service about the activity with a description in its backend.
func ownedElsewhere(t *testing.T, d *device, description string) bool {
	t.Helper()
	for _, a := range d.list(t) {
		if a.Description == description {
			elsewhere, err := d.svc.OwnedElsewhere(context.Background(), a)
			require.NoError(t, err)
			return elsewhere
		}
	}
	t.Fatalf("%s has no activity %s", d.name, description)
	return false
}

func TestOwnedElsewhere(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	startOn(t, mac, "Mac", 0)
	assert.False(t, ownedElsewhere(t, mac, "work"), "before sync is set up")

	mac.init(t)
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(5))
	clock.now = at(6)
	mac.sync(t)
	assert.True(t, ownedElsewhere(t, mac, "Alpha"))
	assert.False(t, ownedElsewhere(t, mac, "work"))

	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "New", Description: "new", StartTime: at(-60), EndTime: at(-30),
	})
	require.NoError(t, err)
	assert.False(t, ownedElsewhere(t, mac, "new"), "not synced yet")
}
