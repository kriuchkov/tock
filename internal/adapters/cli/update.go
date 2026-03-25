package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/kriuchkov/tock/internal/config"
)

const (
	latestReleaseURL      = "https://api.github.com/repos/kriuchkov/tock/releases/latest"
	updateCheckTimeout    = 2 * time.Second
	updateDownloadTimeout = 30 * time.Second
)

type githubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func NewUpdateCmd() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upgrade"},
		Short:   "Update to the latest official release",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpdateCmd(checkOnly)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for a newer release without installing")
	return cmd
}

func fetchLatestRelease(client *http.Client) (githubRelease, error) {
	resp, err := client.Get(latestReleaseURL)
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

	release, err := fetchLatestRelease(&http.Client{Timeout: updateCheckTimeout})
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
	comparison, comparable := compareReleaseVersions(currentVersion, latestVersion)
	if comparable {
		if comparison < 0 {
			fmt.Printf("\nUpdate available %s -> %s\nRun `tock update` or visit %s\n", currentVersion, release.TagName, release.HTMLURL)
		}
		return
	}

	if normalizeVersion(currentVersion) != latestVersion {
		fmt.Printf("\nUpdate available %s -> %s\nRun `tock update` or visit %s\n", currentVersion, release.TagName, release.HTMLURL)
	}
}

func runUpdateCmd(checkOnly bool) error {
	release, err := fetchLatestRelease(&http.Client{Timeout: updateDownloadTimeout})
	if err != nil {
		return err
	}

	currentVersion := currentBuildVersion()
	latestVersion := normalizeVersion(release.TagName)
	comparison, comparable := compareReleaseVersions(currentVersion, latestVersion)

	switch {
	case comparable && comparison == 0:
		fmt.Printf("Already up to date: %s\n", latestVersion)
		return nil
	case comparable && comparison > 0:
		fmt.Printf("Current build %s is newer than the latest official release %s\n", currentVersion, release.TagName)
		return nil
	case checkOnly:
		fmt.Printf("Update available %s -> %s\n%s\n", currentVersion, release.TagName, release.HTMLURL)
		return nil
	}

	executablePath, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "resolve current executable")
	}

	executablePath, err = filepath.EvalSymlinks(executablePath)
	if err != nil {
		return errors.Wrap(err, "resolve executable symlink")
	}

	asset, err := findReleaseArchive(release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	executableDir := filepath.Dir(executablePath)
	archivePath, err := downloadReleaseArchive(&http.Client{Timeout: updateDownloadTimeout}, executableDir, asset)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	binaryPath, err := extractBinaryFromArchive(archivePath, executableDir)
	if err != nil {
		return err
	}
	defer os.Remove(binaryPath)

	if err = replaceExecutable(executablePath, binaryPath); err != nil {
		if stdErrors.Is(err, os.ErrPermission) {
			return errors.Wrap(err, "replace current executable (re-run with permissions to modify the install path)")
		}
		return err
	}

	fmt.Printf("Updated %s -> %s\nInstalled %s\n", currentVersion, release.TagName, executablePath)
	return nil
}

func isUnversionedBuild() bool {
	return version == "" || version == "unknown" || version == "dev"
}

func currentBuildVersion() string {
	current := normalizeVersion(version)
	if current == "" {
		return "unknown"
	}
	return current
}

func findReleaseArchive(release githubRelease, goos, goarch string) (githubReleaseAsset, error) {
	name, err := releaseArchiveName(goos, goarch)
	if err != nil {
		return githubReleaseAsset{}, err
	}

	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, nil
		}
	}

	return githubReleaseAsset{}, errors.Errorf("release asset %s not found", name)
}

func releaseArchiveName(goos, goarch string) (string, error) {
	osName, err := releaseOSName(goos)
	if err != nil {
		return "", err
	}

	archName, err := releaseArchName(goarch)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("tock_%s_%s.tar.gz", osName, archName), nil
}

func releaseOSName(goos string) (string, error) {
	switch goos {
	case "linux":
		return "Linux", nil
	case "darwin":
		return "Darwin", nil
	default:
		return "", errors.Errorf("self-update is not supported on %s", goos)
	}
}

func releaseArchName(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", errors.Errorf("self-update is not supported on %s", goarch)
	}
}

func downloadReleaseArchive(client *http.Client, outputDir string, asset githubReleaseAsset) (string, error) {
	resp, err := client.Get(asset.BrowserDownloadURL)
	if err != nil {
		return "", errors.Wrap(err, "download release asset")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	file, err := os.CreateTemp(outputDir, "tock-update-*.tar.gz")
	if err != nil {
		return "", errors.Wrap(err, "create temporary archive")
	}

	hasher := sha256.New()
	if _, err = io.Copy(io.MultiWriter(file, hasher), resp.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", errors.Wrap(err, "write release archive")
	}

	if err = file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", errors.Wrap(err, "close release archive")
	}

	if err = verifyAssetDigest(asset.Digest, hex.EncodeToString(hasher.Sum(nil))); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}

	return file.Name(), nil
}

func verifyAssetDigest(digest, actual string) error {
	if digest == "" {
		return nil
	}

	algorithm, expected, ok := strings.Cut(digest, ":")
	if !ok {
		return errors.Errorf("unsupported release digest format: %s", digest)
	}

	if algorithm != "sha256" {
		return errors.Errorf("unsupported release digest algorithm: %s", algorithm)
	}

	if !strings.EqualFold(expected, actual) {
		return errors.Errorf("checksum mismatch for release asset")
	}

	return nil
}

func extractBinaryFromArchive(archivePath, outputDir string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", errors.Wrap(err, "open release archive")
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", errors.Wrap(err, "open release archive")
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errors.Wrap(err, "read release archive")
		}

		if header.Typeflag != tar.TypeReg || path.Base(header.Name) != "tock" {
			continue
		}

		extractedPath, err := writeExtractedBinary(outputDir, header.FileInfo().Mode().Perm(), tarReader)
		if err != nil {
			return "", err
		}
		return extractedPath, nil
	}

	return "", errors.New("tock binary not found in release archive")
}

func writeExtractedBinary(outputDir string, mode os.FileMode, reader io.Reader) (string, error) {
	file, err := os.CreateTemp(outputDir, "tock-update-bin-*")
	if err != nil {
		return "", errors.Wrap(err, "create temporary binary")
	}

	if _, err = io.Copy(file, reader); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", errors.Wrap(err, "extract binary from archive")
	}

	if err = file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", errors.Wrap(err, "close extracted binary")
	}

	if mode == 0 {
		mode = 0755
	}

	if err = os.Chmod(file.Name(), mode); err != nil {
		_ = os.Remove(file.Name())
		return "", errors.Wrap(err, "set executable mode")
	}

	return file.Name(), nil
}

func replaceExecutable(targetPath, replacementPath string) error {
	mode := os.FileMode(0755)
	if info, err := os.Stat(targetPath); err == nil {
		mode = info.Mode().Perm()
	}

	if err := os.Chmod(replacementPath, mode); err != nil {
		return errors.Wrap(err, "set executable mode")
	}

	if err := os.Rename(replacementPath, targetPath); err != nil {
		return errors.Wrap(err, "replace executable")
	}

	return nil
}
