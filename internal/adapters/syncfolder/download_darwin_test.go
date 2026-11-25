//go:build darwin

package syncfolder

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOnlyICloudDriveFilesAreAskedFor(t *testing.T) {
	assert.False(t, inICloudDrive(filepath.Join(t.TempDir(), "sync", "format.json")))
	assert.True(t, inICloudDrive("home/Library/Mobile Documents/iCloud~com~kriuchkov~tock/Documents/sync/format.json"))
	assert.True(t, inICloudDrive("home/Library/Mobile Documents/com~apple~CloudDocs/tock/journal/0123456789abcdef/2026-11.jsonl"))
}
