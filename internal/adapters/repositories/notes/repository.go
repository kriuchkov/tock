package notes

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"gopkg.in/yaml.v3"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const fileExtension = ".txt"

type repository struct {
	basePath string
}

func NewRepository(basePath string) ports.NotesRepository {
	return &repository{basePath: basePath}
}

type noteData struct {
	Tags []string `yaml:"tags,omitempty"`
}

func (r *repository) notePath(activityID string, date time.Time) string {
	return filepath.Join(r.basePath, date.Format("2006-01-02"), activityID+fileExtension)
}

func (r *repository) Save(_ context.Context, activityID string, date time.Time, notes string, tags []string) error {
	var buf bytes.Buffer
	if len(tags) > 0 {
		buf.WriteString("---\n")
		if err := yaml.NewEncoder(&buf).Encode(noteData{Tags: tags}); err != nil {
			return errors.Wrap(err, "encode tags")
		}
		buf.WriteString("---\n")
	}
	buf.WriteString(notes)

	if err := fileio.WriteAtomic(r.notePath(activityID, date), buf.Bytes()); err != nil {
		return errors.Wrap(err, "write notes")
	}
	return nil
}

func (r *repository) Delete(_ context.Context, activityID string, date time.Time) error {
	if err := os.Remove(r.notePath(activityID, date)); err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "remove file")
	}
	return nil
}

func (r *repository) Get(ctx context.Context, activityID string, date time.Time) (string, []string, error) {
	notes, tags, _, err := r.Lookup(ctx, activityID, date)
	return notes, tags, err
}

// Lookup returns the notes and tags of an activity, and whether it has a notes file: cleared notes
// leave an empty one.
func (r *repository) Lookup(_ context.Context, activityID string, date time.Time) (string, []string, bool, error) {
	f, err := os.Open(r.notePath(activityID, date))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, false, nil
		}
		return "", nil, false, errors.Wrap(err, "open file")
	}

	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return "", nil, false, errors.Wrap(err, "read file")
	}

	sContent := string(content)

	var tags []string
	var notes = sContent

	if strings.HasPrefix(sContent, "---\n") {
		parts := strings.SplitN(sContent, "---\n", 3)
		if len(parts) == 3 {
			var data noteData
			if err = yaml.Unmarshal([]byte(parts[1]), &data); err == nil {
				tags = data.Tags
			}
			notes = parts[2]
		}
	}
	return strings.TrimSpace(notes), tags, true, nil
}
