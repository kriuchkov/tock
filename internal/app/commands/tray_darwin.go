//go:build darwin

package commands

import (
	"context"
	"fmt"
	"sort"
	"time"

	"fyne.io/systray"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	trayRefreshInterval = time.Second
	trayRecentCount     = 10
	trayTodayProjects   = 6
	// trayTodayEvery throttles the today-summary rebuild. The panel is
	// minute-granular, so refreshing it every second would read the store for
	// nothing; this cadence also picks up activities changed from the CLI while
	// the tray is open.
	trayTodayEvery = 15
)

// trayController drives the macOS menu bar item. All state mutation happens on
// the single loop goroutine, so the menu item fields need no extra locking.
type trayController struct {
	service ports.ActivityResolver
	loc     *localization.Localizer

	// untilIdle quits the tray once an activity that was running goes away — set
	// when `tock start` auto-spawns the tray.
	untilIdle   bool
	sawActivity bool

	mToday     *systray.MenuItem
	mStartLast *systray.MenuItem
	mRecent    *systray.MenuItem
	mStop      *systray.MenuItem
	mQuit      *systray.MenuItem

	// recentSlots are fixed submenu items reused across refreshes; recent holds
	// the activities they currently map to. recentClick funnels slot clicks
	// (with their index) onto the loop goroutine.
	recentSlots []*systray.MenuItem
	recent      []models.Activity
	recentClick chan int

	// todaySlots are fixed submenu items under mToday holding the per-project
	// breakdown; todayTick throttles their rebuild (see trayTodayEvery).
	todaySlots []*systray.MenuItem
	todayTick  int
}

// runTray blocks running the menu bar event loop until the user quits. It must
// be called from the main goroutine because the macOS status bar API requires
// the main thread.
func runTray(cmd *cobra.Command) error {
	rt := getRuntime(cmd)
	ctx := cmd.Context()

	// Single instance: only one menu bar icon at a time. The lock is released
	// automatically when this process exits.
	lock, ok := acquireTrayLock()
	if !ok {
		fmt.Fprintln(cmd.ErrOrStderr(), text(cmd, "tray.already_running"))
		return nil
	}
	defer func() { _ = lock.Close() }()

	untilIdle, _ := cmd.Flags().GetBool("until-idle")
	if !untilIdle {
		// systray.Run blocks silently, so tell the user where to look and how to
		// quit before we hand control to the run loop. In auto-spawn mode stdio is
		// detached, so there is no point printing.
		fmt.Fprintln(cmd.ErrOrStderr(), text(cmd, "tray.running"))
	}

	c := &trayController{service: rt.ActivityService, loc: rt.Localizer, untilIdle: untilIdle}
	systray.Run(func() { c.onReady(ctx) }, func() {})
	return nil
}

func (c *trayController) onReady(ctx context.Context) {
	if icon := trayIconPNG(); len(icon) > 0 {
		systray.SetTemplateIcon(icon, icon)
	}

	// "Today" summary: total tracked time today with a per-project breakdown in
	// a submenu. Info only — the parent just expands, the rows are disabled.
	c.mToday = systray.AddMenuItem(c.loc.Text("tray.menu.today_empty"), "")
	c.todaySlots = make([]*systray.MenuItem, trayTodayProjects)
	for i := range c.todaySlots {
		slot := c.mToday.AddSubMenuItem("", "")
		slot.Disable()
		slot.Hide()
		c.todaySlots[i] = slot
	}
	systray.AddSeparator()

	c.mStartLast = systray.AddMenuItem(c.loc.Text("tray.menu.start_last"), "")

	c.mRecent = systray.AddMenuItem(c.loc.Text("tray.menu.start_recent"), "")
	c.recentClick = make(chan int)
	c.recentSlots = make([]*systray.MenuItem, trayRecentCount)
	for i := range c.recentSlots {
		slot := c.mRecent.AddSubMenuItem("", "")
		slot.Hide()
		c.recentSlots[i] = slot
		go c.forwardRecentClicks(i, slot)
	}

	c.mStop = systray.AddMenuItem(c.loc.Text("tray.menu.stop"), "")
	systray.AddSeparator()
	c.mQuit = systray.AddMenuItem(c.loc.Text("tray.menu.quit"), "")

	c.refreshAll(ctx)
	go c.loop(ctx)
}

