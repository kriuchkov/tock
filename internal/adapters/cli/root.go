package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/kriuchkov/tock/internal/adapters/file"
	"github.com/kriuchkov/tock/internal/adapters/timewarrior"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/activity"
	"github.com/kriuchkov/tock/internal/timeutil"

	"github.com/spf13/cobra"
)

type serviceKey struct{}
type configKey struct{}
type timeFormatterKey struct{}

func NewRootCmd() *cobra.Command {
	var filePath string
	var backend string
	var configPath string

	cmd := &cobra.Command{
		Use:     "tock",
		Short:   "A simple timetracker for the command line",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			var opts []config.Option
			if configPath != "" {
				opts = append(opts, config.WithConfigFile(configPath))
			}

			cfg, err := config.Load(opts...)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// 2. Initialize time formatter
			tf := timeutil.NewFormatter(cfg.TimeFormat)

			if backend == "" {
				backend = cfg.Backend
			}

			if filePath == "" {
				if backend == "timewarrior" {
					filePath = cfg.Timewarrior.DataPath
				} else {
					filePath = cfg.File.Path
				}
			}

			repo := initRepository(backend, filePath)

			svc := activity.NewService(repo)

			ctx := context.WithValue(cmd.Context(), serviceKey{}, svc)
			ctx = context.WithValue(ctx, configKey{}, cfg)
			ctx = context.WithValue(ctx, timeFormatterKey{}, tf)
			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&filePath, "file", "f", "", "Path to the activity log file (or data directory for timewarrior)")
	cmd.PersistentFlags().StringVarP(&backend, "backend", "b", "", "Storage backend: 'file' (default) or 'timewarrior'")
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "Config file path (default is $HOME/.config/tock/tock.yaml)")

	cmd.AddCommand(NewStartCmd())
	cmd.AddCommand(NewStopCmd())
	cmd.AddCommand(NewAddCmd())
	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewReportCmd())
	cmd.AddCommand(NewLastCmd())
	cmd.AddCommand(NewContinueCmd())
	cmd.AddCommand(NewCurrentCmd())
	cmd.AddCommand(NewCalendarCmd())
	cmd.AddCommand(NewAnalyzeCmd())
	cmd.AddCommand(NewICalCmd())
	cmd.AddCommand(NewVersionCmd())
	return cmd
}

func Execute() {
	rootCmd := NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getService(cmd *cobra.Command) ports.ActivityResolver {
	return cmd.Context().Value(serviceKey{}).(ports.ActivityResolver) //nolint:errcheck // always set
}

func getConfig(cmd *cobra.Command) *config.Config {
	return cmd.Context().Value(configKey{}).(*config.Config) //nolint:errcheck // always set
}

func getTimeFormatter(cmd *cobra.Command) *timeutil.Formatter {
	return cmd.Context().Value(timeFormatterKey{}).(*timeutil.Formatter) //nolint:errcheck // always set
}

func initRepository(backend, filePath string) ports.ActivityRepository {
	if backend == "timewarrior" {
		return timewarrior.NewRepository(filePath)
	}
	return file.NewRepository(filePath)
}
