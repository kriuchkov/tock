package export_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	exportapp "github.com/kriuchkov/tock/internal/app/export"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestCombinedCalendarUsesSequenceIDs(t *testing.T) {
	start1 := time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local)
	end1 := start1.Add(time.Hour)
	start2 := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end2 := start2.Add(30 * time.Minute)

	content := exportapp.CombinedCalendar([]models.Activity{
		{Project: "late", Description: "b", StartTime: start1, EndTime: &end1},
		{Project: "early", Description: "a", StartTime: start2, EndTime: &end2},
	})

	assert.Contains(t, content, "UID:2026-03-14-01@tock")
	assert.Contains(t, content, "UID:2026-03-14-02@tock")
	assert.Contains(t, content, "BEGIN:VCALENDAR")
	assert.Contains(t, content, "END:VCALENDAR")
}

func TestResolveExportFileName(t *testing.T) {
	assert.Equal(t, "tock_export.ics", exportapp.ResolveExportFileName(""))
	assert.Equal(t, "custom.ics", exportapp.ResolveExportFileName("custom"))
	assert.Equal(t, "ready.ics", exportapp.ResolveExportFileName("ready.ics"))
}
