package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/services/ics"
)

func NewICalCmd() *cobra.Command {
	var outputDir string
	var openApp bool

	cmd := &cobra.Command{
		Use:   "ical [key or date]",
		Short: "Generate iCal (.ics) file for a specific task, all tasks in a day, or all tasks.",
		Long:  "Generate iCal (.ics) file(s). Provide a key (YYYY-MM-DD-NN) for a single task, a date (YYYY-MM-DD) with --path to export all tasks for that day, or no arguments to export all tasks.\nUse --open to automatically import into the system calendar (macOS only).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if outputDir == "" && !openApp {
					return errors.New("output directory (--path) is required for bulk export unless --open is used")
				}

				if openApp && runtime.GOOS != "darwin" {
					return errors.New("--open is only supported on macOS")
				}

				return handleFullExport(cmd, outputDir, openApp)
			}

			keyOrDate := args[0]
			date, seq, isSingle, err := parseKeyOrDate(keyOrDate)
			if err != nil {
				return errors.Wrap(err, "parse key or date")
			}

			if !isSingle && outputDir == "" && !openApp {
				return errors.New("output directory (--path) is required for bulk export unless --open is used")
			}
			if openApp && runtime.GOOS != "darwin" {
				return errors.New("--open is only supported on macOS")
			}

			activities, err := getActivitiesForDate(cmd, date)
			if err != nil {
				return errors.Wrap(err, "get activities for date")
			}

			if isSingle {
				if seq < 1 || seq > len(activities) {
					return fmt.Errorf("activity not found (index %d out of range 1-%d)", seq, len(activities))
				}
				return handleSingleExport(activities[seq-1], keyOrDate, outputDir, openApp)
			}
			return handleBulkExport(activities, keyOrDate, outputDir, openApp)
		},
	}

	cmd.Flags().StringVar(&outputDir, "path", "", "Output directory for .ics files")
	cmd.Flags().BoolVar(&openApp, "open", false, "Add to macOS Calendar")
	return cmd
}

func handleFullExport(cmd *cobra.Command, outputDir string, openApp bool) error {
	service := getService(cmd)
	ctx := context.Background()

	activities, err := service.List(ctx, dto.ActivityFilter{})
	if err != nil {
		return errors.Wrap(err, "list all activities")
	}

	if len(activities) == 0 {
		fmt.Println("No activities found.")
		return nil
	}

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})

	var sb strings.Builder
	dayCounts := make(map[string]int)

	for _, act := range activities {
		d := act.StartTime.Format("2006-01-02")
		dayCounts[d]++
		id := fmt.Sprintf("%s-%02d", d, dayCounts[d])
		sb.WriteString(ics.GenerateEvent(act, id))
	}

	combinedContent := ics.WrapCalendar(sb.String())

	// Use configured filename or default
	fileName := "tock_export.ics"
	if cfg := getConfig(cmd); cfg != nil && cfg.Export.ICal.FileName != "" {
		fileName = cfg.Export.ICal.FileName
	}
	if !strings.HasSuffix(fileName, ".ics") {
		fileName += ".ics"
	}

	//nolint:nestif // straightforward logic
	if outputDir != "" {
		if err = os.MkdirAll(outputDir, 0750); err != nil {
			return errors.Wrap(err, "create output directory")
		}

		filename := filepath.Join(outputDir, fileName)
		if err = os.WriteFile(filename, []byte(combinedContent), 0600); err != nil {
			return errors.Wrap(err, "write file")
		}

		fmt.Printf("Exported all activities to %s\n", filename)
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

		fmt.Println("Opened combined calendar events in macOS Calendar")
	}
	return nil
}

func parseKeyOrDate(keyOrDate string) (time.Time, int, bool, error) {
	parts := strings.Split(keyOrDate, "-")

	if len(parts) == 3 {
		// Date only: YYYY-MM-DD
		date, err := time.Parse("2006-01-02", keyOrDate)
		if err != nil {
			return time.Time{}, 0, false, errors.Wrap(err, "invalid date format (expected YYYY-MM-DD)")
		}
		return date, 0, false, nil
	}

	if len(parts) >= 4 {
		// Key: YYYY-MM-DD-NN
		seqStr := parts[len(parts)-1]
		seq, err := strconv.Atoi(seqStr)
		if err != nil {
			return time.Time{}, 0, false, errors.Wrap(err, "invalid sequence number")
		}

		dateStr := strings.Join(parts[:len(parts)-1], "-")
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return time.Time{}, 0, false, errors.Wrap(err, "invalid date format")
		}
		return date, seq, true, nil
	}
	return time.Time{}, 0, false, errors.New("invalid key or date format")
}

func getActivitiesForDate(cmd *cobra.Command, date time.Time) ([]models.Activity, error) {
	service := getService(cmd)
	ctx := context.Background()

	start := date.Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	filter := dto.ActivityFilter{
		FromDate: &start,
		ToDate:   &end,
	}

	report, err := service.GetReport(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "generate report")
	}

	activities := make([]models.Activity, len(report.Activities))
	copy(activities, report.Activities)

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})
	return activities, nil
}

func handleSingleExport(activity models.Activity, key string, outputDir string, openApp bool) error {
	content := ics.Generate(activity, key)

	//nolint:nestif,gocritic // straightforward logic
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0750); err != nil {
			return errors.Wrap(err, "create output directory")
		}
		filename := filepath.Join(outputDir, fmt.Sprintf("%s.ics", key))
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			return errors.Wrap(err, "write file")
		}

		fmt.Printf("Exported %s\n", filename)
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

		fmt.Println("Opened calendar event in macOS Calendar")
	} else {
		fmt.Println(content)
	}
	return nil
}

func handleBulkExport(activities []models.Activity, dateKey string, outputDir string, openApp bool) error {
	if len(activities) == 0 {
		fmt.Println("No activities found for this date.")
		return nil
	}

	// Generate combined content for the day
	var sb strings.Builder
	dayCounts := make(map[string]int)
	for _, act := range activities {
		d := act.StartTime.Format("2006-01-02")
		dayCounts[d]++
		id := fmt.Sprintf("%s-%02d", d, dayCounts[d])
		sb.WriteString(ics.GenerateEvent(act, id))
	}
	combinedContent := ics.WrapCalendar(sb.String())

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

		fmt.Println("Opened combined calendar events in macOS Calendar")
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

		fmt.Printf("Exported activities for %s to %s\n", dateKey, filename)
	}
	return nil
}

func openFileInCalendar(path string) error {
	if err := exec.CommandContext(context.Background(), "open", path).Run(); err != nil {
		return errors.Wrap(err, "open in calendar")
	}
	return nil
}
