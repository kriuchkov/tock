package mcpserver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2026, time.November, 18, 15, 30, 0, 0, time.Local)
	fallback := now.Add(time.Minute)

	cases := map[string]time.Time{
		"":                          fallback,
		"09:05":                     time.Date(2026, time.November, 18, 9, 5, 0, 0, time.Local),
		"2026-11-17 18:45":          time.Date(2026, time.November, 17, 18, 45, 0, 0, time.Local),
		"2026-11-17T18:45":          time.Date(2026, time.November, 17, 18, 45, 0, 0, time.Local),
		"2026-11-17T18:45:00Z":      time.Date(2026, time.November, 17, 18, 45, 0, 0, time.UTC),
		"2026-11-17T18:45:00+03:00": time.Date(2026, time.November, 17, 15, 45, 0, 0, time.UTC),
	}
	for input, want := range cases {
		got, err := parseTime(input, now, fallback)
		require.NoError(t, err, input)
		assert.True(t, got.Equal(want), "%q: got %s, want %s", input, got, want)
	}

	_, err := parseTime("yesterday at 5", now, fallback)
	require.Error(t, err)
}

func TestCleanTags(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, cleanTags([]string{" a ", "", "b", "  "}))
	assert.Nil(t, cleanTags(nil))
}
