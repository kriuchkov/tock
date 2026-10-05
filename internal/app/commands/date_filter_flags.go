package commands

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
)

// dateFilterFlags holds the date filter flags shared by commands that build an activity filter.
type dateFilterFlags struct {
	Today     bool
	Yesterday bool
	Week      bool
	Month     bool
	Quarter   bool
	Year      bool
	Date      string
	From      string
	To        string
}

// register adds the date filter flags to cmd, reading their help text from "<keyPrefix>.flag.<name>".
func (f *dateFilterFlags) register(cmd *cobra.Command, keyPrefix string) {
	usage := func(name string) string { return defaultText(keyPrefix + ".flag." + name) }

	cmd.Flags().BoolVar(&f.Today, "today", false, usage("today"))
	cmd.Flags().BoolVar(&f.Yesterday, "yesterday", false, usage("yesterday"))
	cmd.Flags().BoolVar(&f.Week, "week", false, usage("week"))
	cmd.Flags().BoolVar(&f.Month, "month", false, usage("month"))
	cmd.Flags().BoolVar(&f.Quarter, "quarter", false, usage("quarter"))
	cmd.Flags().BoolVar(&f.Year, "year", false, usage("year"))
	cmd.Flags().StringVar(&f.Date, "date", "", usage("date"))
	cmd.Flags().StringVar(&f.From, "from", "", usage("from"))
	cmd.Flags().StringVar(&f.To, "to", "", usage("to"))
}

func (f *dateFilterFlags) filterOptions(project, description string) models.ActivityFilterOptions {
	return models.ActivityFilterOptions{
		Now:         time.Now(),
		Today:       f.Today,
		Yesterday:   f.Yesterday,
		Week:        f.Week,
		Month:       f.Month,
		Quarter:     f.Quarter,
		Year:        f.Year,
		Date:        f.Date,
		From:        f.From,
		To:          f.To,
		Project:     project,
		Description: description,
	}
}
