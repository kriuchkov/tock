package localization

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoadsCatalogs(t *testing.T) {
	eng, err := New("en_US")
	require.NoError(t, err)
	assert.Equal(t, LanguageEnglish, eng.Language())
	assert.Equal(t, "Activity description", eng.Text("add.flag.description"))
}

func TestDefaultUsesEnglishFallback(t *testing.T) {
	loc := Default()
	assert.Equal(t, LanguageEnglish, loc.Language())
	assert.Equal(t, "Activity description", loc.Text("add.flag.description"))
}

func TestNewFallsBackToEnglishForUnsupportedLanguage(t *testing.T) {
	loc, err := New("de_DE")
	require.NoError(t, err)
	assert.Equal(t, LanguageEnglish, loc.Language())
	assert.Equal(t, "Activity description", loc.Text("add.flag.description"))
}
