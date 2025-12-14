package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kriuchkov/tock/internal/adapters/file"
	"github.com/kriuchkov/tock/internal/adapters/timewarrior"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/activity"

	"github.com/spf13/cobra"
)

type serviceKey struct{}

func NewRootCmd() *cobra.Command {
	var filePath string
	var backend string

	cmd := &cobra.Command{
		Use:   "tock",
		Short: "A simple timetracker for the command line",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if backend == "" {
				backend = os.Getenv("TOCK_BACKEND")
				if backend == "" {
					backend = "file"
				}
			}

			var repo ports.ActivityRepository

			if backend == "timewarrior" {
				if filePath == "" {
					filePath = os.Getenv("TIMEWARRIORDB")
					if filePath == "" {
						home, err := os.UserHomeDir()
						if err != nil {
							return err
						}
						filePath = filepath.Join(home, ".timewarrior", "data")
					}
				}
				repo = timewarrior.NewRepository(filePath)
			} else {
				if filePath == "" {
					filePath = os.Getenv("TOCK_FILE")
					if filePath == "" {
						home, err := os.UserHomeDir()
						if err != nil {
							return err
						}
						filePath = filepath.Join(home, ".tock.txt")
					}
				}
				repo = file.NewRepository(filePath)
			}

			svc := activity.NewService(repo)

			ctx := context.WithValue(cmd.Context(), serviceKey{}, svc)
			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&filePath, "file", "f", "", "Path to the activity log file (or data directory for timewarrior)")
	cmd.PersistentFlags().StringVarP(&backend, "backend", "b", "", "Storage backend: 'file' (default) or 'timewarrior'")

	cmd.AddCommand(NewStartCmd())
	cmd.AddCommand(NewStopCmd())
	cmd.AddCommand(NewAddCmd())
	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewReportCmd())
	cmd.AddCommand(NewLastCmd())
	cmd.AddCommand(NewContinueCmd())
	cmd.AddCommand(NewCurrentCmd())
	cmd.AddCommand(NewCalendarCmd())
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
