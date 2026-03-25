package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseArchiveName(t *testing.T) {
	testCases := []struct {
		name    string
		goos    string
		goarch  string
		want    string
		wantErr bool
	}{
		{name: "linux amd64", goos: "linux", goarch: "amd64", want: "tock_Linux_x86_64.tar.gz"},
		{name: "darwin arm64", goos: "darwin", goarch: "arm64", want: "tock_Darwin_arm64.tar.gz"},
		{name: "unsupported os", goos: "windows", goarch: "amd64", wantErr: true},
		{name: "unsupported arch", goos: "linux", goarch: "386", wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := releaseArchiveName(tc.goos, tc.goarch)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFindReleaseArchive(t *testing.T) {
	release := githubRelease{
		Assets: []githubReleaseAsset{
			{Name: "checksums.txt"},
			{Name: "tock_Linux_x86_64.tar.gz", BrowserDownloadURL: "https://example.com/tock_Linux_x86_64.tar.gz"},
		},
	}

	asset, err := findReleaseArchive(release, "linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if asset.Name != "tock_Linux_x86_64.tar.gz" {
		t.Fatalf("unexpected asset name %q", asset.Name)
	}
}

func TestDownloadReleaseArchive(t *testing.T) {
	payload := []byte("archive payload")
	checksum := sha256.Sum256(payload)
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytesReader(payload)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}),
	}

	asset := githubReleaseAsset{
		Name:               "tock_Linux_x86_64.tar.gz",
		BrowserDownloadURL: "https://example.com/tock_Linux_x86_64.tar.gz",
		Digest:             "sha256:" + hex.EncodeToString(checksum[:]),
	}

	archivePath, err := downloadReleaseArchive(context.Background(), client, t.TempDir(), asset)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	if string(content) != string(payload) {
		t.Fatalf("unexpected archive content %q", string(content))
	}
}

func TestExtractBinaryFromArchive(t *testing.T) {
	tempDir := t.TempDir()
	archivePath := filepath.Join(tempDir, "tock.tar.gz")

	if err := writeTestArchive(archivePath, "tock", []byte("binary-data")); err != nil {
		t.Fatalf("unexpected archive write error: %v", err)
	}

	binaryPath, err := extractBinaryFromArchive(archivePath, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	if string(content) != "binary-data" {
		t.Fatalf("unexpected binary content %q", string(content))
	}
}

func writeTestArchive(archivePath, binaryName string, payload []byte) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	header := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(payload)),
	}

	if err = tarWriter.WriteHeader(header); err != nil {
		return err
	}

	_, err = tarWriter.Write(payload)
	return err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func bytesReader(payload []byte) io.Reader {
	return bytes.NewReader(payload)
}
