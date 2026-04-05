package commands

import (
	stdErrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	exportapp "github.com/kriuchkov/tock/internal/app/export"
	"github.com/kriuchkov/tock/internal/core/models"
)

type exportOptions struct {
	Today       bool
	Yesterday   bool
	Date        string
	Project     string
	Description string
	Format      string
	Path        string
	Stdout      bool
}

func NewExportCmd() *cobra.Command {
	var opt exportOptions

	cmd := &cobra.Command{
		Use:     "export",
		Aliases: []string{"e"},
		Short:   "Export report data to file",
		Long:    defaultText("export.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExportCmd(cmd, &opt)
		},
	}

	cmd.Flags().BoolVar(&opt.Today, "today", false, defaultText("export.flag.today"))
	cmd.Flags().BoolVar(&opt.Yesterday, "yesterday", false, defaultText("export.flag.yesterday"))
	cmd.Flags().StringVar(&opt.Date, "date", "", defaultText("export.flag.date"))
	cmd.Flags().StringVarP(&opt.Project, "project", "p", "", defaultText("export.flag.project"))
	cmd.Flags().StringVarP(&opt.Description, "description", "d", "", defaultText("export.flag.description"))
	cmd.Flags().StringVarP(&opt.Format, "format", "m", "txt", defaultText("export.flag.format"))
	cmd.Flags().StringVar(&opt.Format, "fmt", "txt", defaultText("export.flag.format"))
	cmd.Flags().StringVarP(&opt.Path, "path", "o", "", defaultText("export.flag.path"))
	cmd.Flags().BoolVar(&opt.Stdout, "stdout", false, defaultText("export.flag.stdout"))

	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc("description", descriptionRegisterFlagCompletion)
	return cmd
}

func runExportCmd(cmd *cobra.Command, opt *exportOptions) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	out := cmd.OutOrStdout()

	filter, err := models.BuildActivityFilter(models.ActivityFilterOptions{
		Now:         time.Now(),
		Today:       opt.Today,
		Yesterday:   opt.Yesterday,
		Date:        opt.Date,
		Project:     opt.Project,
		Description: opt.Description,
	})
	if err != nil {
		return err
	}

	report, err := service.GetReport(cmd.Context(), filter)
	if err != nil {
		return errors.Wrap(err, "generate report")
	}

	format := strings.ToLower(strings.TrimSpace(opt.Format))
	output, err := exportapp.RenderOutput(format, report, tf)
	if err != nil {
		return err
	}

	if opt.Stdout {
		_, err = out.Write(output)
		if err != nil {
			return errors.Wrap(err, "write stdout")
		}
		if len(output) == 0 || output[len(output)-1] != '\n' {
			fmt.Fprintln(out)
		}
		return nil
	}

	outputDir := opt.Path
	if outputDir == "" {
		outputDir, err = getDefaultExportDir(cmd)
		if err != nil {
			return err
		}
	}

	writtenPath, err := writeExportFile(outputDir, format, output)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, writtenPath)
	return nil
}

func writeExportFile(outputDir, format string, content []byte) (string, error) {
	if outputDir == "" {
		return "", errors.New("output path is empty")
	}

	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return "", errors.Wrap(err, "create output directory")
	}

	baseName := "tock-report-" + time.Now().Format("20060102-150405.000000000")
	for attempt := range 1000 {
		filename := fmt.Sprintf("%s.%s", baseName, format)
		if attempt > 0 {
			filename = fmt.Sprintf("%s-%d.%s", baseName, attempt, format)
		}

		fullPath := filepath.Join(outputDir, filename)
		file, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if stdErrors.Is(err, os.ErrExist) {
				continue
			}
			return "", errors.Wrap(err, "create output file")
		}

		if _, err = file.Write(content); err != nil {
			_ = file.Close()
			return "", errors.Wrap(err, "write output file")
		}
		if err = file.Close(); err != nil {
			return "", errors.Wrap(err, "close output file")
		}
		return fullPath, nil
	}

	return "", errors.New("unable to allocate unique output filename")
}

func getDefaultExportDir(cmd *cobra.Command) (string, error) {
	cfg := getRuntime(cmd).Config

	backend, err := cmd.Root().PersistentFlags().GetString("backend")
	if err != nil {
		return "", errors.Wrap(err, "read backend flag")
	}
	if backend == "" {
		backend = cfg.Backend
	}

	filePath, err := cmd.Root().PersistentFlags().GetString("file")
	if err != nil {
		return "", errors.Wrap(err, "read file flag")
	}
	if filePath == "" {
		if backend == backendTimewarrior {
			filePath = cfg.Timewarrior.DataPath
		} else {
			filePath = cfg.File.Path
		}
	}

	if backend == backendTimewarrior {
		if filePath == "" {
			return "", errors.New("timewarrior data path is empty")
		}
		return filePath, nil
	}

	if filePath == "" {
		return "", errors.New("activity file path is empty")
	}

	return filepath.Dir(filePath), nil
}
