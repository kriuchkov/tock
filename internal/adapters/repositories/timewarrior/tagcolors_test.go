package timewarrior

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTimewarriorColor(t *testing.T) {
	tests := []struct {
		spec   string
		want   string
		wantOK bool
	}{
		{"color2", "2", true},
		{"color196", "196", true},
		{"color0", "0", true},
		{"color255", "255", true},
		{"rgb/1/3/5", "75", true}, // 16 + 36*1 + 6*3 + 5 = 75
		{"red", "1", true},
		{"green", "2", true},
		{"blue", "4", true},
		{"black", "0", true},
		{"white", "7", true},
		{"yellow", "3", true},
		{"magenta", "5", true},
		{"cyan", "6", true},
		// With modifiers — foreground should still be extracted
		{"bold color3 on_color8", "3", true},
		{"underline color5", "5", true},
		// Background-only spec — no foreground
		{"on_color3", "", false},
		// Unknown token
		{"foobar", "", false},
		// Empty
		{"", "", false},
	}

	for _, tt := range tests {
		got, ok := parseTimewarriorColor(tt.spec)
		assert.Equal(t, tt.wantOK, ok, "spec=%q ok mismatch", tt.spec)
		if tt.wantOK {
			assert.Equal(t, tt.want, got, "spec=%q color mismatch", tt.spec)
		}
	}
}

func TestParseTagColors(t *testing.T) {
	// Build a temporary directory structure mirroring ~/.timewarrior/data
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))

	cfgContent := `# TimeWarrior config
color.tag.work=color2
color.tag.personal=red
color.tag.focus=bold color5 on_color0
color.tag.=color3
color.something_else=color1
`
	require.NoError(t, os.WriteFile(filepath.Join(base, "timewarrior.cfg"), []byte(cfgContent), 0o600))

	colors := ParseTagColors(dataDir)
	require.NotNil(t, colors)

	assert.Equal(t, "2", colors["work"])
	assert.Equal(t, "1", colors["personal"])
	assert.Equal(t, "5", colors["focus"])

	// Empty tag name should be skipped
	_, hasEmpty := colors[""]
	assert.False(t, hasEmpty)

	// Non-color.tag.* entries should be ignored
	_, hasSomething := colors["something_else"]
	assert.False(t, hasSomething)
}

func TestParseTagColors_MissingFile(t *testing.T) {
	colors := ParseTagColors("/nonexistent/path/data")
	assert.Nil(t, colors)
}
