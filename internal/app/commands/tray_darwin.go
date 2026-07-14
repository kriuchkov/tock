//go:build darwin

package commands

import (
	"context"
	"time"

	"fyne.io/systray"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const trayRefreshInterval = time.Second

// trayController drives the macOS menu bar item. All state mutation happens on
// the single loop goroutine, so the menu item fields need no extra locking.
type trayController struct {
	service ports.ActivityResolver
	loc     *localization.Localizer

	mStartLast *systray.MenuItem
	mStop      *systray.MenuItem
	mQuit      *systray.MenuItem
}

// runTray blocks running the menu bar event loop until the user quits. It must
// be called from the main goroutine because the macOS status bar API requires
// the main thread.
func runTray(cmd *cobra.Command) error {
	rt := getRuntime(cmd)
	ctx := cmd.Context()

	c := &trayController{service: rt.ActivityService, loc: rt.Localizer}
	systray.Run(func() { c.onReady(ctx) }, func() {})
	return nil
}

func (c *trayController) onReady(ctx context.Context) {
	systray.SetTitle(c.loc.Text("tray.title.idle"))
	systray.SetTooltip(c.loc.Text("tray.title.idle"))

	c.mStartLast = systray.AddMenuItem(c.loc.Text("tray.menu.start_last"), "")
	c.mStop = systray.AddMenuItem(c.loc.Text("tray.menu.stop"), "")
	systray.AddSeparator()
	c.mQuit = systray.AddMenuItem(c.loc.Text("tray.menu.quit"), "")

	c.refresh(ctx)
	go c.loop(ctx)
}

func (c *trayController) loop(ctx context.Context) {
	ticker := time.NewTicker(trayRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			systray.Quit()
			return
		case <-ticker.C:
			c.refresh(ctx)
		case <-c.mStartLast.ClickedCh:
			c.startLast(ctx)
			c.refresh(ctx)
		case <-c.mStop.ClickedCh:
			c.stopCurrent(ctx)
			c.refresh(ctx)
		case <-c.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// refresh syncs the title, tooltip and menu item states with the currently
// running activity (if any).
func (c *trayController) refresh(ctx context.Context) {
	running := c.runningActivity(ctx)
	if running == nil {
		systray.SetTitle(c.loc.Text("tray.title.idle"))
		systray.SetTooltip(c.loc.Text("tray.tooltip.idle"))
		c.mStop.SetTitle(c.loc.Text("tray.menu.stop"))
		c.mStop.Disable()
		c.mStartLast.Enable()
		return
	}

	systray.SetTitle(running.DurationString())
	systray.SetTooltip(activityLabel(running))
	c.mStop.SetTitle(c.loc.Format("tray.menu.stop_running", activityLabel(running)))
	c.mStop.Enable()
	c.mStartLast.Disable()
}

func (c *trayController) runningActivity(ctx context.Context) *models.Activity {
	isRunning := true
	activities, err := c.service.List(ctx, models.ActivityFilter{IsRunning: &isRunning})
	if err != nil || len(activities) == 0 {
		return nil
	}
	return &activities[0]
}

// startLast starts a new activity copied from the most recent one. The service
// auto-stops any running activity, matching `tock continue`.
func (c *trayController) startLast(ctx context.Context) {
	activities, err := c.service.GetRecent(ctx, 1)
	if err != nil || len(activities) == 0 {
		return
	}

	last := activities[0]
	_, _ = c.service.Start(ctx, models.StartActivityRequest{
		Description: last.Description,
		Project:     last.Project,
		StartTime:   time.Now(),
	})
}

func (c *trayController) stopCurrent(ctx context.Context) {
	_, _ = c.service.Stop(ctx, models.StopActivityRequest{EndTime: time.Now()})
}

// activityLabel builds a compact "Project — Description" label, tolerating an
// empty project or description.
func activityLabel(a *models.Activity) string {
	switch {
	case a.Project != "" && a.Description != "":
		return a.Project + " — " + a.Description
	case a.Project != "":
		return a.Project
	default:
		return a.Description
	}
}
