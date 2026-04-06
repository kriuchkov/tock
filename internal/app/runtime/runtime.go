package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-faster/errors"
	"github.com/spf13/viper"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	"github.com/kriuchkov/tock/internal/adapters/repositories/notes"
	"github.com/kriuchkov/tock/internal/adapters/repositories/sqlite"
	"github.com/kriuchkov/tock/internal/adapters/repositories/timewarrior"
	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/activity"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	backendTodoTXT     = "todotxt"
	backendTimewarrior = "timewarrior"
	backendSqlite      = "sqlite"
)

type Request struct {
	Backend    string
	FilePath   string
	ConfigPath string
	Language   string
}

type contextKey struct{}

type Runtime struct {
	ActivityService ports.ActivityResolver
	Backend         string
	DataPath        string
	Config          *config.Config
	Viper           *viper.Viper
	TimeFormatter   *timeutil.Formatter
	Localizer       *localization.Localizer
}

func (rt *Runtime) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, rt)
}

func FromContext(ctx context.Context) (*Runtime, bool) {
	rt, ok := ctx.Value(contextKey{}).(*Runtime)
	return rt, ok && rt != nil
}

func Load(ctx context.Context, req Request) (*Runtime, error) {
	var opts []config.Option
	if req.ConfigPath != "" {
		opts = append(opts, config.WithConfigFile(req.ConfigPath))
	}

	cfg, loadedViper, err := config.Load(opts...)
	if err != nil {
		return nil, errors.Wrap(err, "load config")
	}

	loc, err := localization.New(firstNonEmpty(req.Language, cfg.Language))
	if err != nil {
		return nil, errors.Wrap(err, "init localization")
	}

	backend := strings.TrimSpace(req.Backend)
	if backend == "" {
		backend = cfg.Backend
	}

	filePath := resolveFilePath(backend, req.FilePath, cfg)
	repo, notesRepo, err := initRepositories(ctx, backend, filePath)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		ActivityService: activity.NewService(repo, notesRepo),
		Backend:         backend,
		DataPath:        filePath,
		Config:          cfg,
		Viper:           loadedViper,
		TimeFormatter:   timeutil.NewFormatter(cfg.TimeFormat),
		Localizer:       loc,
	}, nil
}

func (rt *Runtime) DefaultExportDir() (string, error) {
	dataPath := strings.TrimSpace(rt.DataPath)

	if dataPath == "" {
		if rt.Backend == backendTimewarrior {
			return "", errors.New("timewarrior data path is empty")
		}
		return "", errors.New("activity file path is empty")
	}

	if rt.Backend == backendTimewarrior {
		return dataPath, nil
	}
	return filepath.Dir(dataPath), nil
}

func LoadCompletionService(ctx context.Context, req Request) (ports.ActivityResolver, error) {
	deps, err := Load(ctx, req)
	if err != nil {
		return nil, err
	}
	return deps.ActivityService, nil
}

func initRepositories(ctx context.Context, backend, filePath string) (ports.ActivityRepository, ports.NotesRepository, error) {
	notesBase := filePath
	if notesBase == "" {
		notesBase, _ = os.UserHomeDir()
	}
	notesPath := filepath.Join(filepath.Dir(notesBase), ".tock", "notes")

	switch backend {
	case backendTodoTXT:
		return todotxt.NewRepository(filePath), notes.NewRepository(notesPath), nil
	case backendTimewarrior:
		return timewarrior.NewRepository(filePath), notes.NewRepository(notesPath), nil
	case backendSqlite:
		repo, err := sqlite.NewSQLiteActivityRepository(ctx, filePath)
		if err != nil {
			return nil, nil, errors.Wrap(err, "init sqlite repo")
		}
		return repo, sqlite.NewNotesRepository(repo.DB), nil
	default:
		return file.NewRepository(filePath), notes.NewRepository(notesPath), nil
	}
}

func resolveFilePath(backend, filePath string, cfg *config.Config) string {
	if filePath != "" {
		return filePath
	}

	switch backend {
	case backendTodoTXT:
		return cfg.TodoTXT.Path
	case backendTimewarrior:
		return cfg.Timewarrior.DataPath
	case backendSqlite:
		return cfg.Sqlite.Path
	default:
		return cfg.File.Path
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
