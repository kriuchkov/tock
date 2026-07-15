//go:build darwin && cgo

package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestCompactDuration(t *testing.T) {
	cases := map[string]struct {
		in   time.Duration
		want string
	}{
		"zero":            {0, "0m"},
		"seconds only":    {45 * time.Second, "0m"},
		"minutes":         {5*time.Minute + 30*time.Second, "5m"},
		"just under hour": {59 * time.Minute, "59m"},
		"one hour":        {time.Hour, "1:00"},
		"hour and change": {time.Hour + 5*time.Minute, "1:05"},
		"many hours":      {12*time.Hour + 34*time.Minute, "12:34"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, compactDuration(tc.in))
		})
	}
}

func TestMenuDuration(t *testing.T) {
	cases := map[string]struct {
		in   time.Duration
		want string
	}{
		"zero":            {0, "0m"},
		"seconds only":    {45 * time.Second, "0m"},
		"minutes":         {15 * time.Minute, "15m"},
		"just under hour": {59 * time.Minute, "59m"},
		"exact hour":      {time.Hour, "1h 0m"},
		"hour and change": {time.Hour + 30*time.Minute, "1h 30m"},
		"many hours":      {12*time.Hour + 5*time.Minute, "12h 5m"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, menuDuration(tc.in))
		})
	}
}

func TestSortedProjectReports(t *testing.T) {
	byProject := map[string]models.ProjectReport{
		"work":     {ProjectName: "work", Duration: 15 * time.Minute},
		"meetings": {ProjectName: "meetings", Duration: 2 * time.Hour},
		"coding":   {ProjectName: "coding", Duration: 90 * time.Minute},
	}

	got := sortedProjectReports(byProject)

	names := make([]string, len(got))
	for i, r := range got {
		names[i] = r.ProjectName
	}
	// Descending by duration: meetings (2h) > coding (1h30m) > work (15m).
	assert.Equal(t, []string{"meetings", "coding", "work"}, names)
}

func TestSortedProjectReports_TieBreaksByName(t *testing.T) {
	byProject := map[string]models.ProjectReport{
		"beta":  {ProjectName: "beta", Duration: time.Hour},
		"alpha": {ProjectName: "alpha", Duration: time.Hour},
	}

	got := sortedProjectReports(byProject)

	assert.Equal(t, "alpha", got[0].ProjectName)
	assert.Equal(t, "beta", got[1].ProjectName)
}
