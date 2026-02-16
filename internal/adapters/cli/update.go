package cli

import (
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

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func checkUpdate() (githubRelease, error) {
	client := &http.Client{Timeout: 2 * time.Second}

	//nolint:noctx // No context needed for this simple request
	resp, err := client.Get(
		"https://api.github.com/repos/kriuchkov/tock/releases/latest",
	)
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

	if !cfg.CheckUpdates || (version == "" || version == "unknown" || version == "dev") {
		return
	}

	if time.Since(cfg.LastUpdateCheck) < 7*24*time.Hour {
		return
	}

	remote, err := checkUpdate()
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

	remoteVersion := strings.TrimPrefix(remote.TagName, "v")
	localVersion := strings.TrimPrefix(version, "v")

	if remoteVersion != localVersion {
		fmt.Printf("\nUpdate available %s -> %s\nVisit %s to update\n", version, remote.TagName, remote.HTMLURL)
	}
}
