package commands

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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
