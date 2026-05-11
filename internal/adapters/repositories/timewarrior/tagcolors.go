package timewarrior

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseTagColors reads the timewarrior.cfg file located one directory above
// dataDir and returns a map of tag name → raw color value string. Entries in the
// config follow the format documented in timew-tags(1):
//
//	tags.work.color = color2
//	tags.personal.color = black on yellow
//
// Values are converted to ANSI 256-color index strings suitable for direct use
// as lipgloss.Color values. Only the foreground component is used.
// If the config file does not exist or cannot be read, nil is returned.
func ParseTagColors(dataDir string) map[string]string {
	cfgPath := filepath.Join(filepath.Dir(dataDir), "timewarrior.cfg")
	f, err := os.Open(cfgPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string]string)
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

		var c string
		if c, ok = parseTimewarriorColor(value); ok {
			result[tag] = c
		}
	}
	return result
}

// parseTimewarriorColor converts a TimeWarrior foreground color specification
// into a raw color string (ANSI index) suitable for lipgloss.Color.
// Only the foreground component is extracted; background (on_*) and
// text-decoration tokens are ignored.
//
// Supported formats:
//   - colorN         – ANSI 256-color index (e.g. "color2", "color196")
//   - rgbRGB         – 3-digit 6×6×6 RGB cube (each digit 0–5), e.g. "rgb135"
//   - grayN          – grayscale ramp, N ∈ [0,23]; mapped to ANSI indices 232–255
//   - Named ANSI 16  – black, red, green, yellow, blue, magenta, cyan, white
//
// The spec may contain multiple space-separated tokens (e.g. "bold color2 on_color8").
func parseTimewarriorColor(spec string) (string, bool) {
	for token := range strings.FieldsSeq(spec) {
		// Skip background and decoration tokens.
		if strings.HasPrefix(token, "on_") ||
			token == "bold" || token == "underline" || token == "italic" {
			continue
		}

		// colorN
		if after, ok := strings.CutPrefix(token, "color"); ok {
			if idx, err := strconv.Atoi(after); err == nil && idx >= 0 && idx <= 255 {
				return after, true
			}
		}

		// rgbRGB — three digits each 0–5, e.g. "rgb135"
		if after, ok := strings.CutPrefix(token, "rgb"); ok && len(after) == 3 {
			r := int(after[0] - '0')
			g := int(after[1] - '0')
			b := int(after[2] - '0')
			if r >= 0 && r <= 5 && g >= 0 && g <= 5 && b >= 0 && b <= 5 {
				idx := 16 + 36*r + 6*g + b
				return strconv.Itoa(idx), true
			}
		}

		// grayN — N ∈ [0,23], mapped to ANSI 232–255
		if after, ok := strings.CutPrefix(token, "gray"); ok {
			if n, err := strconv.Atoi(after); err == nil && n >= 0 && n <= 23 {
				return strconv.Itoa(232 + n), true
			}
		}

		// Named ANSI colors (foreground ANSI indices 0-7).
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
	}
	return "", false
}
