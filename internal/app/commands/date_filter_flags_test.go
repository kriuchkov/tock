package commands

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestDateFilterFlagsRegisteredOnReportAndExport(t *testing.T) {
	flags := []string{"today", "yesterday", "week", "month", "quarter", "year", "date", "from", "to"}

	for _, cmd := range []*cobra.Command{NewReportCmd(), NewExportCmd()} {
		for _, name := range flags {
			flag := cmd.Flags().Lookup(name)
			if assert.NotNil(t, flag, "%s is missing --%s", cmd.Name(), name) {
				assert.NotEqual(t, cmd.Name()+".flag."+name, flag.Usage, "%s --%s has no help text", cmd.Name(), name)
			}
		}
	}
}

func TestDateFilterPeriodFlags(t *testing.T) {
	tests := []struct {
		args    []string
		month   int
		quarter int
		wantErr bool
	}{
		{args: []string{"--month"}, month: models.CurrentPeriod},
		{args: []string{"--month=9"}, month: 9},
		{args: []string{"--quarter"}, quarter: models.CurrentPeriod},
		{args: []string{"--quarter=2"}, quarter: 2},
		{args: []string{"--month=0"}, wantErr: true},
		{args: []string{"--month=-1"}, wantErr: true},
		{args: []string{"--month=13"}, wantErr: true},
		{args: []string{"--quarter=5"}, wantErr: true},
		{args: []string{"--month=x"}, wantErr: true},
	}

	for _, tt := range tests {
		var flags dateFilterFlags
		cmd := &cobra.Command{Use: "test"}
		flags.register(cmd, "report")

		err := cmd.ParseFlags(tt.args)
		if tt.wantErr {
			require.Error(t, err, "%v", tt.args)
			continue
		}
		require.NoError(t, err, "%v", tt.args)
		assert.Equal(t, tt.month, flags.Month, "%v", tt.args)
		assert.Equal(t, tt.quarter, flags.Quarter, "%v", tt.args)
	}
}

func TestReportAndExportRejectPositionalArgs(t *testing.T) {
	// A bare --month takes no value, so "--month 9" leaves "9" as a positional argument.
	for _, cmd := range []*cobra.Command{NewReportCmd(), NewExportCmd()} {
		require.Error(t, cmd.Args(cmd, []string{"9"}), cmd.Name())
	}
}
