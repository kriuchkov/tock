package commands

import (
	"context"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/app/updatecheck"
)

func persistUpdateCheckTime(ctx context.Context, checkedAt time.Time) error {
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
	runUpdateCheckWith(cmd, time.Now, updatecheck.CheckNow, persistUpdateCheckTime)
}

func runUpdateCheckWith(
	cmd *cobra.Command,
	now func() time.Time,
	check func(context.Context, time.Time, updatecheck.State) (updatecheck.Result, error),
	persist func(context.Context, time.Time) error,
) {
	ctx := cmd.Context()
	state, ok := buildUpdateCheckState(ctx)
	if !ok {
		return
	}

	result, err := check(ctx, now(), state)
	if err != nil {
		cmd.PrintErrln(errors.Wrap(err, "check for updates"))
		return
	}

	if !result.Checked {
		return
	}

	if err = persist(ctx, result.CheckedAt); err != nil {
		cmd.PrintErrln(errors.Wrap(err, "save update check time"))
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
