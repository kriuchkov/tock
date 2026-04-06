package commands

import (
	"fmt"
	"os"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/core/ports"

	"github.com/spf13/cobra"
)

const (
	defaultRecentActivitiesForCompletion = 1000
)

var loadRuntime = appruntime.Load
var loadCompletionService = appruntime.LoadCompletionService

func NewRootCmd() *cobra.Command {
	var filePath string
	var backend string
	var configPath string
	var language string

	cmd := &cobra.Command{
		Use:     "tock",
		Short:   "A simple timetracker for the command line",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if shouldSkipRuntimeContext(cmd) {
				return nil
			}

			deps, err := loadRuntime(cmd.Context(), appruntime.Request{
				Backend:    backend,
				FilePath:   filePath,
				ConfigPath: configPath,
				Language:   language,
			})
			if err != nil {
				return fmt.Errorf("load runtime: %w", err)
			}

			ctx := deps.WithContext(cmd.Context())
			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&filePath, "file", "f", "", defaultText("root.flag.file"))
	cmd.PersistentFlags().StringVarP(&backend, "backend", "b", "", defaultText("root.flag.backend"))
	cmd.PersistentFlags().StringVar(&configPath, "config", "", defaultText("root.flag.config"))
	cmd.PersistentFlags().StringVar(&language, "lang", "", defaultText("root.flag.lang"))

	cmd.AddCommand(NewStartCmd())
	cmd.AddCommand(NewStopCmd())
	cmd.AddCommand(NewAddCmd())
	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewReportCmd())
	cmd.AddCommand(NewExportCmd())
	cmd.AddCommand(NewLastCmd())
	cmd.AddCommand(NewContinueCmd())
	cmd.AddCommand(NewCurrentCmd())
	cmd.AddCommand(NewRemoveCmd())
	cmd.AddCommand(NewWatchCmd())
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

func getRuntime(cmd *cobra.Command) *appruntime.Runtime {
	rt, ok := appruntime.FromContext(cmd.Context())
	if !ok {
		panic("command runtime is not set")
	}
	return rt
}

func getServiceForCompletion(cmd *cobra.Command) (ports.ActivityResolver, error) {
	configPath, _ := cmd.Root().PersistentFlags().GetString("config")
	backend, _ := cmd.Root().PersistentFlags().GetString("backend")
	filePath, _ := cmd.Root().PersistentFlags().GetString("file")

	return loadCompletionService(cmd.Context(), appruntime.Request{
		Backend:    backend,
		FilePath:   filePath,
		ConfigPath: configPath,
	})
}

func shouldSkipRuntimeContext(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "version", "completion":
		return true
	}
	return false
}

func projectRegisterFlagCompletion(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	svc, err := getServiceForCompletion(cmd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	acts, err := svc.GetRecent(cmd.Context(), defaultRecentActivitiesForCompletion)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	seen := make(map[string]bool)
	var projects []string
	for _, a := range acts {
		if a.Project != "" && !seen[a.Project] {
			seen[a.Project] = true
			projects = append(projects, a.Project)
		}
	}

	return projects, cobra.ShellCompDirectiveNoFileComp
}

func descriptionRegisterFlagCompletion(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	svc, err := getServiceForCompletion(cmd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	projectFilter, _ := cmd.Flags().GetString("project")

	acts, err := svc.GetRecent(cmd.Context(), defaultRecentActivitiesForCompletion)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	seen := make(map[string]bool)
	var descriptions []string
	for _, a := range acts {
		if projectFilter != "" && a.Project != projectFilter {
			continue
		}

		if a.Description != "" && !seen[a.Description] {
			seen[a.Description] = true
			descriptions = append(descriptions, a.Description)
		}
	}
	return descriptions, cobra.ShellCompDirectiveNoFileComp
}
