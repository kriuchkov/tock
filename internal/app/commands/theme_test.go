package commands

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"

	"github.com/kriuchkov/tock/internal/config"
)

func TestGetThemeNames(t *testing.T) {
	cases := map[string]Theme{
		"dark":       DarkTheme(),
		"default":    DarkTheme(),
		"light":      LightTheme(),
		"ansi":       ANSIDarkTheme(),
		"ansi_dark":  ANSIDarkTheme(),
		"ansi_light": ANSILightTheme(),
		themeMocha:   MochaTheme(),
	}
	for name, want := range cases {
		assert.Equal(t, want, GetTheme(config.ThemeConfig{Name: name}), name)
	}
}

func TestCustomThemeStartsFromDark(t *testing.T) {
	got := GetTheme(config.ThemeConfig{Name: themeCustom, Highlight: "212"})

	want := DarkTheme()
	want.Highlight = lipgloss.Color("212")
	assert.Equal(t, want, got)
}

func TestCustomThemeBase(t *testing.T) {
	got := GetTheme(config.ThemeConfig{
		Name:    themeCustom,
		Base:    themeMocha,
		Primary: "33",
		Surface: "236",
		OK:      "120",
		Warn:    "208",
	})

	want := MochaTheme()
	want.Primary = lipgloss.Color("33")
	want.Surface = lipgloss.Color("236")
	want.OK = lipgloss.Color("120")
	want.Warn = lipgloss.Color("208")
	assert.Equal(t, want, got)
}

func TestCustomThemeUnknownBaseFallsBackToDark(t *testing.T) {
	for _, base := range []string{"", themeCustom, "nope"} {
		assert.Equal(t, DarkTheme(), CustomTheme(config.ThemeConfig{Base: base}), base)
	}
}

func TestCustomThemeOnLightTerminalReversesSelection(t *testing.T) {
	light := customTheme(config.ThemeConfig{Text: "0"}, false)
	assert.Empty(t, light.Surface, "dark's Surface under black text would be unreadable")
	assert.True(t, withSelection(lipgloss.NewStyle(), light).GetReverse())

	assert.Equal(t, lipgloss.Color("254"), customTheme(config.ThemeConfig{Surface: "254"}, false).Surface)
	assert.Equal(t, MochaTheme().Surface, customTheme(config.ThemeConfig{Base: themeMocha}, false).Surface, "a named base is kept")
	assert.Equal(t, DarkTheme().Surface, customTheme(config.ThemeConfig{}, true).Surface)
}

func TestWithSelection(t *testing.T) {
	mocha := MochaTheme()
	assert.True(t, withSelection(lipgloss.NewStyle(), ANSILightTheme()).GetReverse(), "16-color themes reverse the row")
	assert.Equal(t, lipgloss.TerminalColor(mocha.Surface), withSelection(lipgloss.NewStyle(), mocha).GetBackground())
}
