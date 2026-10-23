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

	_, stderr, err = runTock("note", "kept across edits", "--json")
	require.NoError(t, err, stderr)
	_, stderr, err = runTock("tag", "kept-tag", "--json")
	require.NoError(t, err, stderr)

	stdout, stderr, err = runTock("edit", "-d", "JSON contract fixed", "--json")
	require.NoError(t, err, stderr)
	editedLast := decodeActivity(t, stdout)
	assert.Equal(t, "JSON contract fixed", editedLast.Description)
	assert.Equal(t, "kept across edits", editedLast.Notes, "notes must survive editing the last activity")
	assert.Equal(t, []string{"kept-tag"}, editedLast.Tags, "tags must survive editing the last activity")

	stdout, stderr, err = runTock("continue", "--json")
	require.NoError(t, err, stderr)
	continued := decodeActivity(t, stdout)
	assert.Equal(t, "Integration Project", continued.Project)
	assert.Equal(t, "JSON contract fixed", continued.Description)
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

	stdout, stderr, err = runTock(
		"edit", "2020-01-01-01",
		"-d", "Corrected Task",
		"--end", "2020-01-01 13:00",
		"--tag", "fixed,billable",
		"--note", "end time was wrong",
		"--json",
	)
	require.NoError(t, err, stderr)
	edited := decodeActivity(t, stdout)
	assert.Equal(t, "Past Project", edited.Project)
	assert.Equal(t, "Corrected Task", edited.Description)
	require.NotNil(t, edited.EndTime)
	assert.Equal(t, 3*time.Hour, edited.EndTime.Sub(edited.StartTime))
	assert.Equal(t, []string{"fixed", "billable"}, edited.Tags)
	assert.Equal(t, "end time was wrong", edited.Notes)

	stdout, stderr, err = runTock("edit", "2020-01-01-01", "--end", "2020-01-01 12:30", "--json")
	require.NoError(t, err, stderr)
	kept := decodeActivity(t, stdout)
	assert.Equal(t, []string{"fixed", "billable"}, kept.Tags, "tags must survive an edit that does not mention them")
	assert.Equal(t, "end time was wrong", kept.Notes, "notes must survive an edit that does not mention them")

	stdout, stderr, err = runTock("edit", "2020-01-01-01", "--day", "2020-01-02", "--json")
	require.NoError(t, err, stderr)
	moved := decodeActivity(t, stdout)
	assert.Equal(t, "2020-01-02", moved.StartTime.Format(time.DateOnly))
	require.NotNil(t, moved.EndTime)
	assert.Equal(t, 2*time.Hour+30*time.Minute, moved.EndTime.Sub(moved.StartTime))

	stdout, stderr, err = runTock("export", "--date", "2020-01-01", "--format", "json", "--stdout")
	require.NoError(t, err, stderr)
	assert.Empty(t, decodeActivities(t, stdout))

	stdout, stderr, err = runTock("export", "--date", "2020-01-02", "--format", "json", "--stdout")
	require.NoError(t, err, stderr)
	movedDay := decodeActivities(t, stdout)
	require.Len(t, movedDay, 1)
	assert.Equal(t, "Corrected Task", movedDay[0].Description)
	assert.Equal(t, "end time was wrong", movedDay[0].Notes)

	stdout, stderr, err = runTock("edit", "2020-01-02-01", "--day", "2020-01-01", "--json")
	require.NoError(t, err, stderr)
	movedBack := decodeActivity(t, stdout)
	assert.Equal(t, "2020-01-01", movedBack.StartTime.Format(time.DateOnly))

	stdout, stderr, err = runTock("remove", "2020-01-01-01", "--yes", "--json")
	require.NoError(t, err, stderr)
	removed := decodeActivity(t, stdout)
	assert.Equal(t, "Past Project", removed.Project)
	assert.Equal(t, "Corrected Task", removed.Description)

	stdout, stderr, err = runTock("export", "--date", "2020-01-01", "--format", "json", "--stdout")
	require.NoError(t, err, stderr)
	exported = decodeActivities(t, stdout)
	assert.Empty(t, exported)

	// Regression for issue #99: tags must not survive removal when a new
	// activity is created with the same start time.
	stdout, stderr, err = runTock(
		"add",
		"-p", "Tagged Project",
		"-d", "First",
		"-s", "2020-02-01 09:00",
		"-e", "2020-02-01 10:00",
		"--tag", "test",
		"--json",
	)
	require.NoError(t, err, stderr)
	tagged := decodeActivity(t, stdout)
	assert.Equal(t, []string{"test"}, tagged.Tags)

	stdout, stderr, err = runTock("remove", "2020-02-01-01", "--yes", "--json")
	require.NoError(t, err, stderr)
	decodeActivity(t, stdout)

	stdout, stderr, err = runTock(
		"add",
		"-p", "Tagged Project",
		"-d", "Second",
		"-s", "2020-02-01 09:00",
		"-e", "2020-02-01 09:15",
		"--json",
	)
	require.NoError(t, err, stderr)
	decodeActivity(t, stdout)

	stdout, stderr, err = runTock("export", "--date", "2020-02-01", "--format", "json", "--stdout")
	require.NoError(t, err, stderr)
	exported = decodeActivities(t, stdout)
	require.Len(t, exported, 1)
	assert.Equal(t, "Second", exported[0].Description)
	assert.Empty(t, exported[0].Tags)
}
