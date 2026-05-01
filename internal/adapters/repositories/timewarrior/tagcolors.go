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
// config look like:
//
//	color.tag.work=color2
//	color.tag.personal=rgb/1/3/5
//
// Values are converted to ANSI 256-color index strings or hex strings suitable
// for direct use as lipgloss.Color values. Keys are the tag names as written
// in the config.
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

		const prefix = "color.tag."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		tag := strings.TrimPrefix(key, prefix)
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
// into a raw color string (ANSI index or hex) suitable for lipgloss.Color.
// Only the foreground component is extracted; background (on_*) and
// text-decoration tokens are ignored.
//
// Supported formats:
//   - colorN         – ANSI 256-color index (e.g. "color2", "color196")
//   - rgb/R/G/B      – 6-bit RGB cube (R,G,B ∈ [0,5]); mapped to 256-color index
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
			n := after
			if idx, err := strconv.Atoi(n); err == nil && idx >= 0 && idx <= 255 {
				return n, true
			}
		}

		// rgb/R/G/B
		if after, ok := strings.CutPrefix(token, "rgb/"); ok {
			parts := strings.SplitN(after, "/", 3)
			if len(parts) == 3 {
				r, re := strconv.Atoi(parts[0])
				g, ge := strconv.Atoi(parts[1])
				b, be := strconv.Atoi(parts[2])
				if re == nil && ge == nil && be == nil &&
					r >= 0 && r <= 5 && g >= 0 && g <= 5 && b >= 0 && b <= 5 {
					idx := 16 + 36*r + 6*g + b
					return strconv.Itoa(idx), true
				}
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
