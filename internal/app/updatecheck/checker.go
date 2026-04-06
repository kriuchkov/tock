package updatecheck

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

const (
	LatestReleaseURL    = "https://api.github.com/repos/kriuchkov/tock/releases/latest"
	CheckTimeout        = 2 * time.Second
	CheckInterval       = 7 * 24 * time.Hour
	BuildVersionDev     = "dev"
	BuildVersionUnknown = "unknown"
)

type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type State struct {
	CheckUpdates   bool
	LastCheckedAt  time.Time
	CurrentVersion string
}

type Result struct {
	Checked         bool
	CheckedAt       time.Time
	CurrentVersion  string
	LatestRelease   Release
	UpdateAvailable bool
}

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func FetchLatestRelease(ctx context.Context, client *http.Client) (Release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestReleaseURL, nil)
	if err != nil {
		return Release{}, errors.Wrap(err, "create release request")
	}

	resp, err := client.Do(request)
	if err != nil {
		return Release{}, errors.Wrap(err, "fetch latest release")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release Release
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, errors.Wrap(err, "decode release response")
	}

	return release, nil
}

func CheckNow(ctx context.Context, now time.Time, state State) (Result, error) {
	client := &http.Client{Timeout: CheckTimeout}
	return Check(ctx, client, now, state)
}

func Check(ctx context.Context, client *http.Client, now time.Time, state State) (Result, error) {
	if !state.CheckUpdates || NeedsVersionFallback(state.CurrentVersion) {
		return Result{}, nil
	}

	if !state.LastCheckedAt.IsZero() && now.Sub(state.LastCheckedAt) < CheckInterval {
		return Result{}, nil
	}

	release, err := FetchLatestRelease(ctx, client)
	if err != nil {
		return Result{}, err
	}

	currentVersion := CurrentBuildVersion(state.CurrentVersion)
	latestVersion := NormalizeVersion(release.TagName)
	comparison, isComparable := CompareReleaseVersions(currentVersion, latestVersion)

	return Result{
		Checked:         true,
		CheckedAt:       now,
		CurrentVersion:  currentVersion,
		LatestRelease:   release,
		UpdateAvailable: isComparable && comparison < 0,
	}, nil
}

func NeedsVersionFallback(value string) bool {
	return value == "" || value == BuildVersionUnknown || value == BuildVersionDev
}

func NormalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	return value
}

func CurrentBuildVersion(version string) string {
	current := NormalizeVersion(version)
	if current == "" {
		return BuildVersionUnknown
	}
	return current
}

func CompareReleaseVersions(currentVersion, latestVersion string) (int, bool) {
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
	value = NormalizeVersion(value)
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

	return semanticVersion{major: major, minor: minor, patch: patch, prerelease: prerelease}, true
}
