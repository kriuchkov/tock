package syncfolder

import (
	"cmp"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	deviceVersion = 1
	// seenInterval is how long a seen written to devices/<id>.json stays good enough: a device that
	// syncs every minute rewrites the file, and iCloud uploads it, at most once in this time.
	seenInterval = time.Hour
)

// deviceFile is devices/<id>.json, with its keys in the order of docs/sync.md.
type deviceFile struct {
	V        int    `json:"v"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	App      string `json:"app"`
	Seen     string `json:"seen,omitempty"`
}

// Devices returns every devices/<id>.json with a valid device ID that parses and names its own ID,
// by ID. Missing name, platform and app read as empty, and a seen that is not an RFC 3339 instant
// as zero; a file with a key of the wrong type is left out.
func (f *folder) Devices(ctx context.Context) ([]ports.SyncDevice, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	entries, err := readDir(f.path(devicesDir))
	if err != nil {
		return nil, err
	}
	var devices []ports.SyncDevice
	for _, entry := range entries {
		id, ok := deviceFileID(entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(f.devicePath(id))
		if readErr != nil {
			continue
		}
		if device, parsed := parseDevice(id, data); parsed {
			devices = append(devices, device)
		}
	}
	slices.SortFunc(devices, func(a, b ports.SyncDevice) int { return cmp.Compare(a.ID, b.ID) })
	return devices, nil
}

// WriteDevice writes devices/<device.ID>.json atomically, with "v": 1 and seen in UTC at whole
// seconds. The file is left as it is when it already holds the same name, platform and app with a
// seen less than an hour before device.Seen, so a device that syncs often does not make iCloud
// upload it each time. While the file is not downloaded yet, WriteDevice asks for it and writes
// nothing: the next sync writes it.
func (f *folder) WriteDevice(ctx context.Context, device ports.SyncDevice) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if !isDeviceID(device.ID) {
		return errors.Errorf("invalid device ID %q", device.ID)
	}
	target := f.devicePath(device.ID)
	current, err := os.ReadFile(target)
	switch {
	case err == nil:
		if written, ok := parseDevice(device.ID, current); ok && upToDate(written, device) {
			return nil
		}
	case errors.Is(err, fs.ErrNotExist) && exists(placeholderPath(target)):
		f.requestDownload(ctx, target)
		return nil
	}

	data, err := encodeDevice(device)
	if err != nil {
		return err
	}
	if err = fileio.WriteAtomic(target, data); err != nil {
		return errors.Wrapf(err, "write devices/%s%s", device.ID, deviceExt)
	}
	return nil
}

// devicePath returns devices/<id>.json.
func (f *folder) devicePath(id string) string {
	return f.path(path.Join(devicesDir, id+deviceExt))
}

// deviceFileID returns <id> of a file named <id>.json with a valid device ID.
func deviceFileID(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, deviceExt)
	return id, ok && isDeviceID(id)
}

// encodeDevice returns devices/<id>.json for device, ending in "\n"; seen is left out when zero.
func encodeDevice(device ports.SyncDevice) ([]byte, error) {
	file := deviceFile{V: deviceVersion, ID: device.ID, Name: device.Name, Platform: device.Platform, App: device.App}
	if !device.Seen.IsZero() {
		file.Seen = device.Seen.UTC().Truncate(time.Second).Format(time.RFC3339)
	}
	data, err := json.Marshal(file)
	if err != nil {
		return nil, errors.Wrap(err, "encode device")
	}
	return append(data, '\n'), nil
}

// parseDevice reads devices/<id>.json. Keys are matched exactly and unknown ones ignored.
func parseDevice(id string, data []byte) (ports.SyncDevice, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return ports.SyncDevice{}, false
	}
	var device ports.SyncDevice
	if json.Unmarshal(obj["id"], &device.ID) != nil || device.ID != id {
		return ports.SyncDevice{}, false
	}
	for key, dst := range map[string]*string{"name": &device.Name, "platform": &device.Platform, "app": &device.App} {
		if raw, ok := obj[key]; ok && json.Unmarshal(raw, dst) != nil {
			return ports.SyncDevice{}, false
		}
	}
	var seen string
	if json.Unmarshal(obj["seen"], &seen) == nil {
		if instant, err := time.Parse(time.RFC3339, seen); err == nil {
			device.Seen = instant.UTC()
		}
	}
	return device, true
}

// upToDate reports whether written, read from the device's file, describes device well enough to
// leave the file as it is.
func upToDate(written, device ports.SyncDevice) bool {
	if written.Name != device.Name || written.Platform != device.Platform || written.App != device.App {
		return false
	}
	if device.Seen.IsZero() || written.Seen.IsZero() {
		return device.Seen.IsZero() && written.Seen.IsZero()
	}
	age := device.Seen.Truncate(time.Second).Sub(written.Seen)
	return age >= 0 && age < seenInterval
}
