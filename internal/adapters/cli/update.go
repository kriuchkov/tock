package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kriuchkov/tock/internal/config"
)

const (
	latestReleaseURL   = "https://api.github.com/repos/kriuchkov/tock/releases/latest"
	updateCheckTimeout = 2 * time.Second
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func fetchLatestRelease(ctx context.Context, client *http.Client) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return githubRelease{}, errors.Wrap(err, "create release request")
	}

	resp, err := client.Do(request)
	if err != nil {
		return githubRelease{}, errors.Wrap(err, "fetch latest release")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release githubRelease
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, errors.Wrap(err, "decode release response")
	}

	return release, nil
}

func runUpdateCheck(cmd *cobra.Command) {
	ctx := cmd.Context()
	cfg, ok := ctx.Value(configKey{}).(*config.Config)
	if !ok {
		return
	}

	if !cfg.CheckUpdates || isUnversionedBuild() {
		return
	}

	if time.Since(cfg.LastUpdateCheck) < 7*24*time.Hour {
		return
	}

	release, err := fetchLatestRelease(ctx, &http.Client{Timeout: updateCheckTimeout})
	if err != nil {
		fmt.Printf("Failed to check for updates: %v\n", err)
		return
	}

	if v, done := ctx.Value(viperKey{}).(*viper.Viper); done {
		v.Set("last_update_check", time.Now())
		if err = v.WriteConfig(); err != nil {
			fmt.Printf("Failed to save update check time: %v\n", err)
		}
	}

	currentVersion := currentBuildVersion()
	latestVersion := normalizeVersion(release.TagName)
	comparison, isComparable := compareReleaseVersions(currentVersion, latestVersion)
	if isComparable {
		if comparison < 0 {
			fmt.Printf("\nUpdate available %s -> %s\nVisit %s to update\n", currentVersion, release.TagName, release.HTMLURL)
		}
		return
	}

	if strings.TrimPrefix(currentVersion, "v") != latestVersion {
		fmt.Printf("\nUpdate available %s -> %s\nVisit %s to update\n", currentVersion, release.TagName, release.HTMLURL)
	}
}

func isUnversionedBuild() bool {
	return version == "" || version == buildVersionUnknown || version == buildVersionDev
}

func currentBuildVersion() string {
	current := normalizeVersion(version)
	if current == "" {
		return buildVersionUnknown
	}
	return current
}
