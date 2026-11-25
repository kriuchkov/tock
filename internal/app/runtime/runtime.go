package runtime

import (
	"context"
	"database/sql"
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
	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
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
	// Version is tock's version, for this device's devices/<id>.json in the sync folder.
	Version string
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
	// Sync is the sync service; nil while sync.enabled is off.
	Sync ports.SyncResolver
	// SyncWrites is marked by every write recorded through tock while sync is on, so the delivery
	// layer syncs after the commands that changed data; nil while sync is off.
	SyncWrites *WriteFlag
	// BuildSync builds the sync service as Load does with sync.enabled on, for tock sync init while it
	// is off. The runtime's own services stay unrecorded. Nil when the runtime was not made by Load.
	BuildSync func() (ports.SyncResolver, error)
	// SyncEnabled reads the config again and reports whether sync.enabled is on now, for a process
	// that outlives a change of it, as the menu bar icon does; a config that cannot be read keeps what
	// the runtime was loaded with. Nil when the runtime was not made by Load.
	SyncEnabled func() bool
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

	// With sync off nothing of it is built, so nothing changes and nothing is created on disk, but for
	// the change log a sqlite database kept while it synced: its triggers go, or the log would grow
	// with every write while nothing reads it.
	var wired syncWiring
	if cfg.Sync.Enabled {
		if wired, err = withSync(cfg.Sync, backend, filePath, repos, req.Version); err != nil {
			return nil, err
		}
		repos = wired.repos
	} else if repos.db != nil {
		// A log left in place costs space, not data: the command goes on whatever happens.
		_ = syncbackend.DisableSQLite(ctx, repos.db)
	}
	buildSync := func() (ports.SyncResolver, error) {
		enabled := cfg.Sync
		enabled.Enabled = true
		built, buildErr := withSync(enabled, backend, filePath, repos, req.Version)
		return built.service, buildErr
	}
	if wired.service != nil {
		buildSync = func() (ports.SyncResolver, error) { return wired.service, nil }
	}

	syncEnabled := func() bool {
		now, _, loadErr := config.Load(opts...)
		if loadErr != nil {
			return cfg.Sync.Enabled
		}
		return now.Sync.Enabled
	}

	var activityOptions []activity.Option
	if wired.owners != nil {
		activityOptions = append(activityOptions, activity.WithOwners(wired.owners))
	}
	activityService := activity.NewService(repos.activities, repos.notes, activityOptions...)
	rt := &Runtime{
		ActivityService: activityService,
		ReminderService: reminder.NewService(repos.reminders, activityService),
		Sync:            wired.service,
		SyncWrites:      wired.writes,
		BuildSync:       buildSync,
		SyncEnabled:     syncEnabled,
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
	// remindersPath is the sidecar reminders.yaml of the file-based backends; empty for sqlite.
	remindersPath string
	// db is the sqlite backend's database; nil for the others.
	db *sql.DB
}

func initRepositories(ctx context.Context, backend, filePath string) (repositories, error) {
	sidecarBase := filePath
	if sidecarBase == "" {
		sidecarBase, _ = os.UserHomeDir()
	}
	sidecarDir := filepath.Join(filepath.Dir(sidecarBase), ".tock")
	remindersPath := filepath.Join(sidecarDir, "reminders.yaml")
	notesRepo := notes.NewRepository(filepath.Join(sidecarDir, "notes"))
	remindersRepo := reminders.NewRepository(remindersPath)

	switch backend {
	case backendTodoTXT:
		return repositories{
			activities: todotxt.NewRepository(filePath), notes: notesRepo, reminders: remindersRepo, remindersPath: remindersPath,
		}, nil
	case backendTimewarrior:
		return repositories{
			activities: timewarrior.NewRepository(filePath), notes: notesRepo, reminders: remindersRepo, remindersPath: remindersPath,
		}, nil
	case backendSqlite:
		repo, err := sqlite.NewSQLiteActivityRepository(ctx, filePath)
		if err != nil {
			return repositories{}, errors.Wrap(err, "init sqlite repo")
		}
		reminderRepo, err := sqlite.NewReminderRepository(ctx, repo.DB)
		if err != nil {
			return repositories{}, errors.Wrap(err, "init sqlite reminders repo")
		}
		return repositories{activities: repo, notes: sqlite.NewNotesRepository(repo.DB), reminders: reminderRepo, db: repo.DB}, nil
	default:
		return repositories{
			activities: file.NewRepository(filePath), notes: notesRepo, reminders: remindersRepo, remindersPath: remindersPath,
		}, nil
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
