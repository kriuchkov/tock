package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestRunExportCmdStdoutUsesCommandWriter(t *testing.T) {
	end := time.Date(2026, time.March, 14, 10, 45, 0, 0, time.Local)
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			require.NotNil(t, filter.Project)
			assert.Equal(t, "tock", *filter.Project)

			activity := models.Activity{
				Project:     "tock",
				Description: "export",
				StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
				EndTime:     &end,
			}
			return &models.Report{Activities: []models.Activity{activity}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runExportCmd(cmd, &exportOptions{
		Project: "tock",
		Format:  "json",
		Stdout:  true,
	})
	require.NoError(t, err)

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &payload))
	require.Len(t, payload, 1)
	assert.Equal(t, "tock", payload[0]["project"])
	assert.Equal(t, "export", payload[0]["description"])
	assert.Equal(t, time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local).Format(time.RFC3339), payload[0]["start_time"])
}
