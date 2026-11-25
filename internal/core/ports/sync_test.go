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

	marked := ports.WithAutoStop(ctx)
	assert.True(t, ports.IsAutoStop(marked))
	assert.False(t, ports.IsAutoStop(ctx))

	derived, cancel := context.WithCancel(marked)
	defer cancel()
	assert.True(t, ports.IsAutoStop(derived))
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
