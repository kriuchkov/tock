package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
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

func init() {
	metadata := resolveBuildMetadata(buildMetadata{
		version: version,
		commit:  commit,
		date:    date,
	}, readBuildInfo())
	version = metadata.version
	commit = metadata.commit
	date = metadata.date
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
			metadata.version = normalizeVersion(mainVersion)
		case settings["vcs.revision"] != "":
			metadata.version = "dev-" + shortRevision(settings["vcs.revision"])
		}

		if settings["vcs.modified"] == "true" && metadata.version != "" && !strings.Contains(metadata.version, "+dirty") {
			metadata.version += "+dirty"
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
	return value == "" || value == "unknown" || value == "dev"
}

func needsCommitFallback(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "none" || value == "unknown"
}

func needsDateFallback(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "unknown"
}

func shortRevision(revision string) string {
	if len(revision) <= 7 {
		return revision
	}
	return revision[:7]
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
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
