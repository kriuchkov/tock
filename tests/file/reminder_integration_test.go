package file_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

type listedReminder struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Every  string `json:"every"`
}

func TestFileBackendReminders(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "tock")
	dataPath := filepath.Join(tempDir, "tock.txt")

	buildOut, err := exec.Command("go", "build", "-o", binPath, "../../cmd/tock/main.go").CombinedOutput()
	require.NoError(t, err, "failed to build binary: %s", string(buildOut))

	runTock := func(args ...string) (string, string, error) {
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(),
			"TOCK_BACKEND=file",
			"TOCK_FILE_PATH="+dataPath,
			"TOCK_CHECK_UPDATES=false",
			"HOME="+tempDir,
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		return stdout.String(), stderr.String(), runErr
	}

	// Due a minute ago, so it is overdue from the start.
	stdout, stderr, err := runTock("remind", "add", "Review PRs", "-p", "Tock", "--at", "00:00", "--every", "daily", "--json")
	require.NoError(t, err, stderr)
	var added models.Reminder
	require.NoError(t, json.Unmarshal([]byte(stdout), &added))
	assert.Equal(t, 1, added.ID)

	_, err = os.Stat(filepath.Join(tempDir, ".tock", "reminders.yaml"))
	require.NoError(t, err, "reminders live in the sidecar directory next to the log")

	stdout, stderr, err = runTock("remind", "list", "--overdue", "--json")
	require.NoError(t, err, stderr)
	var overdue []listedReminder
	require.NoError(t, json.Unmarshal([]byte(stdout), &overdue))
	require.Len(t, overdue, 1)
	assert.Equal(t, "Review PRs", overdue[0].Title)
	assert.Equal(t, "daily", overdue[0].Every)

	_, stderr, err = runTock("start", "-p", "Other", "-d", "Something else")
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "1 reminder overdue: Review PRs")

	stdout, stderr, err = runTock("remind", "start", "1", "--json")
	require.NoError(t, err, stderr)
	var activity models.Activity
	require.NoError(t, json.Unmarshal([]byte(stdout), &activity))
	assert.Equal(t, "Tock", activity.Project)
	assert.Equal(t, "Review PRs", activity.Description)

	stdout, stderr, err = runTock("current", "--json")
	require.NoError(t, err, stderr)
	var running []models.Activity
	require.NoError(t, json.Unmarshal([]byte(stdout), &running))
	require.Len(t, running, 1)
	assert.Equal(t, "Review PRs", running[0].Description, "starting a reminder stops the running activity")

	stdout, stderr, err = runTock("remind", "list", "--overdue", "--json")
	require.NoError(t, err, stderr)
	require.NoError(t, json.Unmarshal([]byte(stdout), &overdue))
	assert.Empty(t, overdue, "the tracked occurrence is done")

	_, stderr, err = runTock("remind", "rm", "1", "-y")
	require.NoError(t, err, stderr)
	stdout, stderr, err = runTock("remind", "list", "--all", "--json")
	require.NoError(t, err, stderr)
	assert.JSONEq(t, "[]", stdout)
}
