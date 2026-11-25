package sync_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

// device is one tock installation: its own home, config, backend and state.db, sharing only the sync
// folder with the others.
type device struct {
	t    *testing.T
	bin  string
	home string
	env  []string
	data string
}

// newDevice makes a home under root with the given backend; nothing of the caller's tock setup leaks
// in, since every TOCK_ variable is replaced.
func newDevice(t *testing.T, bin, root, backend string) *device {
	t.Helper()
	home := filepath.Join(root, "home")
	dataDir := filepath.Join(root, "data")
	require.NoError(t, os.MkdirAll(home, 0o750))
	require.NoError(t, os.MkdirAll(dataDir, 0o750))

	env := []string{"HOME=" + home, "TOCK_BACKEND=" + backend, "TOCK_CHECK_UPDATES=false"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TOCK_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}

	d := &device{t: t, bin: bin, home: home}
	switch backend {
	case "sqlite":
		d.data = filepath.Join(dataDir, "tock.db")
		env = append(env, "TOCK_SQLITE_PATH="+d.data)
	default:
		d.data = filepath.Join(dataDir, "tock.txt")
		env = append(env, "TOCK_FILE_PATH="+d.data)
	}
	d.env = env
	return d
}

// try runs tock and returns its stdout, stderr and error.
func (d *device) try(args ...string) (string, string, error) {
	cmd := exec.Command(d.bin, args...)
	cmd.Env = d.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// run runs tock, fails the test on an error, and checks that the sync after the command did not warn.
func (d *device) run(args ...string) string {
	d.t.Helper()
	stdout, stderr, err := d.try(args...)
	require.NoError(d.t, err, "tock %s: %s", strings.Join(args, " "), stderr)
	assert.NotContains(d.t, stderr, "sync failed", "tock %s", strings.Join(args, " "))
	return stdout
}

func (d *device) sync() syncReport {
	d.t.Helper()
	var report syncReport
	require.NoError(d.t, json.Unmarshal([]byte(d.run("sync", "--json")), &report))
	return report
}

func (d *device) running() []models.Activity {
	d.t.Helper()
	var activities []models.Activity
	require.NoError(d.t, json.Unmarshal([]byte(d.run("current", "--json")), &activities))
	return activities
}

// day returns the activities started on date, YYYY-MM-DD.
func (d *device) day(date string) []reportActivity {
	d.t.Helper()
	var activities []reportActivity
	require.NoError(d.t, json.Unmarshal([]byte(d.run("report", "--date", date, "--json")), &activities))
	return activities
}

type reportActivity struct {
	Project     string `json:"project"`
	Description string `json:"description"`
	Duration    string `json:"duration"`
}

type syncReport struct {
	Device  string `json:"device"`
	Written int    `json:"written"`
	Applied int    `json:"applied"`
}

type syncStatus struct {
	Enabled     bool   `json:"enabled"`
	Initialized bool   `json:"initialized"`
	Dir         string `json:"dir"`
	Device      string `json:"device"`
	Devices     []struct {
		ID       string `json:"id"`
		Platform string `json:"platform"`
	} `json:"devices"`
}

// buildTock builds the tock binary into root.
func buildTock(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(root, "tock")
	buildOut, err := exec.Command("go", "build", "-o", bin, "../../cmd/tock/main.go").CombinedOutput()
	require.NoError(t, err, "build tock: %s", buildOut)
	return bin
}

// TestSyncTwoMacsThroughOneFolder syncs a Mac with the file backend and one with sqlite through one
// folder, as two Macs share the iCloud Drive folder: what one does shows up on the other.
func TestSyncTwoMacsThroughOneFolder(t *testing.T) {
	root := t.TempDir()
	bin := buildTock(t, root)

	folder := filepath.Join(root, "iCloud Drive", "tock", "sync")
	mac := newDevice(t, bin, filepath.Join(root, "mac"), "file")
	box := newDevice(t, bin, filepath.Join(root, "box"), "sqlite")

	// Before init sync is off and changes nothing.
	_, stderr, err := mac.try("sync")
	require.Error(t, err)
	assert.Contains(t, stderr, "tock sync init")
	mac.run("add", "-p", "Backend", "-d", "API work", "-s", "2026-11-02 09:00", "-e", "2026-11-02 10:30")
	assert.NoDirExists(t, folder)

	mac.run("sync", "init", "--dir", folder)
	assert.FileExists(t, filepath.Join(folder, "format.json"))
	box.run("sync", "init", "--dir", folder)

	// init turned sync on in the config and sent what the Mac had.
	assert.Equal(t, []reportActivity{{Project: "Backend", Description: "API work", Duration: "01:30:00"}}, box.day("2026-11-02"))

	var status syncStatus
	require.NoError(t, json.Unmarshal([]byte(box.run("sync", "status", "--json")), &status))
	assert.True(t, status.Enabled)
	assert.True(t, status.Initialized)
	assert.Equal(t, folder, status.Dir)
	assert.Len(t, status.Devices, 2)
	macID := mac.sync().Device
	assert.NotEqual(t, macID, status.Device, "each home is a device of its own")

	// Start on one, see it running on the other; stop there, see it stopped here.
	mac.run("start", "-p", "Tock", "-d", "Sync")
	box.sync()
	running := box.running()
	require.Len(t, running, 1)
	assert.Equal(t, "Tock", running[0].Project)
	assert.Equal(t, "Sync", running[0].Description)

	box.run("stop")
	mac.sync()
	assert.Empty(t, mac.running())

	startOnBothBeforeSeeingTheOther(t, folder, mac, box)

	// An edit on sqlite reaches the file backend.
	box.run("edit", "2026-11-02-01", "-d", "API review", "--duration", "2h")
	mac.sync()
	assert.Equal(t, []reportActivity{{Project: "Backend", Description: "API review", Duration: "02:00:00"}}, mac.day("2026-11-02"))

	// A line written into tock.txt by hand is found by the next sync.
	appendLine(t, mac.data, "2026-11-03 08:00 - 2026-11-03 09:15 | Docs | Written by hand")
	assert.Positive(t, mac.sync().Written)
	box.sync()
	assert.Equal(t, []reportActivity{{Project: "Docs", Description: "Written by hand", Duration: "01:15:00"}}, box.day("2026-11-03"))

	// Reminders sync too, each device numbering them on its own.
	box.run("remind", "add", "Renew domain", "-p", "Ops", "--on", "2026-12-01")
	mac.sync()
	assert.Contains(t, mac.run("remind", "list", "--all"), "Renew domain")

	// Nothing new: a sync on either side reads and writes nothing more.
	assert.Zero(t, mac.sync().Applied)
	assert.Zero(t, box.sync().Applied)
}

// startOnBothBeforeSeeingTheOther starts an activity on each device before either has seen the other's
// start: one activity runs on both, and the earlier one stops where the later one starts. The box sees
// both first, in the sync after its start, and writes that stop as the auto_end of the Mac's activity;
// the Mac, which owns it, then leaves it as it is. The starts are minutes apart, since the file backend
// keeps minutes.
func startOnBothBeforeSeeingTheOther(t *testing.T, folder string, mac, box *device) {
	t.Helper()
	now := time.Now()
	alpha, beta := now.Add(-3*time.Minute), now.Add(-2*time.Minute)
	if alpha.Day() != now.Day() {
		t.Log("too close to midnight for start -t; skipping the concurrent starts")
		return
	}

	mac.run("start", "-p", "Alpha", "-d", "Started on the Mac", "-t", alpha.Format("15:04"))
	box.run("start", "-p", "Beta", "-d", "Started on the box", "-t", beta.Format("15:04"))
	assert.Len(t, setsOf(t, folder, box.sync().Device, "auto_end"), 1, "the box writes the auto_end of the Mac's activity")
	assert.Zero(t, mac.sync().Written, "the Mac leaves the auto_end as it is")
	for _, d := range []*device{mac, box} {
		running := d.running()
		require.Len(t, running, 1)
		assert.Equal(t, "Beta", running[0].Project)
	}

	box.run("stop")
	mac.sync()
	assert.Empty(t, mac.running())
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(line + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}
