package commands

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/app/updatecheck"
	"github.com/kriuchkov/tock/internal/config"
)

func TestRunUpdateCheckPersistsAndWritesNotification(t *testing.T) {
	originalNow := updateCheckNow
	originalClient := updateCheckClient
	originalPerform := performUpdateCheck
	originalPersist := persistUpdateCheckTime
	t.Cleanup(func() {
		updateCheckNow = originalNow
		updateCheckClient = originalClient
		performUpdateCheck = originalPerform
		persistUpdateCheckTime = originalPersist
	})

	checkedAt := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	persisted := false
	updateCheckNow = func() time.Time { return checkedAt }
	updateCheckClient = func() *http.Client { return &http.Client{} }
	performUpdateCheck = func(_ context.Context, client *http.Client, now time.Time, state updatecheck.State) (updatecheck.Result, error) {
		require.NotNil(t, client)
		assert.Equal(t, checkedAt, now)
		assert.True(t, state.CheckUpdates)
		assert.Equal(t, version, state.CurrentVersion)
		return updatecheck.Result{
			Checked:         true,
			CheckedAt:       checkedAt,
			CurrentVersion:  "1.0.0",
			LatestRelease:   updatecheck.Release{TagName: "v1.1.0", HTMLURL: "https://example.com/release"},
			UpdateAvailable: true,
		}, nil
	}
	persistUpdateCheckTime = func(_ context.Context, got time.Time) error {
		persisted = true
		assert.Equal(t, checkedAt, got)
		return nil
	}

	cmd := newTestCLICommand(&stubActivityResolver{})
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	rt, ok := appruntime.FromContext(cmd.Context())
	require.True(t, ok)
	rt.Config = &config.Config{CheckUpdates: true}
	ctx := rt.WithContext(context.Background())
	cmd.SetContext(ctx)

	runUpdateCheck(cmd)

	assert.True(t, persisted)
	assert.Contains(t, out.String(), "Update available 1.0.0 -> v1.1.0")
	assert.Empty(t, errOut.String())
}
