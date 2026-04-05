package commands

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRunAnalyzeCmdUsesCommandWriter(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			require.NotNil(t, filter.FromDate)
			require.NotNil(t, filter.ToDate)
			assert.True(t, filter.ToDate.After(*filter.FromDate))
			return &models.Report{}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runAnalyzeCmd(cmd, 7)
	require.NoError(t, err)
	assert.Equal(t, "No activities found for analysis.\n", out.String())
}

func TestRenderAnalysisLocalizesCanonicalDistributionKeys(t *testing.T) {
	stats := insights.Stats{
		DeepWorkScore:      55,
		AvgSessionDuration: 90 * time.Minute,
		Chronotype:         "Night Owl",
		PeakHour:           22,
		MostProductiveDay:  "Friday",
		AvgSwitchesPerDay:  2,
		FocusDistribution: map[string]int{
			insights.FocusDistributionFragmented: 1,
			insights.FocusDistributionFlow:       2,
			insights.FocusDistributionDeep:       3,
		},
	}

	var out bytes.Buffer
	err := renderAnalysis(&out, stats, &config.Config{}, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish))
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Fragmented (<15m)")
	assert.Contains(t, out.String(), "Flow (15m-1h)")
	assert.Contains(t, out.String(), "Deep Focus (>1h)")
	assert.Contains(t, out.String(), "3")
}
