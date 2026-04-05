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
	loadBootstrapConfig    = config.Load
)

func newBootstrapLocalizer() *localization.Localizer {
	language := localization.LanguageEnglish

	if cfg, _, err := loadBootstrapConfig(); err == nil && cfg != nil {
		language = localization.DetectLanguage(cfg.Language)
	}

	loc, err := localization.New(language)
	if err == nil {
		return loc
	}

	return localization.MustNew(localization.LanguageEnglish)
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

func defaultText(key string, args ...any) string {
	loc := getBootstrapLocalizer()
	if len(args) == 0 {
		return loc.Text(key)
	}
	return loc.Format(key, args...)
}

func text(cmd *cobra.Command, key string, args ...any) string {
	loc := getLocalizer(cmd)
	if len(args) == 0 {
		return loc.Text(key)
	}
	return loc.Format(key, args...)
}
