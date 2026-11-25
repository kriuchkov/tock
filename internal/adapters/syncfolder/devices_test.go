package syncfolder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/ports"
)

func TestDevicesReadsValidFiles(t *testing.T) {
	f, _ := newTestFolder(t)
	writeFile(t, f, "devices/"+devA+".json",
		`{"v":1,"id":"`+devA+`","name":"MacBook","platform":"macos","app":"tock 2.4.0","seen":"2026-11-18T22:00:00+03:00","extra":1}`)
	writeFile(t, f, "devices/"+devB+".json", `{"id":"`+devB+`","seen":"yesterday"}`)
	writeFile(t, f, "devices/1111111111111111.json", `{"id":"2222222222222222","name":"names another ID"}`)
	writeFile(t, f, "devices/3333333333333333.json", `{"id":"3333333333333333","name":7}`)
	writeFile(t, f, "devices/4444444444444444.json", `{"ID":"4444444444444444"}`)
	writeFile(t, f, "devices/5555555555555555.json", `not json`)
	writeFile(t, f, "devices/.6666666666666666.json.icloud", "")
	writeFile(t, f, "devices/MacBook.json", `{"id":"MacBook"}`)

	devices, err := f.Devices(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ports.SyncDevice{
		{ID: devB},
		{
			ID: devA, Name: "MacBook", Platform: "macos", App: "tock 2.4.0",
			Seen: time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC),
		},
	}, devices)
}

func TestDevicesOfMissingFolder(t *testing.T) {
	f, _ := newTestFolder(t)

	devices, err := f.Devices(t.Context())
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestWriteDeviceWritesFile(t *testing.T) {
	f, _ := newTestFolder(t)
	seen := time.Date(2026, 11, 18, 22, 0, 0, 500, time.FixedZone("MSK", 3*60*60))
	device := ports.SyncDevice{ID: devA, Name: "MacBook", Platform: "macos", App: "tock 2.4.0", Seen: seen}

	require.NoError(t, f.WriteDevice(t.Context(), device))

	assertJSONLine(t,
		`{"v":1,"id":"7f3a9c2e5b1d4a60","name":"MacBook","platform":"macos","app":"tock 2.4.0","seen":"2026-11-18T19:00:00Z"}`,
		readFile(t, f, "devices/"+devA+".json"))
	assert.Equal(t, []string{"devices/" + devA + ".json"}, tree(t, f.dir), "no temporary file is left behind")

	devices, err := f.Devices(t.Context())
	require.NoError(t, err)
	device.Seen = time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	assert.Equal(t, []ports.SyncDevice{device}, devices)
}

func TestWriteDeviceLeavesOutZeroSeen(t *testing.T) {
	f, _ := newTestFolder(t)

	require.NoError(t, f.WriteDevice(t.Context(), ports.SyncDevice{ID: devB, Name: "iPhone", Platform: "ios", App: "Tock 1.0"}))
	assertJSONLine(t, `{"v":1,"id":"0123456789abcdef","name":"iPhone","platform":"ios","app":"Tock 1.0"}`,
		readFile(t, f, "devices/"+devB+".json"))
}

func TestWriteDeviceRefreshesSeenHourly(t *testing.T) {
	f, _ := newTestFolder(t)
	start := time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	device := ports.SyncDevice{ID: devA, Name: "MacBook", Platform: "macos", App: "tock 2.4.0", Seen: start}
	rel := "devices/" + devA + ".json"
	seenIn := func() time.Time {
		written, ok := parseDevice(devA, []byte(readFile(t, f, rel)))
		require.True(t, ok)
		return written.Seen
	}
	require.NoError(t, f.WriteDevice(t.Context(), device))

	device.Seen = start.Add(59 * time.Minute)
	require.NoError(t, f.WriteDevice(t.Context(), device))
	assert.Equal(t, start, seenIn(), "a seen less than an hour old stays")

	device.Seen = start.Add(time.Hour)
	require.NoError(t, f.WriteDevice(t.Context(), device))
	assert.Equal(t, start.Add(time.Hour), seenIn(), "an hour later seen is refreshed")

	device.Seen = start.Add(time.Hour + time.Minute)
	device.App = "tock 2.5.0"
	require.NoError(t, f.WriteDevice(t.Context(), device))
	assert.Equal(t, start.Add(time.Hour+time.Minute), seenIn(), "a new app version is written at once")

	device.Seen = start
	require.NoError(t, f.WriteDevice(t.Context(), device))
	assert.Equal(t, start, seenIn(), "a seen in the future of the clock is replaced")
}

func TestWriteDeviceWaitsForPlaceholder(t *testing.T) {
	f, asked := newTestFolder(t)
	writeFile(t, f, "devices/."+devA+".json.icloud", "")

	require.NoError(t, f.WriteDevice(t.Context(), ports.SyncDevice{ID: devA, Name: "MacBook"}))
	assert.Equal(t, []string{"devices/." + devA + ".json.icloud"}, tree(t, f.dir))
	assert.Equal(t, []string{f.path("devices/" + devA + ".json")}, asked.asked())
}

func TestWriteDeviceRejectsBadID(t *testing.T) {
	f, _ := newTestFolder(t)

	require.Error(t, f.WriteDevice(t.Context(), ports.SyncDevice{ID: "../format"}))
	assert.NoDirExists(t, f.dir)
}
