package cli

import "testing"

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
