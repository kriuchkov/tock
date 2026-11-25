package commands

import (
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// syncReportJSON is a sync report as tock sync --json prints it.
type syncReportJSON struct {
	Device         string   `json:"device"`
	ReplacedDevice string   `json:"replaced_device,omitempty"`
	Read           int      `json:"read"`
	Skipped        int      `json:"skipped"`
	Written        int      `json:"written"`
	Applied        int      `json:"applied"`
	Compared       bool     `json:"compared"`
	Rebuilt        bool     `json:"rebuilt"`
	Pending        int      `json:"pending"`
	Unreadable     []string `json:"unreadable,omitempty"`
	Backups        []string `json:"backups,omitempty"`
}

func newSyncReportJSON(report ports.SyncReport) syncReportJSON {
	return syncReportJSON{
		Device:         report.Device,
		ReplacedDevice: report.ReplacedDevice,
		Read:           report.Read,
		Skipped:        report.Skipped,
		Written:        report.Written,
		Applied:        report.Applied,
		Compared:       report.Compared,
		Rebuilt:        report.Rebuilt,
		Pending:        report.Pending,
		Unreadable:     report.Unreadable,
		Backups:        report.Backups,
	}
}

// syncInitJSON is what tock sync init --json prints.
type syncInitJSON struct {
	Dir string `json:"dir"`
	// ConfigUpdated is set when init turned sync on in the config file.
	ConfigUpdated bool           `json:"config_updated"`
	Report        syncReportJSON `json:"report"`
}

// syncStatusJSON is what tock sync status --json prints.
type syncStatusJSON struct {
	Enabled     bool             `json:"enabled"`
	Initialized bool             `json:"initialized"`
	Dir         string           `json:"dir"`
	Device      string           `json:"device,omitempty"`
	Backend     *syncBackendJSON `json:"backend,omitempty"`
	LastSync    *time.Time       `json:"last_sync,omitempty"`
	Pending     int              `json:"pending"`
	Skipped     int              `json:"skipped"`
	Devices     []syncDeviceJSON `json:"devices"`
}

type syncBackendJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type syncDeviceJSON struct {
	ID       string     `json:"id"`
	Name     string     `json:"name,omitempty"`
	Platform string     `json:"platform,omitempty"`
	App      string     `json:"app,omitempty"`
	Seen     *time.Time `json:"seen,omitempty"`
	Current  bool       `json:"current"`
}

func newSyncStatusJSON(enabled bool, status ports.SyncStatus) syncStatusJSON {
	out := syncStatusJSON{
		Enabled:     enabled,
		Initialized: status.Initialized,
		Dir:         status.Dir,
		Device:      status.Device,
		LastSync:    optionalTime(status.LastSync),
		Pending:     status.Pending,
		Skipped:     status.Skipped,
		Devices:     make([]syncDeviceJSON, 0, len(status.Devices)),
	}
	if status.Backend.Name != "" {
		out.Backend = &syncBackendJSON{Name: status.Backend.Name, Path: status.Backend.Path}
	}
	for _, device := range status.Devices {
		out.Devices = append(out.Devices, syncDeviceJSON{
			ID:       device.ID,
			Name:     device.Name,
			Platform: device.Platform,
			App:      device.App,
			Seen:     optionalTime(device.Seen),
			Current:  device.ID == status.Device,
		})
	}
	return out
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// writeSyncReport prints what a sync did: one line, plus a line for each thing that needs attention.
func writeSyncReport(cmd *cobra.Command, report ports.SyncReport) error {
	var b strings.Builder
	b.WriteString(text(cmd, "sync.report", report.Read, report.Written, report.Applied))
	if report.ReplacedDevice != "" {
		b.WriteString(text(cmd, "sync.report.replaced", report.ReplacedDevice, report.Device))
	}
	if report.Pending > 0 {
		b.WriteString(text(cmd, "sync.report.pending", report.Pending))
	}
	if report.Skipped > 0 {
		b.WriteString(text(cmd, "sync.report.skipped", report.Skipped))
	}
	if len(report.Unreadable) > 0 {
		b.WriteString(text(cmd, "sync.report.unreadable", strings.Join(report.Unreadable, ", ")))
	}
	for _, path := range report.Backups {
		b.WriteString(text(cmd, "sync.report.backup", path))
	}
	_, err := io.WriteString(cmd.OutOrStdout(), b.String())
	return err
}

// writeSyncStatus prints tock sync status: whether sync is on, the folder, this device, the last
// sync, what waits for iCloud, and the devices of the folder.
func writeSyncStatus(cmd *cobra.Command, enabled bool, status ports.SyncStatus) error {
	var b strings.Builder
	state := text(cmd, "sync.status.on")
	if !enabled {
		state = text(cmd, "sync.status.off")
	}
	b.WriteString(text(cmd, "sync.status.line.enabled", state))
	b.WriteString(text(cmd, "sync.status.line.dir", status.Dir))
	if !enabled {
		_, err := io.WriteString(cmd.OutOrStdout(), b.String())
		return err
	}

	device := status.Device
	if !status.Initialized || device == "" {
		device = text(cmd, "sync.status.not_initialized")
	}
	b.WriteString(text(cmd, "sync.status.line.device", device))
	b.WriteString(text(cmd, "sync.status.line.last_sync", formatSyncTime(cmd, status.LastSync)))
	if status.Pending > 0 {
		b.WriteString(text(cmd, "sync.status.line.pending", status.Pending))
	}
	if status.Skipped > 0 {
		b.WriteString(text(cmd, "sync.status.line.skipped", status.Skipped))
	}
	b.WriteString(text(cmd, "sync.status.line.devices", len(status.Devices)))
	for _, d := range status.Devices {
		current := ""
		if d.ID == status.Device {
			current = text(cmd, "sync.status.this_device")
		}
		about := describeSyncDevice(d)
		if about == "" {
			about = text(cmd, "sync.status.unknown_device")
		}
		b.WriteString(text(cmd, "sync.status.device_row", d.ID, about, formatSyncTime(cmd, d.Seen), current))
	}
	_, err := io.WriteString(cmd.OutOrStdout(), b.String())
	return err
}

// describeSyncDevice is "MacBook, macos, tock 2.4.0", leaving out what the device did not say.
func describeSyncDevice(d ports.SyncDevice) string {
	var parts []string
	for _, part := range []string{d.Name, d.Platform, d.App} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

// formatSyncTime shows a sync time in local time with the configured clock, or "never".
func formatSyncTime(cmd *cobra.Command, t time.Time) string {
	if t.IsZero() {
		return text(cmd, "sync.status.never")
	}
	tf := getRuntime(cmd).TimeFormatter
	if tf == nil {
		tf = timeutil.NewFormatter("24")
	}
	return t.Local().Format(tf.GetDisplayFormatWithDate())
}
