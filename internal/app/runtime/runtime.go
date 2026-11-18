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
	"github.com/kriuchkov/tock/internal/adapters/repositories/reminders"
	"github.com/kriuchkov/tock/internal/adapters/repositories/sqlite"
	"github.com/kriuchkov/tock/internal/adapters/repositories/timewarrior"
	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/activity"
	"github.com/kriuchkov/tock/internal/services/reminder"
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
	ReminderService ports.ReminderResolver
	Backend         string
	DataPath        string
	Config          *config.Config
	Viper           *viper.Viper
	TimeFormatter   *timeutil.Formatter
	Localizer       *localization.Localizer
	TagColors       map[string]models.TagColor
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
	repos, err := initRepositories(ctx, backend, filePath)
	if err != nil {
		return nil, err
	}

	activityService := activity.NewService(repos.activities, repos.notes)
	rt := &Runtime{
		ActivityService: activityService,
		ReminderService: reminder.NewService(repos.reminders, activityService),
		Backend:         backend,
		DataPath:        filePath,
		Config:          cfg,
		Viper:           loadedViper,
		TimeFormatter:   timeutil.NewFormatter(cfg.TimeFormat),
		Localizer:       loc,
		TagColors: buildTagColors(
			cfg.Theme.TagColors,
			backend,
			filePath,
			cfg.Timewarrior.ConfigPath,
			cfg.Timewarrior.UseTockTagColors,
		),
	}
	return rt, nil
}

// WorkingHours returns the configured end of the working day; enabled is false when it isn't set.
func (rt *Runtime) WorkingHours() (models.WorkingHours, bool, error) {
	if rt.Config == nil {
		return models.WorkingHours{}, false, nil
	}
	cfg := rt.Config.WorkingHours
	if !cfg.Enabled || strings.TrimSpace(cfg.StopAt) == "" {
		return models.WorkingHours{}, false, nil
	}

	formatter := rt.TimeFormatter
	if formatter == nil {
		formatter = timeutil.NewFormatter("24")
	}
	stopAt, err := formatter.ParseTime(cfg.StopAt)
	if err != nil {
		return models.WorkingHours{}, false, errors.Wrap(err, "parse working hours stop time")
	}
	weekdays, err := models.ParseWorkingWeekdays(cfg.Weekdays)
	if err != nil {
		return models.WorkingHours{}, false, err
	}
	return models.WorkingHours{StopHour: stopAt.Hour(), StopMinute: stopAt.Minute(), Weekdays: weekdays}, true, nil
}

// buildTagColors merges per-tag colors from two sources. Config-defined colors
// are the base; backend-specific colors (e.g. TimeWarrior tags.*.color) are
// overlaid on top so that the backend's own palette takes precedence unless
// useTockTagColors is enabled.
// twCfgPath is an optional explicit path to timewarrior.cfg (empty = auto-detect).
func buildTagColors(cfgColors map[string]string, backend, dataPath, twCfgPath string, useTockTagColors bool) map[string]models.TagColor {
	var result map[string]models.TagColor

	if len(cfgColors) > 0 {
		result = make(map[string]models.TagColor, len(cfgColors))
		for tag, color := range cfgColors {
			if color != "" {
				result[tag] = models.TagColor{FG: color}
			}
		}
	}

	if backend == backendTimewarrior && !useTockTagColors {
		twColors := timewarrior.ParseTagColors(dataPath, twCfgPath)
		if len(twColors) > 0 {
			if result == nil {
				result = make(map[string]models.TagColor, len(twColors))
			}
			for tag, tc := range twColors {
				result[tag] = models.TagColor{FG: tc.FG, BG: tc.BG}
			}
		}
	}
	return result
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

type repositories struct {
	activities ports.ActivityRepository
	notes      ports.NotesRepository
	reminders  ports.ReminderRepository
}

func initRepositories(ctx context.Context, backend, filePath string) (repositories, error) {
	sidecarBase := filePath
	if sidecarBase == "" {
		sidecarBase, _ = os.UserHomeDir()
	}
	sidecarDir := filepath.Join(filepath.Dir(sidecarBase), ".tock")
	notesRepo := notes.NewRepository(filepath.Join(sidecarDir, "notes"))
	remindersRepo := reminders.NewRepository(filepath.Join(sidecarDir, "reminders.yaml"))

	switch backend {
	case backendTodoTXT:
		return repositories{activities: todotxt.NewRepository(filePath), notes: notesRepo, reminders: remindersRepo}, nil
	case backendTimewarrior:
		return repositories{activities: timewarrior.NewRepository(filePath), notes: notesRepo, reminders: remindersRepo}, nil
	case backendSqlite:
		repo, err := sqlite.NewSQLiteActivityRepository(ctx, filePath)
		if err != nil {
			return repositories{}, errors.Wrap(err, "init sqlite repo")
		}
		reminderRepo, err := sqlite.NewReminderRepository(ctx, repo.DB)
		if err != nil {
			return repositories{}, errors.Wrap(err, "init sqlite reminders repo")
		}
		return repositories{activities: repo, notes: sqlite.NewNotesRepository(repo.DB), reminders: reminderRepo}, nil
	default:
		return repositories{activities: file.NewRepository(filePath), notes: notesRepo, reminders: remindersRepo}, nil
	}
}

func resolveFilePath(backend, filePath string, cfg *config.Config) string {
	if filePath != "" {
		return expandTilde(filePath)
	}

	switch backend {
	case backendTodoTXT:
		return expandTilde(cfg.TodoTXT.Path)
	case backendTimewarrior:
		return expandTilde(cfg.Timewarrior.DataPath)
	case backendSqlite:
		return expandTilde(cfg.Sqlite.Path)
	default:
		return expandTilde(cfg.File.Path)
	}
}

// expandTilde replaces a leading "~" with the current user's home directory.
func expandTilde(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
