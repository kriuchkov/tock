package cli

import (
	"fmt"
	"regexp"
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

var pseudoVersionPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)-(.*)-([0-9a-fA-F]{12,})$`)

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
			metadata.version = normalizeBuildVersion(mainVersion, settings["vcs.revision"], settings["vcs.modified"] == "true")
		case settings["vcs.revision"] != "":
			metadata.version = normalizeBuildVersion("", settings["vcs.revision"], settings["vcs.modified"] == "true")
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

func normalizeBuildVersion(value, revision string, modified bool) string {
	value, extraBuildMetadata, metadataMarkedModified := splitBuildMetadata(strings.TrimSpace(value))
	modified = modified || metadataMarkedModified
	value = normalizeVersion(value)

	if value == "" || value == "(devel)" {
		if revision == "" {
			return buildVersionDev
		}
		return formatDevelopmentVersion("0.0.0", buildVersionDev, shortRevision(revision), extraBuildMetadata, modified)
	}

	if display, ok := normalizePseudoVersion(value, revision, extraBuildMetadata, modified); ok {
		return display
	}

	return appendBuildMetadata(value, extraBuildMetadata, modified)
}

func normalizePseudoVersion(value, revision, extraBuildMetadata string, modified bool) (string, bool) {
	matches := pseudoVersionPattern.FindStringSubmatch(value)
	if matches == nil {
		return "", false
	}

	baseVersion := matches[1]
	pseudoSegment := matches[2]
	embeddedRevision := matches[3]

	prereleasePrefix := buildVersionDev
	if !isPseudoTimestamp(pseudoSegment) {
		idx := strings.LastIndexByte(pseudoSegment, '.')
		if idx < 0 || !isPseudoTimestamp(pseudoSegment[idx+1:]) {
			return "", false
		}

		prefix := strings.TrimSuffix(pseudoSegment[:idx], ".0")
		if prefix != "" && prefix != "0" {
			prereleasePrefix = prefix + "." + buildVersionDev
		}
	}

	usedRevision := revision
	if usedRevision == "" {
		usedRevision = embeddedRevision
	}

	return formatDevelopmentVersion(baseVersion, prereleasePrefix, shortRevision(usedRevision), extraBuildMetadata, modified), true
}

func formatDevelopmentVersion(baseVersion, prerelease, shortSHA, extraBuildMetadata string, modified bool) string {
	buildMetadata := joinBuildMetadata(shortSHA, extraBuildMetadata, modified)
	if buildMetadata == "" {
		return baseVersion + "-" + prerelease
	}

	return baseVersion + "-" + prerelease + "+" + buildMetadata
}

func splitBuildMetadata(value string) (string, string, bool) {
	idx := strings.IndexByte(value, '+')
	if idx < 0 {
		return value, "", false
	}

	var filtered []string
	modified := false

	for part := range strings.SplitSeq(value[idx+1:], ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "dirty" {
			modified = true
			continue
		}
		filtered = append(filtered, part)
	}

	return value[:idx], strings.Join(filtered, "."), modified
}

func appendBuildMetadata(value, extraBuildMetadata string, modified bool) string {
	buildMetadata := joinBuildMetadata("", extraBuildMetadata, modified)
	if buildMetadata == "" {
		return value
	}
	return value + "+" + buildMetadata
}

func joinBuildMetadata(shortSHA, extraBuildMetadata string, modified bool) string {
	var parts []string

	if shortSHA != "" {
		parts = append(parts, shortSHA)
	}

	if extraBuildMetadata != "" {
		parts = append(parts, extraBuildMetadata)
	}

	if modified {
		parts = append(parts, "dirty")
	}

	return strings.Join(parts, ".")
}

func isPseudoTimestamp(value string) bool {
	if len(value) != 14 {
		return false
	}

	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
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
