package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	exportapp "github.com/kriuchkov/tock/internal/app/export"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func NewICalCmd() *cobra.Command {
	var outputDir string
	var openApp bool

	cmd := &cobra.Command{
		Use:   "ical [key or date]",
		Short: "Generate iCal (.ics) file for a specific task, all tasks in a day, or all tasks.",
		Long:  defaultText("ical.long"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runICalCmd(cmd, args, outputDir, openApp)
		},
	}

	cmd.Flags().StringVar(&outputDir, "path", "", defaultText("ical.flag.path"))
	cmd.Flags().BoolVar(&openApp, "open", false, defaultText("ical.flag.open"))
	return cmd
}

func runICalCmd(cmd *cobra.Command, args []string, outputDir string, openApp bool) error {
	out := cmd.OutOrStdout()
	if openApp && runtime.GOOS != "darwin" {
		return errors.New(text(cmd, "ical.error.open_macos_only"))
	}

	if len(args) == 0 {
		if outputDir == "" && !openApp {
			return errors.New(text(cmd, "ical.error.path_required"))
		}
		return handleFullExport(cmd, out, outputDir, openApp)
	}

	keyOrDate := args[0]
	ref, err := models.ParseActivityReference(keyOrDate)
	if err != nil {
		return errors.Wrap(err, "parse key or date")
	}

	if !ref.HasSequence && outputDir == "" && !openApp {
		return errors.New(text(cmd, "ical.error.path_required"))
	}

	activities, err := getActivitiesForDate(cmd, ref.Date)
	if err != nil {
		return errors.Wrap(err, "get activities for date")
	}

	if ref.HasSequence {
		activity, seqErr := models.ActivityForSequence(activities, ref.Sequence)
		if seqErr != nil {
			return errors.New(text(cmd, "ical.error.activity_not_found", ref.Sequence, len(activities)))
		}
		return handleSingleExport(out, activity, keyOrDate, outputDir, openApp)
	}

	return handleBulkExport(out, activities, keyOrDate, outputDir, openApp)
}

func handleFullExport(cmd *cobra.Command, out io.Writer, outputDir string, openApp bool) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	activities, err := service.List(cmd.Context(), models.ActivityFilter{})
	if err != nil {
		return errors.Wrap(err, "list all activities")
	}

	if len(activities) == 0 {
		fmt.Fprintln(out, text(cmd, "common.no_activities"))
		return nil
	}

	combinedContent := exportapp.CombinedCalendar(activities)

	// Use configured filename or default
	fileName := ""
	if cfg := rt.Config; cfg != nil && cfg.Export.ICal.FileName != "" {
		fileName = cfg.Export.ICal.FileName
	}
	fileName = exportapp.ResolveExportFileName(fileName)

	//nolint:nestif // straightforward logic
	if outputDir != "" {
		if err = os.MkdirAll(outputDir, 0750); err != nil {
			return errors.Wrap(err, "create output directory")
		}

		filename := filepath.Join(outputDir, fileName)
		if err = os.WriteFile(filename, []byte(combinedContent), 0600); err != nil {
			return errors.Wrap(err, "write file")
		}

		if _, err = fmt.Fprintf(out, text(cmd, "ical.export.all"), filename); err != nil {
			return errors.Wrap(err, "write export message")
		}
		if openApp {
			return openFileInCalendar(filename)
		}
	} else if openApp {
		var f *os.File
		tempPattern := strings.TrimSuffix(fileName, ".ics") + "-*.ics"
		f, err = os.CreateTemp("", tempPattern)
		if err != nil {
			return errors.Wrap(err, "create temp file")
		}

		if _, err = f.WriteString(combinedContent); err != nil {
			return errors.Wrap(err, "write temp file")
		}

		f.Close() //nolint:gosec // file is used later

		if err = openFileInCalendar(f.Name()); err != nil {
			return err
		}

		fmt.Fprintln(out, text(cmd, "ical.opened.all"))
	}
	return nil
}

func getActivitiesForDate(cmd *cobra.Command, date time.Time) ([]models.Activity, error) {
	service := getRuntime(cmd).ActivityService

	start, end := timeutil.LocalDayBounds(date)
	filter := models.ActivityFilter{
		FromDate: &start,
		ToDate:   &end,
	}

	report, err := service.GetReport(cmd.Context(), filter)
	if err != nil {
		return nil, errors.Wrap(err, "generate report")
	}
	return models.SortActivitiesByStart(report.Activities), nil
}

func handleSingleExport(out io.Writer, activity models.Activity, key string, outputDir string, openApp bool) error {
	content := exportapp.Generate(activity, key)

	//nolint:nestif,gocritic // straightforward logic
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0750); err != nil {
			return errors.Wrap(err, "create output directory")
		}
		filename := filepath.Join(outputDir, fmt.Sprintf("%s.ics", key))
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			return errors.Wrap(err, "write file")
		}

		if _, err := fmt.Fprintf(out, text(nil, "ical.export.single"), filename); err != nil {
			return errors.Wrap(err, "write export message")
		}
		if openApp {
			return openFileInCalendar(filename)
		}
	} else if openApp {
		f, err := os.CreateTemp("", "tock-*.ics")
		if err != nil {
			return errors.Wrap(err, "create temp file")
		}

		if _, err = f.WriteString(content); err != nil {
			return errors.Wrap(err, "write temp file")
		}

		f.Close() //nolint:gosec // file is used later

		if err = openFileInCalendar(f.Name()); err != nil {
			return err
		}

		fmt.Fprintln(out, defaultText("ical.opened.single"))
	} else {
		fmt.Fprintln(out, content)
	}
	return nil
}

func handleBulkExport(out io.Writer, activities []models.Activity, dateKey string, outputDir string, openApp bool) error {
	if len(activities) == 0 {
		fmt.Fprintln(out, defaultText("ical.empty_date"))
		return nil
	}

	combinedContent := exportapp.CombinedCalendar(activities)

	if openApp {
		f, err := os.CreateTemp("", fmt.Sprintf("tock-%s-*.ics", dateKey))
		if err != nil {
			return errors.Wrap(err, "create temp file")
		}

		if _, err = f.WriteString(combinedContent); err != nil {
			return errors.Wrap(err, "write temp file")
		}

		f.Close() //nolint:gosec // file is used later

		if err = openFileInCalendar(f.Name()); err != nil {
			return err
		}

		fmt.Fprintln(out, defaultText("ical.opened.all"))
	}

	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0750); err != nil {
			return errors.Wrap(err, "create output directory")
		}

		// Save as a single file for the day: YYYY-MM-DD.ics
		filename := filepath.Join(outputDir, fmt.Sprintf("%s.ics", dateKey))
		if err := os.WriteFile(filename, []byte(combinedContent), 0600); err != nil {
			return errors.Wrapf(err, "write %s", filename)
		}

		fmt.Fprintf(out, defaultText("ical.export.date"), dateKey, filename)
	}
	return nil
}

func openFileInCalendar(path string) error {
	if err := exec.CommandContext(context.Background(), "open", path).Run(); err != nil {
		return errors.Wrap(err, "open in calendar")
	}
	return nil
}
