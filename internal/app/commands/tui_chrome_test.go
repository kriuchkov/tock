package commands

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestFooterKeepsQuitAndTabsWhenNarrow(t *testing.T) {
	ui := newChromeStyles(DarkTheme())
	tabs := tabStrip{names: []string{"one", "two", "three", "four"}}
	keys := tabs.withTabs([]footerKey{
		{key: "←→", action: "day"}, {key: "↑↓", action: "row"}, {key: "[]", action: "jump"},
		{key: "j/k", action: "select"}, {key: "q", action: "quit"},
	}, "tabs")

	assert.Equal(t, " ←→ day   ↑↓ row   [] jump   j/k select   1–4 tabs   q quit", ui.renderFooter(keys, "", 0))
	assert.Equal(t, " ←→ day   ↑↓ row   [] jump   1–4 tabs   q quit", ui.renderFooter(keys, "", 46),
		"the keys in the middle go first")
	narrow := ui.renderFooter(keys, "", 30)
	assert.Contains(t, narrow, "1–4 tabs   q quit")
	assert.LessOrEqual(t, lipgloss.Width(narrow), 30)
}

func TestHeaderFillsTheWidth(t *testing.T) {
	assert.Equal(t, 1, lipgloss.Width(glyphBrand), "the mark takes one column, as terminals draw it")
	ui := newChromeStyles(DarkTheme())
	header := ui.renderHeader(screenHeader{tabs: []string{"one", "two"}, title: "November 2026", hint: "←→"}, 100)
	for line := range strings.SplitSeq(header, "\n") {
		assert.Equal(t, 100, lipgloss.Width(line), line)
	}
}

func TestMoveCursor(t *testing.T) {
	tests := []struct {
		key  string
		from int
		want int
	}{
		{"down", 3, 4}, {"k", 3, 2}, {"k", 0, 0},
		{"pgdown", 3, 13}, {"f", 25, 29}, {"pgup", 13, 3}, {"b", 5, 0},
		{"ctrl+d", 3, 8}, {"u", 8, 3},
		{"home", 17, 0}, {"G", 3, 29}, {"end", 0, 29},
	}
	for _, tt := range tests {
		got, ok := moveCursor(tt.key, tt.from, 30, 10)
		assert.True(t, ok, tt.key)
		assert.Equal(t, tt.want, got, "%s from %d", tt.key, tt.from)
	}
	_, ok := moveCursor("x", 3, 30, 10)
	assert.False(t, ok, "other keys are left to the screen")
	got, _ := moveCursor("G", 0, 0, 10)
	assert.Equal(t, 0, got, "an empty list keeps the cursor at 0")
}
