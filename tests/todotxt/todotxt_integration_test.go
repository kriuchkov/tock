package todotxt_test

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

func TestTodoTXTIntegration(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "tock")
	todoPath := filepath.Join(tempDir, "todo.txt")

	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/tock/main.go")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build binary: %s", string(buildOut))

	runTock := func(args ...string) (string, string, error) {
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(),
			"TOCK_BACKEND=todotxt",
			"TOCK_TODOTXT_PATH="+todoPath,
			"HOME="+tempDir,
		)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	stdout, stderr, err := runTock(
		"start",
		"-p",
		"Integration Project",
		"-d",
		"Testing TodoTXT",
		"--note",
		"Testing the todotxt backend",
		"--tag",
		"integration",
	)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Started")

	stdout, _, err = runTock("current")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Testing TodoTXT")

	stdout, stderr, err = runTock("stop")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Stopped")

	stdout, stderr, err = runTock("last", "--json")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "\"project\": \"Integration Project\"")
	assert.Contains(t, stdout, "\"description\": \"Testing TodoTXT\"")
	assert.Contains(t, stdout, "\"notes\": \"Testing the todotxt backend\"")
	assert.Contains(t, stdout, "\"integration\"")

	content, err := os.ReadFile(todoPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "tock_start:")
	assert.Contains(t, string(content), "tock_end:")
	assert.Contains(t, string(content), "tock_project:Integration+Project")
	assert.Contains(t, string(content), "tock_desc:Testing+TodoTXT")
	assert.Contains(t, string(content), "@integration")

	stdout, stderr, err = runTock(
		"add",
		"-p",
		"Past Project",
		"-d",
		"Old Task",
		"-s",
		"2020-01-01 10:00",
		"-e",
		"2020-01-01 12:00",
	)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Added")
	assert.Contains(t, stdout, "Past Project")

	stdout, stderr, err = runTock("report", "--date", "2020-01-01", "--json")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "\"project\": \"Past Project\"")
	assert.Contains(t, stdout, "\"duration\": \"02:00:00\"")
}

func TestTodoTXTReadsPlainTodoTXTData(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "tock")
	todoPath := filepath.Join(tempDir, "todo.txt")

	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/tock/main.go")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "Failed to build binary: %s", string(buildOut))

	plainTodoTXT := strings.Join([]string{
		"x 2020-01-02 2020-01-01 Review PR +Work @github",
		"(A) 2020-01-03 Plan sprint +Work @desk",
		"Call Mom +Family @phone",
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(todoPath, []byte(plainTodoTXT), 0644))

	runTock := func(args ...string) (string, string, error) {
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(),
			"TOCK_BACKEND=todotxt",
			"TOCK_TODOTXT_PATH="+todoPath,
			"HOME="+tempDir,
		)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	stdout, stderr, err := runTock("last", "--json")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "\"project\": \"Work\"")
	assert.Contains(t, stdout, "\"description\": \"Plan sprint\"")
	assert.Contains(t, stdout, "\"desk\"")

	stdout, stderr, err = runTock("report", "--date", "2020-01-01", "--json")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "\"project\": \"Work\"")
	assert.Contains(t, stdout, "\"description\": \"Review PR\"")
	assert.Contains(t, stdout, "\"github\"")

	stdout, stderr, err = runTock("last")
	require.NoError(t, err, stderr)
	assert.NotContains(t, stdout, "Call Mom")
}
