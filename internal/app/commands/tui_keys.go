package commands

// Shared Bubble Tea key bindings used across the list/calendar/watch TUIs.
const (
	keyCtrlC = "ctrl+c"
	keyLeft  = "left"
	keyRight = "right"
	keyDown  = "down"
	keyEsc   = "esc"
	keyEnter = "enter"
	keyDel   = "delete"
	keyTab   = "tab"
	keySpace = "space"
	// keyShiftTab steps back through the views that tab steps through.
	keyShiftTab = "shift+tab"
)

// moveCursor moves the selected row of a list of total rows, page of them on screen, on the
// keys the bubbles table took: ↑/k and ↓/j a row, pgup/b and pgdown/f a page, ctrl+u/u and
// ctrl+d/d half a page, home/g and end/G to the ends. ok is false for any other key.
func moveCursor(key string, cursor, total, page int) (int, bool) {
	page = max(page, 1)
	switch key {
	case "up", "k":
		cursor--
	case keyDown, "j":
		cursor++
	case "pgup", "b":
		cursor -= page
	case "pgdown", "f":
		cursor += page
	case "ctrl+u", "u":
		cursor -= max(page/2, 1)
	case "ctrl+d", "d":
		cursor += max(page/2, 1)
	case "home", "g":
		cursor = 0
	case "end", "G":
		cursor = total - 1
	default:
		return cursor, false
	}
	return max(min(cursor, total-1), 0), true
}
