package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
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

func TestGetDefaultExportDirUsesRuntimeDataPathForSQLite(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})
	rt := getRuntime(cmd)
	rt.Backend = "sqlite"
	rt.DataPath = "/tmp/tock/data/tock.db"
	cmd.SetContext(rt.WithContext(context.Background()))

	dir, err := getDefaultExportDir(cmd)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/tock/data", dir)
}

func TestGetDefaultExportDirUsesRuntimeDataPathForTimewarrior(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})
	rt := getRuntime(cmd)
	rt.Backend = "timewarrior"
	rt.DataPath = "/tmp/timewarrior"
	cmd.SetContext(rt.WithContext(context.Background()))

	dir, err := getDefaultExportDir(cmd)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/timewarrior", dir)
}

func TestRuntimeDefaultExportDirUsesTimewarriorErrorForEmptyPath(t *testing.T) {
	dir, err := (&appruntime.Runtime{Backend: "timewarrior"}).DefaultExportDir()
	assert.Empty(t, dir)
	require.EqualError(t, err, "timewarrior data path is empty")
}

func TestValidateExportFlags(t *testing.T) {
    tests := []struct {
        name    string
        opt     exportOptions
        wantErr string
    }{
        {
            name:    "from and today are mutually exclusive",
            opt:     exportOptions{From: "2026-04-01", Today: true},
            wantErr: "cannot specify multiple date filters",
        },
        {
            name:    "invalid from date",
            opt:     exportOptions{From: "not-a-date"},
            wantErr: "invalid --from date format",
        },
        {
            name:    "invalid to date",
            opt:     exportOptions{To: "2026-13-01"},
            wantErr: "invalid --to date format",
        },
        {
            name: "from only is valid",
            opt:  exportOptions{From: "2026-04-01"},
        },
        {
            name: "to only is valid",
            opt:  exportOptions{To: "2026-04-15"},
        },
        {
            name: "from and to together are valid",
            opt:  exportOptions{From: "2026-04-01", To: "2026-04-15"},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateExportFlags(&tt.opt)
            if tt.wantErr != "" {
                require.ErrorContains(t, err, tt.wantErr)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
