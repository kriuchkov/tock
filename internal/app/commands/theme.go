package commands

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/kriuchkov/tock/internal/config"
)

// TagColorStyle holds the lipgloss foreground and optional background colors for a tag.
type TagColorStyle struct {
	FG lipgloss.Color
	BG lipgloss.Color
}

// Theme defines the color palette for the application. Each field is a role named
// after its theme.* config key: Primary is the info color (charts, projects),
// Secondary the danger color (errors, overdue), SubText the muted one (times, labels, keys),
// Faint is for lines only and Highlight is the accent. Surface is the background of a
// selected row; empty means reverse video, which the 16-color themes use.
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Text      lipgloss.Color
	SubText   lipgloss.Color
	Faint     lipgloss.Color
	Highlight lipgloss.Color
	Tag       lipgloss.Color
	Reminder  lipgloss.Color
	Surface   lipgloss.Color
	OK        lipgloss.Color
	Warn      lipgloss.Color
	TagColors map[string]TagColorStyle
}

// DarkTheme returns the standard 256-color theme for dark backgrounds.
func DarkTheme() Theme {
	return Theme{
		Primary:   lipgloss.Color("63"),  // Blue
		Secondary: lipgloss.Color("196"), // Red
		Text:      lipgloss.Color("255"), // White
		SubText:   lipgloss.Color("248"), // Light Grey
		Faint:     lipgloss.Color("240"), // Dark Grey
		Highlight: lipgloss.Color("214"), // Orange/Gold
		Tag:       lipgloss.Color("120"), // Light Green
		Reminder:  lipgloss.Color("81"),  // Sky Blue
		Surface:   lipgloss.Color("236"), // Darker Grey
		OK:        lipgloss.Color("120"), // Light Green
		Warn:      lipgloss.Color("208"), // Orange
	}
}

// LightTheme returns the standard 256-color theme for light backgrounds.
func LightTheme() Theme {
	return Theme{
		Primary:   lipgloss.Color("27"),  // Blue (Darker than 63)
		Secondary: lipgloss.Color("160"), // Red (Darker than 196)
		Text:      lipgloss.Color("232"), // Black/Very Dark Grey
		SubText:   lipgloss.Color("240"), // Dark Grey
		Faint:     lipgloss.Color("250"), // Light Grey
		Highlight: lipgloss.Color("166"), // Orange (Darker than 214)
		Tag:       lipgloss.Color("28"),  // Dark Green
		Reminder:  lipgloss.Color("31"),  // Deep Sky Blue
		Surface:   lipgloss.Color("254"), // Lightest Grey
		OK:        lipgloss.Color("28"),  // Dark Green
		Warn:      lipgloss.Color("130"), // Dark Orange
	}
}

// ANSIDarkTheme returns a 16-color compatible theme for dark backgrounds.
func ANSIDarkTheme() Theme {
	return Theme{
		Primary:   lipgloss.Color("4"),  // Blue
		Secondary: lipgloss.Color("1"),  // Red
		Text:      lipgloss.Color("15"), // White
		SubText:   lipgloss.Color("7"),  // Light Grey
		Faint:     lipgloss.Color("8"),  // Dark Grey
		Highlight: lipgloss.Color("3"),  // Yellow
		Tag:       lipgloss.Color("2"),  // Green
		Reminder:  lipgloss.Color("6"),  // Cyan
		OK:        lipgloss.Color("2"),  // Green
		Warn:      lipgloss.Color("3"),  // Yellow
	}
}

// ANSILightTheme returns a 16-color compatible theme for light backgrounds.
func ANSILightTheme() Theme {
	return Theme{
		Primary:   lipgloss.Color("4"), // Blue
		Secondary: lipgloss.Color("1"), // Red
		Text:      lipgloss.Color("0"), // Black
		SubText:   lipgloss.Color("8"), // Dark Grey
		Faint:     lipgloss.Color("7"), // Light Grey
		Highlight: lipgloss.Color("5"), // Magenta (Yellow is often invisible on white)
		Tag:       lipgloss.Color("2"), // Green
		Reminder:  lipgloss.Color("6"), // Cyan
		OK:        lipgloss.Color("2"), // Green
		Warn:      lipgloss.Color("5"), // Magenta, as Highlight
	}
}

