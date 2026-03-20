package file_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestFileBackendJSONCommands(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "tock")
	dataPath := filepath.Join(tempDir, "tock.txt")

	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/tock/main.go")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "failed to build binary: %s", string(buildOut))

	runTock := func(args ...string) (string, string, error) {
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(),
			"TOCK_BACKEND=file",
			"TOCK_FILE_PATH="+dataPath,
			"TOCK_CHECK_UPDATES=false",
			"HOME="+tempDir,
		)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	decodeActivity := func(t *testing.T, output string) models.Activity {
		t.Helper()
		var activity models.Activity
		require.NoError(t, json.Unmarshal([]byte(output), &activity))
		return activity
	}

	decodeActivities := func(t *testing.T, output string) []models.Activity {
		t.Helper()
		var activities []models.Activity
		require.NoError(t, json.Unmarshal([]byte(output), &activities))
		return activities
	}

	stdout, stderr, err := runTock("start", "-p", "Integration Project", "-d", "JSON contract", "--json")
	require.NoError(t, err, stderr)
	started := decodeActivity(t, stdout)
	assert.Equal(t, "Integration Project", started.Project)
	assert.Equal(t, "JSON contract", started.Description)
	assert.Nil(t, started.EndTime)

	stdout, stderr, err = runTock("current", "--json")
	require.NoError(t, err, stderr)
	current := decodeActivities(t, stdout)
	require.Len(t, current, 1)
	assert.Equal(t, "Integration Project", current[0].Project)
	assert.Equal(t, "JSON contract", current[0].Description)

	stdout, stderr, err = runTock("stop", "--json")
	require.NoError(t, err, stderr)
	stopped := decodeActivity(t, stdout)
	assert.Equal(t, "Integration Project", stopped.Project)
	require.NotNil(t, stopped.EndTime)

	stdout, stderr, err = runTock("continue", "--json")
	require.NoError(t, err, stderr)
	continued := decodeActivity(t, stdout)
	assert.Equal(t, "Integration Project", continued.Project)
	assert.Equal(t, "JSON contract", continued.Description)
	assert.Nil(t, continued.EndTime)

	stdout, stderr, err = runTock("stop", "--json")
	require.NoError(t, err, stderr)
	decodeActivity(t, stdout)

	stdout, stderr, err = runTock(
		"add",
		"-p", "Past Project",
		"-d", "Historical Task",
		"-s", "2020-01-01 10:00",
		"-e", "2020-01-01 12:00",
		"--json",
	)
	require.NoError(t, err, stderr)
	added := decodeActivity(t, stdout)
	assert.Equal(t, "Past Project", added.Project)
	assert.Equal(t, "Historical Task", added.Description)
	require.NotNil(t, added.EndTime)
	assert.Equal(t, 2*time.Hour, added.EndTime.Sub(added.StartTime))

	stdout, stderr, err = runTock("last", "--json", "-n", "10")
	require.NoError(t, err, stderr)
	last := decodeActivities(t, stdout)
	assert.NotEmpty(t, last)
	assert.Contains(t, stdout, "\"project\": \"Integration Project\"")
	assert.Contains(t, stdout, "\"project\": \"Past Project\"")

	stdout, stderr, err = runTock("export", "--date", "2020-01-01", "--format", "json", "--stdout")
	require.NoError(t, err, stderr)
	exported := decodeActivities(t, stdout)
	require.Len(t, exported, 1)
	assert.Equal(t, "Past Project", exported[0].Project)
	assert.Equal(t, "Historical Task", exported[0].Description)

	stdout, stderr, err = runTock("remove", "2020-01-01-01", "--yes", "--json")
	require.NoError(t, err, stderr)
	removed := decodeActivity(t, stdout)
	assert.Equal(t, "Past Project", removed.Project)
	assert.Equal(t, "Historical Task", removed.Description)

	stdout, stderr, err = runTock("export", "--date", "2020-01-01", "--format", "json", "--stdout")
	require.NoError(t, err, stderr)
	exported = decodeActivities(t, stdout)
	assert.Empty(t, exported)
}
