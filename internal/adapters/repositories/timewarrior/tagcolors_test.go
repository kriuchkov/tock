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
		wantFG string
		wantBG string
		wantOK bool
	}{
		{"color2", "2", "", true},
		{"color196", "196", "", true},
		{"color0", "0", "", true},
		{"color255", "255", "", true},
		// rgb3-digit: 16 + 36*r + 6*g + b
		{"rgb135", "75", "", true},  // 16 + 36 + 18 + 5
		{"rgb000", "16", "", true},  // 16 + 0 + 0 + 0
		{"rgb555", "231", "", true}, // 16 + 180 + 30 + 5
		{"rgb345", "153", "", true}, // 16 + 108 + 24 + 5
		// grayN: 232 + N
		{"gray0", "232", "", true},
		{"gray8", "240", "", true},
		{"gray23", "255", "", true},
		{"red", "1", "", true},
		{"green", "2", "", true},
		{"blue", "4", "", true},
		{"black", "0", "", true},
		{"white", "7", "", true},
		{"yellow", "3", "", true},
		{"magenta", "5", "", true},
		{"cyan", "6", "", true},
		// With modifiers — foreground should still be extracted
		{"bold color3 on_color8", "3", "8", true},
		{"black on yellow", "0", "3", true},
		{"black on magenta", "0", "5", true},
		{"underline color5", "5", "", true},
		// Background-only spec — no foreground, but bg is still parsed
		{"on_color3", "", "3", false},
		{"on yellow", "", "3", false},
		// Unknown token
		{"foobar", "", "", false},
		// Empty
		{"", "", "", false},
	}

	for _, tt := range tests {
		fg, bg, ok := parseTimewarriorColor(tt.spec)
		assert.Equal(t, tt.wantOK, ok, "spec=%q ok mismatch", tt.spec)
		assert.Equal(t, tt.wantFG, fg, "spec=%q fg mismatch", tt.spec)
		assert.Equal(t, tt.wantBG, bg, "spec=%q bg mismatch", tt.spec)
	}
}

func TestParseTagColors(t *testing.T) {
	// Build a temporary directory structure mirroring ~/.timewarrior/data
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))

	cfgContent := `# TimeWarrior config
tags.work.color = color2
tags.personal.color = red
tags.focus.color = bold color5 on_color0
tags.grayed.color = gray8
tags.vividred.color = rgb500
tags..color = color3
something_else = color1
`
	require.NoError(t, os.WriteFile(filepath.Join(base, "timewarrior.cfg"), []byte(cfgContent), 0o600))

	colors := ParseTagColors(dataDir, "")
	require.NotNil(t, colors)

	assert.Equal(t, "2", colors["work"].FG)
	assert.Empty(t, colors["work"].BG)
	assert.Equal(t, "1", colors["personal"].FG)
	assert.Equal(t, "5", colors["focus"].FG)
	assert.Equal(t, "0", colors["focus"].BG)      // on_color0
	assert.Equal(t, "240", colors["grayed"].FG)   // gray8 = 232+8
	assert.Equal(t, "196", colors["vividred"].FG) // rgb500 = 16+36*5+0+0

	// Empty tag name should be skipped
	_, hasEmpty := colors[""]
	assert.False(t, hasEmpty)

	// Non-tags.*.color entries should be ignored
	_, hasSomething := colors["something_else"]
	assert.False(t, hasSomething)
}

func TestParseTagColors_MissingFile(t *testing.T) {
	// Supply an explicit nonexistent path to prevent XDG fallback from
	// picking up the real ~/.config/timewarrior/timewarrior.cfg on the host.
	colors := ParseTagColors("/nonexistent/path/data", "/nonexistent/custom.cfg")
	assert.Nil(t, colors)
}

func TestParseTagColors_ExplicitCfgPath(t *testing.T) {
	base := t.TempDir()

	cfgFile := filepath.Join(base, "custom.cfg")
	require.NoError(t, os.WriteFile(cfgFile, []byte("tags.work.color = color3\n"), 0o600))

	// dataDir is irrelevant when explicit path is given
	colors := ParseTagColors("/not/a/real/data/dir", cfgFile)
	require.NotNil(t, colors)
	assert.Equal(t, "3", colors["work"].FG)
}
