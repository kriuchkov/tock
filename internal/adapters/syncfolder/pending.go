package syncfolder

import (
	"context"
	"path"
	"slices"
)

// Pending returns the files that exist only as iCloud placeholders, as slash-separated paths in
// the folder (format.json, devices/<id>.json, journal/<device>/<file>), sorted, and asks iCloud for
// them. Only names the folder uses count: a valid device ID, a segment name.
func (f *folder) Pending(ctx context.Context) ([]string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	var pending []string
	if isPlaceholder(f.path(formatFile)) {
		pending = append(pending, formatFile)
	}

	devices, err := placeholders(f.path(devicesDir))
	if err != nil {
		return nil, err
	}
	for _, name := range devices {
		if _, ok := deviceFileID(name); ok {
			pending = append(pending, path.Join(devicesDir, name))
		}
	}

	journals, err := readDir(f.path(journalDir))
	if err != nil {
		return nil, err
	}
	for _, entry := range journals {
		if !entry.IsDir() || !isDeviceID(entry.Name()) {
			continue
		}
		segments, listErr := placeholders(f.journalPath(entry.Name()))
		if listErr != nil {
			return nil, listErr
		}
		for _, name := range segments {
			if _, _, ok := parseSegmentName(name); ok {
				pending = append(pending, path.Join(journalDir, entry.Name(), name))
			}
		}
	}

	slices.Sort(pending)
	if len(pending) > 0 {
		paths := make([]string, len(pending))
		for i, rel := range pending {
			paths[i] = f.path(rel)
		}
		f.download(ctx, paths)
	}
	return pending, nil
}

// placeholders returns the names of the files in dir that only have a placeholder there.
func placeholders(dir string) ([]string, error) {
	entries, err := readDir(dir)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		present[entry.Name()] = true
	}
	var names []string
	for _, entry := range entries {
		if target, ok := placeholderTarget(entry.Name()); ok && !present[target] {
			names = append(names, target)
		}
	}
	return names, nil
}
