package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	buildVersionDev     = "dev"
	buildVersionUnknown = "unknown"
	buildCommitNone     = "none"
	darwinGOOS          = "darwin"
)

type buildMetadata struct {
	version string
	commit  string
	date    string
}

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

var (
	version, commit, date = resolveInitialBuildMetadata()
)

func resolveInitialBuildMetadata() (string, string, string) {
	metadata := resolveBuildMetadata(buildMetadata{
		version: buildVersionDev,
		commit:  buildCommitNone,
		date:    buildVersionUnknown,
	}, readBuildInfo())
	return metadata.version, metadata.commit, metadata.date
}

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version info",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("tock %s\ncommit: %s\nbuilt at: %s\n%s\n", version, commit, date, runtime.Version())
		},
	}
}

func readBuildInfo() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info
}

func resolveBuildMetadata(metadata buildMetadata, info *debug.BuildInfo) buildMetadata {
	if info == nil {
		return metadata
	}

	settings := buildSettings(info)
	if needsVersionFallback(metadata.version) {
		switch mainVersion := strings.TrimSpace(info.Main.Version); {
		case mainVersion != "" && mainVersion != "(devel)":
			metadata.version = normalizeBuildVersion(mainVersion, settings["vcs.modified"] == "true")
		case settings["vcs.revision"] != "":
			metadata.version = buildVersionDev
		}
	}

	if needsCommitFallback(metadata.commit) && settings["vcs.revision"] != "" {
		metadata.commit = settings["vcs.revision"]
	}

	if needsDateFallback(metadata.date) && settings["vcs.time"] != "" {
		metadata.date = settings["vcs.time"]
	}

	return metadata
}

func buildSettings(info *debug.BuildInfo) map[string]string {
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	return settings
}

func needsVersionFallback(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == buildVersionUnknown || value == buildVersionDev
}

func needsCommitFallback(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == buildCommitNone || value == buildVersionUnknown
}

func needsDateFallback(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == buildVersionUnknown
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	return value
}

func normalizeBuildVersion(value string, modified bool) string {
	value = normalizeVersion(strings.TrimSpace(value))
	if value == "" || value == "(devel)" {
		return buildVersionDev
	}

	if modified && !strings.Contains(value, "+dirty") {
		value += "+dirty"
	}

	return value
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
		return compareInts(current.major, latest.major), true
	case current.minor != latest.minor:
		return compareInts(current.minor, latest.minor), true
	case current.patch != latest.patch:
		return compareInts(current.patch, latest.patch), true
	case current.prerelease == latest.prerelease:
		return 0, true
	case current.prerelease == "":
		return 1, true
	case latest.prerelease == "":
		return -1, true
	default:
		return compareStrings(current.prerelease, latest.prerelease), true
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

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
