package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBootstrapLocalizerFallsBackToEnglishForUnsupportedLanguage(t *testing.T) {
	loc := newBootstrapLocalizerForLanguage("de")
	require.NotNil(t, loc)
	assert.Equal(t, "eng", loc.Language())
	assert.Equal(t, "Activity description", loc.Text("add.flag.description"))
}
