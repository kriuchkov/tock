package sync_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// dropLastLines cuts the last n lines off a device's journal, as an older copy of it winning an
// iCloud conflict, or a restored backup, would.
func dropLastLines(t *testing.T, folder *memFolder, device string, n int) {
	t.Helper()
	folder.mu.Lock()
	defer folder.mu.Unlock()
	for key, data := range folder.plain {
		if key[0] == device {
			lines := bytes.SplitAfter(data, []byte("\n"))
			require.Greater(t, len(lines), n)
			folder.plain[key] = bytes.Join(lines[:len(lines)-1-n], nil)
		}
	}
}

// A journal of this Mac that lost its last line, the stop of an activity, is read again from the start:
// the stop is journaled again, so neither the Mac's tock.txt nor any other device sees the activity
// running again.
func TestRebuildRestoresTheChangesThisDeviceLost(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Start(context.Background(), models.StartActivityRequest{Project: "Mac", Description: "x", StartTime: at(0)})
	require.NoError(t, err)
	mac.sync(t)
	clock.advance(30 * time.Minute)
	_, err = mac.tracker.Stop(context.Background(), models.StopActivityRequest{EndTime: at(30)})
	require.NoError(t, err)
	mac.sync(t)
	before := fileText(t, mac)

	dropLastLines(t, mac.folder, mac.deviceID(), 1)
	require.Nil(t, visibleFrom(mac.folder, mac.deviceID())[0].EndTime)
	clock.advance(time.Hour)
	report := mac.sync(t)
	assert.True(t, report.Rebuilt)
	assert.Equal(t, 1, report.Written)
	assert.Zero(t, report.Applied)
	assert.Equal(t, before, fileText(t, mac))
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 1)
	require.NotNil(t, visible[0].EndTime)
	assert.True(t, visible[0].EndTime.Equal(at(30)))
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Written)
}

// The rebuild leaves alone what another device changed later than this one: only the fields whose
// latest value was this device's are journaled again.
func TestRebuildKeepsLaterChangesOfOthers(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "first", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	clock.now = at(60)
	mac.sync(t)
	uid := uidOf(t, mac, "Mac")
	a := mac.list(t)[0]
	edited := a
	edited.Description = "mac edit"
	require.NoError(t, mac.activities.Remove(context.Background(), a))
	require.NoError(t, mac.activities.Save(context.Background(), edited))
	clock.advance(time.Minute)
	mac.sync(t)
	clock.advance(time.Minute)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{"description": "phone edit"})
	clock.advance(time.Minute)
	mac.sync(t)

	dropLastLines(t, mac.folder, mac.deviceID(), 1)
	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.True(t, report.Rebuilt)
	assert.Zero(t, report.Written, "the phone's edit is later than the one the Mac lost")
	assert.Equal(t, []string{"phone edit"}, descriptions(visibleFrom(mac.folder, phone, mac.deviceID())))
	assert.Equal(t, []string{"phone edit"}, descriptions(mac.list(t)))
}

// A rebuild of state.db keeps the backend sync was set up with: the first command after it on another
// data file, as tock -f scratch.txt runs one, neither journals that file's activities nor takes its own
// for the backend. The real backend is matched again, and nothing is journaled twice.
func TestStateRebuildKeepsTheBackendSyncWasSetUpWith(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	mac.init(t)
	for i := range 3 {
		_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
			Project: "P", Description: "d", StartTime: at(i * 60), EndTime: at(i*60 + 30),
		})
		require.NoError(t, err)
	}
	clock.advance(4 * time.Hour)
	mac.sync(t)
	lines := len(folder.lines(mac.deviceID()))

	// What the rebuild keeps (syncstate's keptMeta).
	meta := mac.state.meta
	rebuilt := newMemState()
	rebuilt.meta = ports.SyncMeta{Dir: meta.Dir, Device: meta.Device, Clock: meta.Clock, Backend: meta.Backend, MatchBackend: true}

	scratch := newFileDevice(t, "mac", folder, clock)
	scratch.state = rebuilt
	scratch.build()
	_, err := scratch.tracker.Start(
		context.Background(), models.StartActivityRequest{Project: "Scratch", Description: "x", StartTime: clock.now},
	)
	require.NoError(t, err)
	_, err = scratch.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncBackendChanged)
	assert.Len(t, scratch.list(t), 1, "the scratch file gets nothing of the merged state")
	assert.Len(t, folder.lines(mac.deviceID()), lines, "nothing journaled")

	mac.state = rebuilt
	mac.build()
	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.Zero(t, report.Written)
	assert.Zero(t, report.Applied)
	assert.False(t, rebuilt.meta.MatchBackend, "matched once")
	assert.Len(t, visibleFrom(folder, mac.deviceID()), 3)
	assertQuiet(t, mac)
}
