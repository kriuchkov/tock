package commands

import (
	"sync"

	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
)

var (
	bootstrapLocalizerOnce sync.Once
	bootstrapLocalizer     *localization.Localizer
)

func newBootstrapLocalizerForLanguage(language string) *localization.Localizer {
	loc, err := localization.New(language)
	if err == nil {
		return loc
	}

	return localization.MustNew(localization.LanguageEnglish)
}

func loadBootstrapLanguage() string {
	if cfg, _, err := config.Load(); err == nil && cfg != nil {
		return localization.DetectLanguage(cfg.Language)
	}

	return localization.LanguageEnglish
}

func newBootstrapLocalizer() *localization.Localizer {
	return newBootstrapLocalizerForLanguage(loadBootstrapLanguage())
}

func getBootstrapLocalizer() *localization.Localizer {
	bootstrapLocalizerOnce.Do(func() {
		bootstrapLocalizer = newBootstrapLocalizer()
	})
	return bootstrapLocalizer
}

func getLocalizer(cmd *cobra.Command) *localization.Localizer {
	if cmd != nil {
		if rt, ok := appruntime.FromContext(cmd.Context()); ok && rt.Localizer != nil {
			return rt.Localizer
		}
	}
	return getBootstrapLocalizer()
}

func formatText(loc *localization.Localizer, key string, args ...any) string {
	if len(args) == 0 {
		return loc.Text(key)
	}
	return loc.Format(key, args...)
}

func defaultText(key string, args ...any) string {
	return formatText(getBootstrapLocalizer(), key, args...)
}

func text(cmd *cobra.Command, key string, args ...any) string {
	return formatText(getLocalizer(cmd), key, args...)
}