// forwardRecentClicks relays a submenu slot's clicks (tagged with its index)
// onto the loop goroutine so all state stays single-threaded.
func (c *trayController) forwardRecentClicks(index int, item *systray.MenuItem) {
	for range item.ClickedCh {
		c.recentClick <- index
	}
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
			c.refreshAll(ctx)
		case index := <-c.recentClick:
			c.startRecent(ctx, index)
			c.refreshAll(ctx)
		case <-c.mStop.ClickedCh:
			c.stopCurrent(ctx)
			c.refreshAll(ctx)
		case <-c.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// refresh syncs the title, tooltip and menu item states with the currently
// running activity (if any).
func (c *trayController) refresh(ctx context.Context) {
	// Keep the today panel fresh (and pick up CLI edits) without querying the
	// store every second; actions refresh it immediately via refreshAll.
	c.todayTick++
	if c.todayTick >= trayTodayEvery {
		c.todayTick = 0
		c.refreshToday(ctx)
	}

	running := c.runningActivity(ctx)
	if running == nil {
		// In auto-spawn mode the tray exists only for the activity that launched
		// it: once that activity is stopped, quit so the icon disappears.
		if c.untilIdle && c.sawActivity {
			systray.Quit()
			return
		}
		// Icon only when idle; the timer text appears next to it while running.
		systray.SetTitle("")
		systray.SetTooltip(c.loc.Text("tray.tooltip.idle"))
		c.mStop.SetTitle(c.loc.Text("tray.menu.stop"))
		c.mStop.Disable()
		c.mStartLast.Enable()
		return
	}

	c.sawActivity = true
	label := activityLabel(running)
	systray.SetTitle(compactDuration(running.Duration()))
	systray.SetTooltip(label)
	c.mStop.SetTitle(c.loc.Format("tray.menu.stop_running", label))
	c.mStop.Enable()
	c.mStartLast.Disable()
}

// refreshAll updates both the running-activity state and the recent submenu; the
// per-second tick only refreshes the timer (via refresh) to avoid re-querying
// the recent list every second.
func (c *trayController) refreshAll(ctx context.Context) {
	c.refresh(ctx)
	c.refreshRecent(ctx)
	c.refreshToday(ctx)
	c.todayTick = 0
}

// refreshToday rebuilds the "Today" summary: the total tracked time today and a
// per-project breakdown ordered by descending duration. A running activity's
// time is counted live (GetReport clips it to the local day).
func (c *trayController) refreshToday(ctx context.Context) {
	from, to := timeutil.LocalDayBounds(time.Now())
	report, err := c.service.GetReport(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	if err != nil || report == nil || len(report.ByProject) == 0 {
		c.mToday.SetTitle(c.loc.Text("tray.menu.today_empty"))
		c.mToday.Disable()
		for _, slot := range c.todaySlots {
			slot.Hide()
		}
		return
	}

	c.mToday.SetTitle(c.loc.Format("tray.menu.today", menuDuration(report.TotalDuration)))
	c.mToday.Enable()

	projects := sortedProjectReports(report.ByProject)
	for i, slot := range c.todaySlots {
		if i < len(projects) {
			slot.SetTitle(c.projectSummaryLabel(projects[i]))
			slot.Show()
		} else {
			slot.Hide()
		}
	}
}

// sortedProjectReports orders project reports by descending duration, breaking
// ties by name so the menu is stable across refreshes.
func sortedProjectReports(byProject map[string]models.ProjectReport) []models.ProjectReport {
	reports := make([]models.ProjectReport, 0, len(byProject))
	for _, r := range byProject {
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Duration != reports[j].Duration {
			return reports[i].Duration > reports[j].Duration
		}
		return reports[i].ProjectName < reports[j].ProjectName
	})
	return reports
}

// projectSummaryLabel formats one "Project — 1h 30m" breakdown row, falling back
// to a placeholder when the activity has no project.
func (c *trayController) projectSummaryLabel(r models.ProjectReport) string {
	name := r.ProjectName
	if name == "" {
		name = c.loc.Text("tray.menu.no_project")
	}
	return fmt.Sprintf("%s — %s", name, menuDuration(r.Duration))
}

// refreshRecent repoints the fixed submenu slots at the latest recent
// activities, hiding the unused ones.
func (c *trayController) refreshRecent(ctx context.Context) {
	recent, err := c.service.GetRecent(ctx, trayRecentCount)
	if err != nil {
		recent = nil
	}
	c.recent = recent

	for i, slot := range c.recentSlots {
		if i < len(recent) {
			// Index matches `tock continue N` / `tock last` numbering.
			slot.SetTitle(fmt.Sprintf("[%d] %s", i, activityLabel(&recent[i])))
			slot.Show()
		} else {
			slot.Hide()
		}
	}

	if len(recent) == 0 {
		c.mRecent.Disable()
	} else {
		c.mRecent.Enable()
	}
}

// compactDuration formats an elapsed duration for the menu bar without ticking
// seconds: "5m" under an hour, "1:05" at or above an hour.
func compactDuration(d time.Duration) string {
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// menuDuration formats a duration for the dropdown: "2h 5m" with hours, "45m"
// without. Coarser than the status-bar timer so the breakdown stays readable.
func menuDuration(d time.Duration) string {
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
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
	c.startCopy(ctx, activities[0])
}

// startRecent starts the recent activity currently mapped to a submenu slot.
func (c *trayController) startRecent(ctx context.Context, index int) {
	if index < 0 || index >= len(c.recent) {
		return
	}
	c.startCopy(ctx, c.recent[index])
}

// startCopy starts a new activity with the same project/description as act. The
// service auto-stops any running activity, so this also acts as a project switch.
func (c *trayController) startCopy(ctx context.Context, act models.Activity) {
	_, _ = c.service.Start(ctx, models.StartActivityRequest{
		Description: act.Description,
		Project:     act.Project,
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
