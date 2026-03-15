package sqlite_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteIntegration(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "tock")
	dbPath := filepath.Join(tempDir, "tock.db")

	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/tock/main.go")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build binary: %s", string(buildOut))

	runTock := func(args ...string) (string, string, error) {
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(),
			"TOCK_BACKEND=sqlite",
			"TOCK_SQLITE_PATH="+dbPath,
			"HOME="+tempDir,
		)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	// 1. Start an activity
	stdout, stderr, err := runTock(
		"start",
		"-p",
		"IntegrationProject",
		"-d",
		"Testing Tock",
		"--note",
		"Testing the sqlite backend",
		"--tag",
		"integration",
	)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Started")

	// 2. Verify it's running
	stdout, _, err = runTock("current")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Testing Tock")

	// 3. Stop it
	stdout, stderr, err = runTock("stop")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Stopped")

	// 4. Verify it's saved correctly
	stdout, stderr, err = runTock("last", "--json")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "\"project\": \"IntegrationProject\"")
	assert.Contains(t, stdout, "\"description\": \"Testing Tock\"")
	// lowercase tags output or Notes checking
	assert.Contains(t, strings.ToLower(stdout), "integration")

	// 5. Continue the activity
	stdout, stderr, err = runTock("continue")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Started")
	assert.Contains(t, stdout, "IntegrationProject")

	// 6. Provide specific time for stopping it
	stdout, stderr, err = runTock("stop", "-t", "23:59")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Stopped")

	// 7. Add a historical offline activity
	// using explicit past dates to ensure it won't clash with today's metrics depending on when tests are run
	stdout, stderr, err = runTock(
		"add",
		"-p",
		"PastProject",
		"-d",
		"Old Task",
		"-s",
		"2020-01-01 10:00",
		"-e",
		"2020-01-01 12:00",
	)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Added")
	assert.Contains(t, stdout, "PastProject")

	// 8. Generate a report for that specific day
	stdout, stderr, err = runTock("report", "--date", "2020-01-01", "--json")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "\"project\": \"PastProject\"")
	assert.Contains(t, stdout, "\"duration\": \"02:00:00\"") // since it was 10:00 to 12:00

	// 9. Remove the last activity (the continued one from today)
	stdout, stderr, err = runTock("remove", "-y") // automatically remove last without wizard
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Activity removed")

	// 10. Check that we only have one IntegrationProject event left (the first one)
	stdout, stderr, err = runTock("report", "--today", "--json")
	require.NoError(t, err, stderr)

	// We expect the original activity from today to still be there
	assert.Contains(t, stdout, "\"project\": \"IntegrationProject\"")
}
