package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/config"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func checkUpdate() (githubRelease, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(
		"https://api.github.com/repos/kriuchkov/tock/releases/latest",
	) //nolint:noctx // No context needed for this simple request
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
	if cfg, ok := cmd.Context().Value(configKey{}).(*config.Config); ok {
		if !cfg.CheckUpdates || version == "" || version == "unknown" || version == "dev" {
			return
		}

		remote, err := checkUpdate()
		if err != nil {
			fmt.Printf("Failed to check for updates: %v\n", err)
			return
		}

		remoteVersion := strings.TrimPrefix(remote.TagName, "v")
		localVersion := strings.TrimPrefix(version, "v")

		if remoteVersion != localVersion {
			fmt.Printf("\nUpdate available %s -> %s\nVisit %s to update\n", version, remote.TagName, remote.HTMLURL)
		}
	}
}
