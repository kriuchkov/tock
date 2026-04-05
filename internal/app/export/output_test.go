package export_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	exportapp "github.com/kriuchkov/tock/internal/app/export"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRenderCSVReportSortsActivities(t *testing.T) {
	lateStart := time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local)
	lateEnd := lateStart.Add(30 * time.Minute)
	earlyStart := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	earlyEnd := earlyStart.Add(45 * time.Minute)

	content, err := exportapp.RenderCSVReport([]models.Activity{
		{Project: "b", Description: "late", StartTime: lateStart, EndTime: &lateEnd},
		{Project: "a", Description: "early", StartTime: earlyStart, EndTime: &earlyEnd},
	})
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[1], "a,early")
	assert.Contains(t, lines[2], "b,late")
}

func TestRenderTextReportIncludesTotals(t *testing.T) {
	start := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end := start.Add(90 * time.Minute)
	report := &models.Report{
		Activities:    []models.Activity{{Project: "tock", Description: "refactor", StartTime: start, EndTime: &end}},
		TotalDuration: 90 * time.Minute,
		ByProject: map[string]models.ProjectReport{
			"tock": {
				ProjectName: "tock",
				Duration:    90 * time.Minute,
				Activities:  []models.Activity{{Project: "tock", Description: "refactor", StartTime: start, EndTime: &end}},
			},
		},
	}

	content := exportapp.RenderTextReport(report, timeutil.NewFormatter("24"))
	assert.Contains(t, content, "📁 tock: 1h 30m")
	assert.Contains(t, content, "⏱️  Total: 1h 30m")
	assert.Contains(t, content, "refactor")
}