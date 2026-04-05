package commands

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/config"
)

func TestNewBootstrapLocalizerFallsBackToEnglishForUnsupportedLanguage(t *testing.T) {
	originalLoadBootstrapConfig := loadBootstrapConfig
	loadBootstrapConfig = func(_ ...config.Option) (*config.Config, *viper.Viper, error) {
		return &config.Config{Language: "de"}, viper.New(), nil
	}
	t.Cleanup(func() {
		loadBootstrapConfig = originalLoadBootstrapConfig
	})

	loc := newBootstrapLocalizer()
	require.NotNil(t, loc)
	assert.Equal(t, "eng", loc.Language())
	assert.Equal(t, "Activity description", loc.Text("add.flag.description"))
}
