package commands

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestRunLastCmdJSONUsesCommandWriter(t *testing.T) {
	end := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.Local)
	service := &stubActivityResolver{
		getRecentFn: func(_ context.Context, limit int) ([]models.Activity, error) {
			assert.Equal(t, 3, limit)
			return []models.Activity{{
				Project:     "tock",
				Description: "recent",
				StartTime:   time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local),
				EndTime:     &end,
			}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runLastCmd(cmd, &lastOptions{Limit: 3, JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
	assert.Contains(t, out.String(), "\"description\": \"recent\"")
}

func TestRunLastCmdTableUsesCommandWriter(t *testing.T) {
	service := &stubActivityResolver{
		getRecentFn: func(_ context.Context, _ int) ([]models.Activity, error) {
			return []models.Activity{{Project: "core", Description: "a"}, {Project: "ops", Description: "b"}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runLastCmd(cmd, &lastOptions{Limit: 2})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Description")
	assert.Contains(t, out.String(), "b")
	assert.Contains(t, out.String(), "ops")
}
