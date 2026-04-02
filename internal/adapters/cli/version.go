package cli

import (
	"cmp"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	buildVersionDev     = "dev"
	buildVersionUnknown = "unknown"
)

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version info",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("tock %s\ncommit: %s\nbuilt at: %s\n%s\n", version, commit, date, runtime.Version())
		},
	}
}

func needsVersionFallback(value string) bool {
	return value == "" || value == buildVersionUnknown || value == buildVersionDev
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	return value
}

func currentBuildVersion() string {
	current := normalizeVersion(version)
	if current == "" {
		return buildVersionUnknown
	}
	return current
}

func compareReleaseVersions(currentVersion, latestVersion string) (int, bool) {
	current, ok := parseSemanticVersion(currentVersion)
	if !ok {
		return 0, false
	}

	latest, ok := parseSemanticVersion(latestVersion)
	if !ok {
		return 0, false
	}

	switch {
	case current.major != latest.major:
		return cmp.Compare(current.major, latest.major), true
	case current.minor != latest.minor:
		return cmp.Compare(current.minor, latest.minor), true
	case current.patch != latest.patch:
		return cmp.Compare(current.patch, latest.patch), true
	case current.prerelease == latest.prerelease:
		return 0, true
	case current.prerelease == "":
		return 1, true
	case latest.prerelease == "":
		return -1, true
	default:
		return cmp.Compare(current.prerelease, latest.prerelease), true
	}
}

func parseSemanticVersion(value string) (semanticVersion, bool) {
	value = normalizeVersion(value)
	if value == "" {
		return semanticVersion{}, false
	}

	if idx := strings.IndexByte(value, '+'); idx >= 0 {
		value = value[:idx]
	}

	prerelease := ""
	if idx := strings.IndexByte(value, '-'); idx >= 0 {
		prerelease = value[idx+1:]
		value = value[:idx]
	}

	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semanticVersion{}, false
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semanticVersion{}, false
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semanticVersion{}, false
	}

	return semanticVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: prerelease,
	}, true
}
