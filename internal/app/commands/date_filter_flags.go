package commands

import (
	"strconv"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
)

// currentPeriodArg is the value pflag supplies for a bare --month or --quarter.
const currentPeriodArg = "current"

// periodValue is a flag value that takes a period number from 1 to limit.
// An int flag cannot be used: it accepts 0 and the models.CurrentPeriod sentinel from the command line.
type periodValue struct {
	target *int
	limit  int
}

func (v *periodValue) Set(s string) error {
	if s == currentPeriodArg {
		*v.target = models.CurrentPeriod
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > v.limit {
		return errors.Errorf("must be a number from 1 to %d", v.limit)
	}
	*v.target = n
	return nil
}

func (v *periodValue) String() string { return "" }

func (v *periodValue) Type() string { return "N" }

// dateFilterFlags holds the date filter flags shared by commands that build an activity filter.
type dateFilterFlags struct {
	Today     bool
	Yesterday bool
	Week      bool
	Month     int
	Quarter   int
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
	cmd.Flags().Var(&periodValue{target: &f.Month, limit: 12}, "month", usage("month"))
	cmd.Flags().Lookup("month").NoOptDefVal = currentPeriodArg
	cmd.Flags().Var(&periodValue{target: &f.Quarter, limit: 4}, "quarter", usage("quarter"))
	cmd.Flags().Lookup("quarter").NoOptDefVal = currentPeriodArg
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