// MochaTheme returns the Catppuccin Mocha palette shared with the t8s and Postero TUIs:
// one yellow accent, every other color carries a meaning. Terminals without true color
// get the nearest 256 or 16 colors.
func MochaTheme() Theme {
	return Theme{
		Primary:   lipgloss.Color("#89b4fa"), // Blue
		Secondary: lipgloss.Color("#f38ba8"), // Red
		Text:      lipgloss.Color("#cdd6f4"),
		SubText:   lipgloss.Color("#9399b2"),
		Faint:     lipgloss.Color("#45475a"),
		Highlight: lipgloss.Color("#f9e2af"), // Yellow
		Tag:       lipgloss.Color("#cba6f7"), // Mauve
		Reminder:  lipgloss.Color("#94e2d5"), // Teal
		Surface:   lipgloss.Color("#232636"),
		OK:        lipgloss.Color("#a6e3a1"), // Green
		Warn:      lipgloss.Color("#fab387"), // Peach
	}
}

// CustomTheme returns a theme based on configuration. Colors that are not set come from
// the theme named by theme.base, DarkTheme by default.
func CustomTheme(cfg config.ThemeConfig) Theme {
	return customTheme(cfg, lipgloss.HasDarkBackground())
}

// customTheme builds a custom theme for a dark or light terminal. Without theme.base on a light
// terminal, selected rows are reversed rather than put on dark's Surface, which configs from
// before Surface cannot have set to go with their colors.
func customTheme(cfg config.ThemeConfig, darkTerminal bool) Theme {
	t, ok := namedTheme(cfg.Base)
	if !ok {
		t = DarkTheme()
		if !darkTerminal {
			t.Surface = ""
		}
	}

	if cfg.Primary != "" {
		t.Primary = lipgloss.Color(cfg.Primary)
	}
	if cfg.Secondary != "" {
		t.Secondary = lipgloss.Color(cfg.Secondary)
	}
	if cfg.Text != "" {
		t.Text = lipgloss.Color(cfg.Text)
	}
	if cfg.SubText != "" {
		t.SubText = lipgloss.Color(cfg.SubText)
	}
	if cfg.Faint != "" {
		t.Faint = lipgloss.Color(cfg.Faint)
	}
	if cfg.Highlight != "" {
		t.Highlight = lipgloss.Color(cfg.Highlight)
	}
	if cfg.Tag != "" {
		t.Tag = lipgloss.Color(cfg.Tag)
	}
	if cfg.Reminder != "" {
		t.Reminder = lipgloss.Color(cfg.Reminder)
	}
	if cfg.Surface != "" {
		t.Surface = lipgloss.Color(cfg.Surface)
	}
	if cfg.OK != "" {
		t.OK = lipgloss.Color(cfg.OK)
	}
	if cfg.Warn != "" {
		t.Warn = lipgloss.Color(cfg.Warn)
	}
	if len(cfg.TagColors) > 0 {
		t.TagColors = make(map[string]TagColorStyle, len(cfg.TagColors))
		for tag, color := range cfg.TagColors {
			t.TagColors[tag] = TagColorStyle{FG: lipgloss.Color(color)}
		}
	}
	return t
}

// Values of theme.name.
const (
	themeCustom = "custom"
	themeMocha  = "mocha"
)

// namedTheme returns a built-in theme by its theme.name.
func namedTheme(name string) (Theme, bool) {
	switch name {
	case "ansi", "ansi_dark":
		return ANSIDarkTheme(), true
	case "ansi_light":
		return ANSILightTheme(), true
	case "dark", "default":
		return DarkTheme(), true
	case "light":
		return LightTheme(), true
	case themeMocha:
		return MochaTheme(), true
	}
	return Theme{}, false
}

// GetTheme returns the appropriate theme based on terminal capabilities or user preference.
func GetTheme(cfg config.ThemeConfig) Theme {
	if cfg.Name == themeCustom {
		return CustomTheme(cfg)
	}
	if t, ok := namedTheme(cfg.Name); ok {
		return t
	}

	// Auto-detect based on terminal capabilities
	profile := lipgloss.ColorProfile()
	isDark := lipgloss.HasDarkBackground()

	if profile == termenv.Ascii || profile == termenv.ANSI {
		if isDark {
			return ANSIDarkTheme()
		}
		return ANSILightTheme()
	}

	if isDark {
		return DarkTheme()
	}
	return LightTheme()
}

// withSelection puts a style on the selected-row background, or reverses it when the
// theme has no Surface.
func withSelection(s lipgloss.Style, t Theme) lipgloss.Style {
	if t.Surface == "" {
		return s.Reverse(true)
	}
	return s.Background(t.Surface)
}
