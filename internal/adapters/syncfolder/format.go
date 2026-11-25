package syncfolder

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
)

const (
	formatName    = "tock-sync"
	formatVersion = 1
	// formatContent is the format.json this version writes.
	formatContent = `{"format":"tock-sync","version":1}` + "\n"
)

// EnsureFormat reads format.json and checks that this version knows its format. A missing one is
// written first, creating the folder, unless its placeholder shows it exists but is not downloaded:
// it may name another version, so EnsureFormat asks for it and fails without writing.
func (f *folder) EnsureFormat(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	path := f.path(formatFile)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		return checkFormat(data)
	case !errors.Is(err, fs.ErrNotExist):
		return errors.Wrap(err, "read format.json")
	case exists(placeholderPath(path)):
		f.requestDownload(ctx, path)
		return errors.Wrap(errNotDownloaded, "format.json")
	}

	created, err := createFile(path, []byte(formatContent))
	if err != nil {
		return errors.Wrap(err, "write format.json")
	}
	if created {
		return nil
	}
	// Another process or device made it first: check what it wrote.
	if data, err = os.ReadFile(path); err != nil {
		return errors.Wrap(err, "read format.json")
	}
	return checkFormat(data)
}

// checkFormat returns coreErrors.ErrSyncFormatUnsupported unless data is the format.json of format
// tock-sync, version 1.
func checkFormat(data []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return errors.Wrap(err, "parse format.json")
	}
	var (
		name    string
		version float64
	)
	nameErr := json.Unmarshal(obj["format"], &name)
	versionErr := json.Unmarshal(obj["version"], &version)
	if nameErr != nil || versionErr != nil || name != formatName || version != formatVersion {
		return errors.Wrapf(coreErrors.ErrSyncFormatUnsupported, "format.json holds %s", bytes.TrimSpace(data))
	}
	return nil
}

// createFile writes data to path unless something is there already, creating its folder, and
// reports whether it did. The file appears whole: it is written to a temporary sibling and linked
// into place, which fails when path exists. Where hard links are not supported it is renamed into
// place after checking that path is still free.
func createFile(path string, data []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return false, errors.Wrap(err, "create folder")
	}
	file, err := createTemp(path)
	if err != nil {
		return false, err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err = writeAndClose(file, data); err != nil {
		return false, errors.Wrap(err, "write temporary file")
	}

	err = os.Link(tmp, path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrExist):
		return false, nil
	case exists(path):
		return false, nil
	}
	if err = os.Rename(tmp, path); err != nil {
		return false, errors.Wrap(err, "rename temporary file")
	}
	return true, nil
}

// createTemp creates a new hidden temporary file beside path.
func createTemp(path string) (*os.File, error) {
	file, err := os.CreateTemp(filepath.Dir(path), hiddenStart+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, errors.Wrap(err, "create temporary file")
	}
	return file, nil
}

// writeAndClose writes data to file with one write call, sets its mode, syncs it to disk and
// closes it.
func writeAndClose(file *os.File, data []byte) error {
	_, err := file.Write(data)
	if err == nil {
		err = file.Chmod(fileMode)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
