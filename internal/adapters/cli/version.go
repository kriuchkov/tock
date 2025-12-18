package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tock %s\ncommit: %s\nbuilt at: %s\n%s\n", version, commit, date, runtime.Version())
		},
	}
}
