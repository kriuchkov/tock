package commands

import (
	"context"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/app/updatecheck"
)

const (
	updateCheckFailedMessage   = "Failed to check for updates:"
	updatePersistFailedMessage = "Failed to save update check time:"
)

var updateCheckNow = time.Now

var updateCheckClient = func() *http.Client {
	return &http.Client{Timeout: updatecheck.CheckTimeout}
}

var performUpdateCheck = updatecheck.Check

var persistUpdateCheckTime = func(ctx context.Context, checkedAt time.Time) error {
	rt, ok := appruntime.FromContext(ctx)
	if !ok || rt.Viper == nil {
		return nil
	}
	rt.Viper.Set("last_update_check", checkedAt)
	return rt.Viper.WriteConfig()
}

func buildUpdateCheckState(ctx context.Context) (updatecheck.State, bool) {
	rt, ok := appruntime.FromContext(ctx)
	if !ok || rt.Config == nil {
		return updatecheck.State{}, false
	}

	return updatecheck.State{
		CheckUpdates:   rt.Config.CheckUpdates,
		LastCheckedAt:  rt.Config.LastUpdateCheck,
		CurrentVersion: version,
	}, true
}

func runUpdateCheck(cmd *cobra.Command) {
	ctx := cmd.Context()
	state, ok := buildUpdateCheckState(ctx)
	if !ok {
		return
	}

	result, err := performUpdateCheck(ctx, updateCheckClient(), updateCheckNow(), state)
	if err != nil {
		cmd.PrintErrln(updateCheckFailedMessage, err)
		return
	}
	if !result.Checked {
		return
	}

	if err = persistUpdateCheckTime(ctx, result.CheckedAt); err != nil {
		cmd.PrintErrln(updatePersistFailedMessage, err)
	}

	if result.UpdateAvailable {
		cmd.Printf(
			text(cmd, "update.available_notification"),
			result.CurrentVersion,
			result.LatestRelease.TagName,
			result.LatestRelease.HTMLURL,
		)
	}
}
