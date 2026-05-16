package timewarrior

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TagColor holds the foreground and optional background ANSI 256-color index
// strings parsed from a TimeWarrior color spec (see timew-tags(1)).
// An empty string means no color was specified for that component.
type TagColor struct {
	FG string // foreground ANSI index, e.g. "3", "196"
	BG string // background ANSI index, e.g. "8"; empty if not set
}

// ParseTagColors reads a TimeWarrior config file and returns a map of
// tag name → TagColor. The config file is resolved in the following order:
//
//  1. cfgPath, if non-empty (explicit override)
//  2. filepath.Dir(dataDir)/timewarrior.cfg  (default XDG_DATA_HOME layout)
//  3. ~/.config/timewarrior/timewarrior.cfg  (XDG_CONFIG_HOME layout)
//
// The first path that exists and can be opened is used.
// If no file is found, nil is returned.
func ParseTagColors(dataDir, cfgPath string) map[string]TagColor {
	candidates := resolveCfgCandidates(dataDir, cfgPath)
	for _, p := range candidates {
		if result := parseTagColorsFromFile(p); result != nil {
			return result
		}
	}
	return nil
}

// resolveCfgCandidates returns the ordered list of config file paths to try.
func resolveCfgCandidates(dataDir, explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	candidates := []string{
		filepath.Join(filepath.Dir(dataDir), "timewarrior.cfg"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "timewarrior", "timewarrior.cfg"))
	}
	return candidates
}

// parseTagColorsFromFile opens path and parses tag color entries.
// Returns nil if the file cannot be opened.
func parseTagColorsFromFile(path string) map[string]TagColor {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string]TagColor)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Format: tags.<name>.color
		const prefix = "tags."
		const suffix = ".color"
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		tag := key[len(prefix) : len(key)-len(suffix)]
		if tag == "" {
			continue
		}

		fg, bg, hasFG := parseTimewarriorColor(value)
		if hasFG || bg != "" {
			result[tag] = TagColor{FG: fg, BG: bg}
		}
	}
	return result
}

// colorToken parses a single color token into an ANSI 256-color index string.
// Supported: colorN, rgbRGB (3-digit 6×6×6 cube), grayN (N ∈ [0,23]), and
// the 8 named ANSI colors (black, red, green, yellow, blue, magenta, cyan, white).
func colorToken(token string) (string, bool) {
	if after, ok := strings.CutPrefix(token, "color"); ok {
		if idx, err := strconv.Atoi(after); err == nil && idx >= 0 && idx <= 255 {
			return after, true
		}
	}
	if after, ok := strings.CutPrefix(token, "rgb"); ok && len(after) == 3 {
		r := int(after[0] - '0')
		g := int(after[1] - '0')
		b := int(after[2] - '0')
		if r <= 5 && g <= 5 && b <= 5 {
			return strconv.Itoa(16 + 36*r + 6*g + b), true
		}
	}
	if after, ok := strings.CutPrefix(token, "gray"); ok {
		if n, err := strconv.Atoi(after); err == nil && n >= 0 && n <= 23 {
			return strconv.Itoa(232 + n), true
		}
	}
	switch token {
	case "black":
		return "0", true
	case "red":
		return "1", true
	case "green":
		return "2", true
	case "yellow":
		return "3", true
	case "blue":
		return "4", true
	case "magenta":
		return "5", true
	case "cyan":
		return "6", true
	case "white":
		return "7", true
	}
	return "", false
}

// parseTimewarriorColor parses a TimeWarrior color spec into foreground and
// background ANSI index strings. ok is true when a foreground color is found.
//
// Spec grammar (timew-tags(1)):
//   - colorN | rgbRGB | grayN | <named>   — foreground
//   - on_colorN | on_<named>              — compact background
//   - on <named>                          — word-form background
//   - bold | underline | italic           — decoration, ignored
func parseTimewarriorColor(spec string) (string, string, bool) {
	var fg, bg string
	tokens := strings.Fields(spec)
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]

		// "on <color>" — background in word form
		if token == "on" && i+1 < len(tokens) {
			if c, isColor := colorToken(tokens[i+1]); isColor {
				bg = c
				i++
				continue
			}
		}

		// "on_*" — compact background
		if after, hasPrefix := strings.CutPrefix(token, "on_"); hasPrefix {
			if c, isColor := colorToken(after); isColor {
				bg = c
			}
			continue
		}

		// Decoration tokens
		if token == "bold" || token == "underline" || token == "italic" {
			continue
		}

		// First foreground color wins
		if fg == "" {
			if c, isColor := colorToken(token); isColor {
				fg = c
			}
		}
	}
	return fg, bg, fg != ""
}
