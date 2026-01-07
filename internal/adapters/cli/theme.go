package cli

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/muesli/termenv"
)

// Theme defines the color palette for the application.
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Text      lipgloss.Color
	SubText   lipgloss.Color
	Faint     lipgloss.Color
	Highlight lipgloss.Color
}

// Styles holds all the lipgloss styles used in the UI.
type Styles struct {
	Wrapper       lipgloss.Style
	Header        lipgloss.Style
	Weekday       lipgloss.Style
	Day           lipgloss.Style
	Today         lipgloss.Style
	Selected      lipgloss.Style
	DetailsHeader lipgloss.Style
	Time          lipgloss.Style
	Project       lipgloss.Style
	Desc          lipgloss.Style
	Duration      lipgloss.Style
	Sidebar       lipgloss.Style
	Dot           lipgloss.Style
	Line          lipgloss.Style
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
	}
}

// CustomTheme returns a theme based on configuration
// Falls back to DarkTheme values if not set.
func CustomTheme(cfg config.ThemeConfig) Theme {
	t := DarkTheme()

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

	return t
}

// GetTheme returns the appropriate theme based on terminal capabilities or user preference.
func GetTheme(cfg config.ThemeConfig) Theme {
	// 1. Check configuration
	name := cfg.Name

	switch name {
	case "ansi":
		return ANSIDarkTheme()
	case "ansi_dark":
		return ANSIDarkTheme()
	case "ansi_light":
		return ANSILightTheme()
	case "dark":
		return DarkTheme()
	case "light":
		return LightTheme()
	case "custom":
		return CustomTheme(cfg)
	case "default":
		return DarkTheme()
	}

	// 2. Auto-detect based on terminal capabilities
	profile := lipgloss.ColorProfile()
	isDark := lipgloss.HasDarkBackground()

	if profile == termenv.Ascii {
		if isDark {
			return ANSIDarkTheme()
		}
		return ANSILightTheme()
	}
	if profile == termenv.ANSI {
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

// InitStyles creates the styles based on the provided theme.
func InitStyles(t Theme) Styles {
	s := Styles{}

	s.Wrapper = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Faint).
		Padding(0, 1).
		MarginRight(1)

	s.Header = lipgloss.NewStyle().
		Foreground(t.Text).
		Bold(true).
		Align(lipgloss.Center).
		Width(28)

	s.Weekday = lipgloss.NewStyle().
		Foreground(t.SubText).
		Width(4).
		Align(lipgloss.Center)

	s.Day = lipgloss.NewStyle().
		Width(4).
		Align(lipgloss.Center)

	s.Today = s.Day.
		Foreground(t.Text).
		Background(t.Secondary).
		Bold(true)

	s.Selected = s.Day.
		Foreground(t.Text).
		Background(t.Primary)

	s.DetailsHeader = lipgloss.NewStyle().
		Foreground(t.Text).
		Bold(true).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(t.Faint).
		Width(100)

	s.Time = lipgloss.NewStyle().
		Foreground(t.SubText).
		Width(12)

	s.Project = lipgloss.NewStyle().
		Foreground(t.Primary).
		Bold(true)

	s.Desc = lipgloss.NewStyle().
		Foreground(t.Text)

	s.Duration = lipgloss.NewStyle().
		Foreground(t.Highlight)

	s.Sidebar = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Faint).
		Padding(0, 1).
		Width(40)

	s.Dot = lipgloss.NewStyle().Foreground(t.Highlight)
	s.Line = lipgloss.NewStyle().Foreground(t.Faint)

	return s
}
