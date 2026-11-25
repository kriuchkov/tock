package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-faster/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/mcpserver"
)

// NewMCPCmd returns the command that serves tock to AI clients over the Model Context
// Protocol on stdin/stdout. It is opt-in via the mcp.enabled config option.
func NewMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cliMCP,
		Short: defaultText("mcp.short"),
		Long:  defaultText("mcp.long"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runMCPCmd(cmd) },
	}
	cmd.AddCommand(newMCPInstallCmd())
	cmd.AddCommand(newMCPUninstallCmd())
	return cmd
}

func runMCPCmd(cmd *cobra.Command) error {
	rt := getRuntime(cmd)
	if !rt.Config.MCP.Enabled {
		return errors.New(text(cmd, "mcp.disabled"))
	}

	// Only JSON-RPC may reach stdout from here on; notices go to stderr.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return mcpserver.Run(ctx, mcpserver.Options{
		Activities:    rt.ActivityService,
		Reminders:     rt.ReminderService,
		WorkingHours:  rt.WorkingHours,
		ReadOnly:      rt.Config.MCP.ReadOnly,
		DisabledTools: rt.Config.MCP.DisabledTools,
		Version:       version,
		Snooze:        rt.Config.Reminders.Snooze,
		OnStarted:     func() { ensureTrayRunning(cmd) },
		// The recorder already sends every write to the sync journal; this syncs it, as after a command.
		AfterCall: func(ctx context.Context) { syncRecordedWrites(ctx, cmd) },
	}, &mcp.StdioTransport{})
}
