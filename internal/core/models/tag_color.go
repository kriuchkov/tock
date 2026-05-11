package models

// TagColor holds the foreground and optional background ANSI 256-color index
// strings for a tag. Empty string means no color was specified for that component.
type TagColor struct {
	FG string // foreground ANSI index, e.g. "3", "196"
	BG string // background ANSI index; empty if not set
}
