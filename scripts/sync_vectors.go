// sync_vectors writes the sync test vectors of docs/sync.md as JSON files, one per vector, into
// <out>/hlc, <out>/merge and <out>/repeat. tock-ios runs them against its Swift code.
//
//	go run ./scripts/sync_vectors.go -out <dir>   (or: make sync-vectors OUT=<dir>)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal/journaltest"
)

func main() {
	out := flag.String("out", "", "directory to write the vectors to")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/sync_vectors.go -out <dir>")
		os.Exit(2)
	}
	if err := write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "sync_vectors:", err)
		os.Exit(1)
	}
}

func write(out string) error {
	files := make(map[string]map[string]any)
	add := func(kind, name string, vector any) {
		if files[kind] == nil {
			files[kind] = make(map[string]any)
		}
		files[kind][name+".json"] = vector
	}
	for _, v := range journaltest.HLCVectors() {
		add("hlc", v.File, v)
	}
	for _, v := range journaltest.MergeVectors() {
		add("merge", v.File, v)
	}
	for _, v := range journaltest.RepeatVectors() {
		add("repeat", v.File, v)
	}

	for kind, vectors := range files {
		dir := filepath.Join(out, kind)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return errors.Wrap(err, "create directory")
		}
		// Vectors that were renamed or removed must not linger.
		stale, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return errors.Wrap(err, "list old vectors")
		}
		for _, path := range stale {
			if err = os.Remove(path); err != nil {
				return errors.Wrap(err, "remove old vector")
			}
		}
		for name, vector := range vectors {
			data, marshalErr := json.MarshalIndent(vector, "", "  ")
			if marshalErr != nil {
				return errors.Wrapf(marshalErr, "encode %s/%s", kind, name)
			}
			if err = os.WriteFile(filepath.Join(dir, name), append(data, '\n'), 0o600); err != nil {
				return errors.Wrap(err, "write vector")
			}
		}
	}
	return nil
}
