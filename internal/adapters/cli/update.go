package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kriuchkov/tock/internal/config"
)

const (
	latestReleaseURL         = "https://api.github.com/repos/kriuchkov/tock/releases/latest"
	updateCheckTimeout       = 2 * time.Second
	updateNotificationFormat = "\nUpdate available %s -> %s\nVisit %s to update\n"
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

	if !cfg.CheckUpdates || needsVersionFallback(version) {
		return
	}

	if time.Since(cfg.LastUpdateCheck) < 7*24*time.Hour {
		return
	}

	release, err := fetchLatestRelease(ctx, &http.Client{Timeout: updateCheckTimeout})
	if err != nil {
		cmd.PrintErrln("Failed to check for updates:", err)
		return
	}

	if v, found := ctx.Value(viperKey{}).(*viper.Viper); found {
		v.Set("last_update_check", time.Now())
		if err = v.WriteConfig(); err != nil {
			cmd.PrintErrln("Failed to save update check time:", err)
		}
	}

	currentVersion := currentBuildVersion()
	latestVersion := normalizeVersion(release.TagName)
	comparison, isComparable := compareReleaseVersions(currentVersion, latestVersion)
	if isComparable && comparison < 0 {
		cmd.Printf(updateNotificationFormat, currentVersion, release.TagName, release.HTMLURL)
	}
}
