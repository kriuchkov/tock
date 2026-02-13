package notes

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"gopkg.in/yaml.v3"

	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	fileExtension = ".txt"
	directoryMode = 0750
	fileMode      = 0644
)

type repository struct {
	basePath string
}

func NewRepository(basePath string) ports.NotesRepository {
	return &repository{basePath: basePath}
}

type noteData struct {
	Tags []string `yaml:"tags,omitempty"`
}

func (r *repository) Save(_ context.Context, activityID string, date time.Time, notes string, tags []string) error {
	dateDir := date.Format("2006-01-02")

	dirPath := filepath.Join(r.basePath, dateDir)
	if err := os.MkdirAll(dirPath, directoryMode); err != nil {
		return errors.Wrap(err, "mkdir all")
	}

	filePath := filepath.Join(dirPath, fmt.Sprintf("%s%s", activityID, fileExtension))

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer f.Close()

	if len(tags) > 0 {
		if _, err = fmt.Fprintf(f, "---\n"); err != nil {
			return err
		}

		data := noteData{Tags: tags}
		if err = yaml.NewEncoder(f).Encode(data); err != nil {
			return errors.Wrap(err, "encode tags")
		}

		if _, err = fmt.Fprintf(f, "---\n"); err != nil {
			return errors.Wrap(err, "write tags")
		}
	}

	if _, err = fmt.Fprintf(f, "%s", notes); err != nil {
		return errors.Wrap(err, "write notes")
	}
	return nil
}

func (r *repository) Get(_ context.Context, activityID string, date time.Time) (string, []string, error) {
	dateDir := date.Format("2006-01-02")
	filePath := filepath.Join(r.basePath, dateDir, fmt.Sprintf("%s%s", activityID, fileExtension))

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, nil
		}
		return "", nil, errors.Wrap(err, "open file")
	}

	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return "", nil, errors.Wrap(err, "read file")
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
	return strings.TrimSpace(notes), tags, nil
}
