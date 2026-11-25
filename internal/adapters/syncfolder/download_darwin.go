//go:build darwin

package syncfolder

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// brctl is the macOS tool for iCloud Drive; brctl download asks iCloud for a file.
	brctl = "/usr/bin/brctl"
	// iCloudDrive is in the path of every folder iCloud Drive syncs on a Mac.
	iCloudDrive = "/Library/Mobile Documents/"
	// downloadTimeout bounds the wait for the download requests, not for the downloads.
	downloadTimeout = 5 * time.Second
)

// requestDownloads asks iCloud Drive to download files, by absolute path, with brctl download, and
// waits for the requests only: the files arrive later. Files outside iCloud Drive are left alone,
// and a failed request is ignored, since the next sync asks again.
func requestDownloads(ctx context.Context, paths []string) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	var requests []*exec.Cmd
	for _, file := range paths {
		if !inICloudDrive(file) {
			continue
		}
		request := exec.CommandContext(ctx, brctl, "download", file)
		if request.Start() == nil {
			requests = append(requests, request)
		}
	}
	for _, request := range requests {
		_ = request.Wait()
	}
}

// inICloudDrive reports whether file is in a folder iCloud Drive syncs, following symlinks of its
// folder.
func inICloudDrive(file string) bool {
	if dir, err := filepath.EvalSymlinks(filepath.Dir(file)); err == nil {
		file = filepath.Join(dir, filepath.Base(file))
	}
	return strings.Contains(file, iCloudDrive)
}
