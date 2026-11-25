package ports_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kriuchkov/tock/internal/core/ports"
)

func TestAutoStopMarksOnlyItsContext(t *testing.T) {
	ctx := context.Background()
	assert.False(t, ports.IsAutoStop(ctx))
	_, ok := ports.AutoStopNext(ctx)
	assert.False(t, ok)

	next := time.Date(2026, time.November, 18, 9, 10, 0, 0, time.UTC)
	marked := ports.WithAutoStop(ctx, next)
	assert.True(t, ports.IsAutoStop(marked))
	assert.False(t, ports.IsAutoStop(ctx))
	got, ok := ports.AutoStopNext(marked)
	assert.True(t, ok)
	assert.True(t, got.Equal(next), "the start that stopped the activity")

	derived, cancel := context.WithCancel(marked)
	defer cancel()
	assert.True(t, ports.IsAutoStop(derived))

	workday := ports.WithAutoStop(ctx, time.Time{})
	assert.True(t, ports.IsAutoStop(workday))
	got, ok = ports.AutoStopNext(workday)
	assert.True(t, ok)
	assert.True(t, got.IsZero(), "the end of the working day")
}

func TestSyncTokenEqual(t *testing.T) {
	a := ports.SyncToken{"tock.txt": "120@1700000000", "reminders.yaml": "40@1700000001"}
	assert.True(t, a.Equal(ports.SyncToken{"reminders.yaml": "40@1700000001", "tock.txt": "120@1700000000"}))
	assert.False(t, a.Equal(ports.SyncToken{"tock.txt": "120@1700000000"}))
	assert.False(t, a.Equal(nil))
	assert.True(t, ports.SyncToken{}.Equal(nil))
}

func TestSyncScopeIsEmpty(t *testing.T) {
	assert.True(t, ports.SyncScope{}.IsEmpty())
	assert.False(t, ports.SyncScope{AllActivities: true}.IsEmpty())
	assert.False(t, ports.SyncScope{Reminders: true}.IsEmpty())

	day := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	assert.False(t, ports.SyncScope{Activities: []ports.SyncSpan{{From: day, To: day.AddDate(0, 0, 1)}}}.IsEmpty())
}
