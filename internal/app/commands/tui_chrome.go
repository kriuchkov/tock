package commands

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Glyphs shared by the TUI screens.
const (
	glyphSelected = "▌"
	glyphCursor   = "▏"
	glyphOverdue  = "◆"
	glyphRunning  = "●"
	glyphPaused   = "⏸"
	glyphMark     = "•"
	glyphRepeat   = "↻"
	glyphMore     = "▾"
	glyphBar      = "▇"
	glyphTrack    = "░"
	rulerThin     = "─"
	columnRule    = " │ "
	brandLabel    = "tock"
	// glyphBrand stands for the Tock logo, its stacked bars, before the name. Not ☰: lipgloss
	// counts it two columns wide, terminals draw it in one.
	glyphBrand = "≡"
)

const (
	// rowGutterW is what a list row keeps on its left: the selection bar, the row's glyph and a space.
	rowGutterW = 3
	// rulerEndW is the thin rule after the ruler summary.
	rulerEndW = 2
	// footerKeyGap separates the keys of the footer.
	footerKeyGap = "   "
	// focusBoxH is the height of the focus box with its border.
	focusBoxH = 4
	// rowCellsCap is room for the cells of a list row, so building one allocates once.
	rowCellsCap = 8
)

// clockWidth is how wide a time is in format: 5 for "15:04", 8 for "03:04 PM".
func clockWidth(format string) int {
	return lipgloss.Width(time.Date(2006, time.January, 2, 15, 4, 5, 0, time.Local).Format(format))
}

// chromeStyles are the text styles of the TUI screens, built from the roles of a theme.
type chromeStyles struct {
	theme      Theme
	text       lipgloss.Style
	bold       lipgloss.Style
	muted      lipgloss.Style
	mutedBold  lipgloss.Style
	faint      lipgloss.Style
	accent     lipgloss.Style
	accentBold lipgloss.Style
	info       lipgloss.Style
	ok         lipgloss.Style
	danger     lipgloss.Style
	tag        lipgloss.Style
	reminder   lipgloss.Style
}

func newChromeStyles(t Theme) chromeStyles {
	base := lipgloss.NewStyle()
	return chromeStyles{
		theme:      t,
		text:       base.Foreground(t.Text),
		bold:       base.Foreground(t.Text).Bold(true),
		muted:      base.Foreground(t.SubText),
		mutedBold:  base.Foreground(t.SubText).Bold(true),
		faint:      base.Foreground(t.Faint),
		accent:     base.Foreground(t.Highlight),
		accentBold: base.Foreground(t.Highlight).Bold(true),
		info:       base.Foreground(t.Primary),
		ok:         base.Foreground(t.OK),
		danger:     base.Foreground(t.Secondary),
		tag:        base.Foreground(t.Tag),
		reminder:   base.Foreground(t.Reminder),
	}
}

// screenHeader is the top of a screen: the tabs and a title on the first line, the ruler
// under them with a summary at its right end.
type screenHeader struct {
	tabs    []string
	active  int
	title   string
	hint    string
	summary []rulerItem
}

// tabStrip is the tabs of the TUI with the one a screen is on.
type tabStrip struct {
	names  []string
	active int
}

// singleTab is the strip of a screen shown on its own.
func singleTab(name string) tabStrip {
	return tabStrip{names: []string{name}}
}

// footerKey is the key that switches tabs, "1–4", when there is more than one.
func (t tabStrip) footerKey(action string) (footerKey, bool) {
	if len(t.names) < 2 {
		return footerKey{}, false
	}
	return footerKey{key: fmt.Sprintf("1–%d", len(t.names)), action: action, pin: true}, true
}

// withTabs adds the tab key in front of the last key of a footer, which is quit.
func (t tabStrip) withTabs(keys []footerKey, action string) []footerKey {
	key, ok := t.footerKey(action)
	if !ok || len(keys) == 0 {
		return keys
	}
	last := keys[len(keys)-1]
	return append(append(keys[:len(keys)-1:len(keys)-1], key), last)
}

// rulerItem is one styled piece of the ruler summary with a shorter form for narrow terminals.
type rulerItem struct {
	long  string
	short string
}

// renderHeader draws the two header lines in width columns. Tabs that do not fit fold
// into the active one; summary items shorten from the right end, then drop off it.
func (s chromeStyles) renderHeader(h screenHeader, width int) string {
	lead := " " + s.accent.Render(glyphBrand) + " " + s.bold.Render(brandLabel) + "   "
	right := s.bold.Render(h.title) + " "
	if h.hint != "" {
		right += s.muted.Render(h.hint + " ")
	}

	if width <= 0 { // the size is not known yet: nothing is cut
		// The summary keeps a column of the ruler on each side, as fitRulerSummary asks.
		summary := lipgloss.Width(s.fitRulerSummary(h.summary, math.MaxInt32)) + 2 + rulerEndW
		width = max(lipgloss.Width(lead)+tabCellsWidth(h.tabs)+lipgloss.Width(right)+1, summary)
	}
	tabs, folded, tabsW := h.tabs, false, tabCellsWidth(h.tabs)
	if len(tabs) > 1 && lipgloss.Width(lead)+tabsW+lipgloss.Width(right)+1 > width {
		// The active tab stays, marked ▾ for the others.
		tabs, folded = []string{tabs[h.active]}, true
		tabsW = tabCellsWidth(tabs) + lipgloss.Width(glyphMore) + 1
	}
	if lipgloss.Width(lead)+tabsW+lipgloss.Width(right)+1 > width {
		right = s.bold.Render(cutText(h.title, max(width-lipgloss.Width(lead)-tabsW-2, 0))) + " "
	}

	var line strings.Builder
	line.WriteString(lead)
	for i, label := range tabs {
		if i > 0 {
			line.WriteString("  ")
		}
		cell := " " + label + " "
		if folded {
			cell += glyphMore + " "
		}
		if folded || i == h.active {
			line.WriteString(withSelection(s.bold, s.theme).Render(cell))
		} else {
			line.WriteString(s.muted.Render(cell))
		}
	}
	gap := max(width-lipgloss.Width(line.String())-lipgloss.Width(right), 1)
	line.WriteString(strings.Repeat(" ", gap) + right)

	summary := s.fitRulerSummary(h.summary, width-rulerEndW)
	fill := max(width-lipgloss.Width(summary)-rulerEndW, 0)
	rule := s.faint.Render(strings.Repeat(rulerThin, fill)) + summary + s.faint.Render(strings.Repeat(rulerThin, rulerEndW))
	return line.String() + "\n" + rule
}

// tabCellsWidth is the width the tabs take on the header line: " label " cells two columns apart.
func tabCellsWidth(tabs []string) int {
	w := 0
	for i, label := range tabs {
		if i > 0 {
			w += 2
		}
		w += lipgloss.Width(label) + 2
	}
	return w
}

// fitRulerSummary joins the items as " item ─ item " in at most width columns, keeping
// a thin rule of at least two columns in front of them.
func (s chromeStyles) fitRulerSummary(items []rulerItem, width int) string {
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = item.long
	}
	join := func(parts []string) string {
		cells := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				cells = append(cells, " "+part+" ")
			}
		}
		return strings.Join(cells, s.faint.Render(rulerThin))
	}

	fits := func() bool { return lipgloss.Width(join(texts))+2 <= width }
	for i := len(texts) - 1; i >= 0 && !fits(); i-- {
		texts[i] = items[i].short
	}
	for len(texts) > 0 && !fits() {
		texts = texts[:len(texts)-1]
	}
	return join(texts)
}

// renderSection is a section title in capitals with its counts: "REMINDERS · 2/5".
func (s chromeStyles) renderSection(title string, meta ...string) string {
	parts := []string{strings.ToUpper(title)}
	for _, m := range meta {
		if m != "" {
			parts = append(parts, m)
		}
	}
	return s.mutedBold.Render(strings.Join(parts, " · "))
}

// renderViewLine is the line under the header that names a screen's views, the shown one on
// the selection color, and the key that switches them.
func (s chromeStyles) renderViewLine(label string, views []string, active int, key string) string {
	var b strings.Builder
	b.WriteString(s.muted.Render(" " + label + " "))
	for i, view := range views {
		cell := " " + view + " "
		if i == active {
			b.WriteString(withSelection(s.bold, s.theme).Render(cell))
		} else {
			b.WriteString(s.muted.Render(cell))
		}
		b.WriteString(" ")
	}
	if key != "" {
		b.WriteString(s.muted.Render(" (" + key + ")"))
	}
	return b.String()
}

// renderScreen stacks the top of a screen, its body cut or padded to the lines the top and
// the bottom leave, and the bottom, so the footer stays on the last line of the terminal.
// Before the terminal size is known the parts are only joined.
func renderScreen(top, body, bottom string, width, height int) string {
	if width <= 0 || height <= 0 {
		return top + "\n" + body + "\n" + bottom
	}
	bodyH := height - lipgloss.Height(top) - lipgloss.Height(bottom)
	if bodyH <= 0 {
		return top + "\n" + bottom
	}
	return top + "\n" + fitBlock(body, width, bodyH) + "\n" + bottom
}

// barLine is a row of a bar chart: "Project Alpha  ▇▇▇▇▇▇  2h45m".
type barLine struct {
	label      string
	labelStyle lipgloss.Style
	labelW     int
	// length is the bar's columns out of barW; a value above zero with no columns is a hairline,
	// so that it does not read as nothing at all.
	length   int
	nonzero  bool
	barStyle lipgloss.Style
	barW     int
	value    string
	valueW   int
}

func (s chromeStyles) renderBarLine(b barLine) string {
	bar := strings.Repeat(glyphBar, min(b.length, b.barW))
	if bar == "" && b.nonzero {
		bar = barChar
	}
	return b.labelStyle.Render(padText(cutText(b.label, b.labelW-2), b.labelW, false)) +
		b.barStyle.Render(bar) +
		strings.Repeat(" ", max(b.barW-lipgloss.Width(bar), 0)) +
		s.text.Render(padText(b.value, b.valueW, true))
}

// overdueItem is the ruler item of overdue reminders, "◆ 2 overdue", in the accent.
func (s chromeStyles) overdueItem(label string, count int) rulerItem {
	return rulerItem{
		long:  s.accentBold.Render(glyphOverdue + " " + label),
		short: s.accentBold.Render(fmt.Sprintf("%s %d", glyphOverdue, count)),
	}
}

// renderStatus is the result of an action at the right end of the footer: "✓ Done: Standup",
// or "✗ …" in the danger color for an error.
func (s chromeStyles) renderStatus(status string, isErr bool) string {
	switch {
	case status == "":
		return ""
	case isErr:
		return s.danger.Render("✗ " + status)
	}
	return s.ok.Render("✓ " + status)
}

// fitFooterKeys picks the keys that fit in room columns: the last one (quit, back or cancel)
// and the pinned ones always, the others in order while they fit. room <= 0 fits all.
func fitFooterKeys(keys []footerKey, parts []string, room int) []bool {
	shown := make([]bool, len(keys))
	pinned := func(i int) bool { return i == len(keys)-1 || keys[i].pin }
	used := 1 - len(footerKeyGap) // the space before the keys, and no gap before the first
	for i := range keys {
		if pinned(i) || room <= 0 {
			shown[i] = true
			used += len(footerKeyGap) + lipgloss.Width(parts[i])
		}
	}
	for i := range keys {
		if shown[i] {
			continue
		}
		w := len(footerKeyGap) + lipgloss.Width(parts[i])
		if used+w > room {
			break
		}
		shown[i], used = true, used+w
	}
	return shown
}

// renderPrompt is a question in place of the footer, such as a delete confirmation.
func (s chromeStyles) renderPrompt(prompt string, width int) string {
	if width > 0 {
		prompt = cutText(prompt, width-2)
	}
	return " " + s.danger.Bold(true).Render(prompt)
}

// footerKey is one key of the footer: the key in muted text, then what it does. A pinned key
// stays when the footer is too narrow for all of them, as the last one does.
type footerKey struct {
	key    string
	action string
	pin    bool
}

// renderFooter draws the keys of a screen on one line with a styled status at its right end;
// keys that do not fit drop off the end.
func (s chromeStyles) renderFooter(keys []footerKey, status string, width int) string {
	statusW := 0
	if status != "" {
		statusW = lipgloss.Width(status) + 2
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = s.muted.Render(k.key) + " " + s.text.Render(k.action)
	}
	shown := fitFooterKeys(keys, parts, width-statusW)
	var line strings.Builder
	line.WriteString(" ")
	sep := ""
	for i, part := range parts {
		if shown[i] {
			line.WriteString(sep + part)
			sep = footerKeyGap
		}
	}
	if status == "" {
		return line.String()
	}
	if width <= 0 { // no limit: the status follows the keys
		return line.String() + footerKeyGap + status
	}
	gap := width - lipgloss.Width(line.String()) - lipgloss.Width(status) - 1
	if gap < 1 {
		return " " + lipgloss.NewStyle().MaxWidth(max(width-1, 0)).Render(status)
	}
	return line.String() + strings.Repeat(" ", gap) + status + " "
}

// renderFocusBox frames the details of the selected row in width columns.
func (s chromeStyles) renderFocusBox(lines []string, width int) string {
	inner := max(width-4, 1)
	cut := lipgloss.NewStyle().MaxWidth(inner)
	fitted := make([]string, 0, focusBoxH-2)
	for _, line := range lines[:min(len(lines), focusBoxH-2)] {
		fitted = append(fitted, cut.Render(line))
	}
	for len(fitted) < focusBoxH-2 {
		fitted = append(fitted, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.theme.Faint).
		Padding(0, 1).
		Width(max(width-2, 1)).
		Render(strings.Join(fitted, "\n"))
}

// rowCell is one column of a list row. A cell with no width takes what the others
// leave; text that does not fit is cut with "…", its suffix first.
type rowCell struct {
	text        string
	style       lipgloss.Style
	suffix      string
	suffixStyle lipgloss.Style
	// styled is text already styled, used as is in place of text; it is cut without "…".
	styled string
	width  int
	right  bool
}

// renderRow draws a list row in width columns behind its gutter: the selection bar and
// the row's glyph. A selected row is bold on the surface color, or reversed without one.
func (s chromeStyles) renderRow(glyph string, glyphStyle lipgloss.Style, cells []rowCell, selected bool, width int) string {
	on := func(st lipgloss.Style) lipgloss.Style {
		if !selected {
			return st
		}
		return withSelection(st, s.theme).Bold(true)
	}

	flex := width - rowGutterW
	for _, c := range cells {
		flex -= c.width
	}
	flex = max(flex, 0)

	var b strings.Builder
	bar, barStyle := " ", lipgloss.NewStyle()
	if selected {
		// The bar keeps its own colors: reversed, it would turn into a block.
		bar, barStyle = glyphSelected, s.accent
		if s.theme.Surface != "" {
			barStyle = barStyle.Background(s.theme.Surface)
		}
	}
	b.WriteString(barStyle.Render(bar))
	if glyph == "" {
		// A row without a glyph keeps the plain selection there, not a block of the glyph's color.
		glyphStyle = lipgloss.NewStyle()
	}
	b.WriteString(on(glyphStyle).Render(padText(cutText(glyph, 1), 1, false)))
	b.WriteString(on(lipgloss.NewStyle()).Render(" "))

	for _, c := range cells {
		w := c.width
		if w == 0 {
			w = flex
		}
		b.WriteString(renderCell(c, w, on))
	}
	if pad := width - lipgloss.Width(b.String()); pad > 0 {
		b.WriteString(on(lipgloss.NewStyle()).Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}

// renderCell draws a cell in width columns, one of which is kept free as the gap to the next.
func renderCell(c rowCell, width int, on func(lipgloss.Style) lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	room := max(width-1, 0)
	if c.styled != "" {
		styled := ""
		if room > 0 {
			styled = lipgloss.NewStyle().MaxWidth(room).Render(c.styled)
		}
		return styled + on(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(width-lipgloss.Width(styled), 0)))
	}
	text := cutText(c.text, room)
	suffix := ""
	if c.suffix != "" && text == c.text {
		suffix = cutText(c.suffix, room-lipgloss.Width(text))
	}
	pad := strings.Repeat(" ", max(width-lipgloss.Width(text)-lipgloss.Width(suffix), 0))
	rendered := on(c.style).Render(text)
	if suffix != "" {
		rendered += on(c.suffixStyle).Render(suffix)
	}
	if c.right {
		return on(lipgloss.NewStyle()).Render(pad) + rendered
	}
	return rendered + on(lipgloss.NewStyle()).Render(pad)
}

// cutText cuts plain text to width terminal columns, ending it with "…" when cut.
func cutText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// padText pads plain text with spaces to width columns, on the left when right is set.
func padText(s string, width int, right bool) string {
	pad := strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
	if right {
		return pad + s
	}
	return s + pad
}

// fitBlock cuts or pads styled text to exactly width columns and height lines.
func fitBlock(text string, width, height int) string {
	lines := strings.Split(text, "\n")
	cut := lipgloss.NewStyle().MaxWidth(width)
	out := make([]string, height)
	for i := range height {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if lipgloss.Width(line) > width {
			line = cut.Render(line)
		}
		out[i] = line + strings.Repeat(" ", max(width-lipgloss.Width(line), 0))
	}
	return strings.Join(out, "\n")
}

// columnRules is the separator between two columns of a screen, height lines tall.
func (s chromeStyles) columnRules(height int) string {
	rules := make([]string, height)
	for i := range rules {
		rules[i] = s.faint.Render(columnRule)
	}
	return strings.Join(rules, "\n")
}

// formatHM formats a duration as "45m", "2h" or "2h05m"; hours are never folded into days.
func formatHM(d time.Duration) string {
	d = max(d.Round(time.Minute), 0)
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}

// barLength is how many of width columns a bar of value out of maxValue takes.
func barLength[T ~int | ~int64](value, maxValue T, width int) int {
	if maxValue <= 0 || value <= 0 || width <= 0 {
		return 0
	}
	return max(int(float64(value)/float64(maxValue)*float64(width)), 0)
}

// formFieldW is the width a form is drawn in while the terminal size is unknown.
const formFieldW = 80

// formField is a line of a form: its label, its value, with the cursor while it is edited, and
// a note after the value, "→ Mon 16 Nov". styled is a value already styled, used as is.
type formField struct {
	label      string
	labelW     int
	value      string
	valueStyle lipgloss.Style
	styled     string
	note       string
	active     bool
}

// renderFormField draws a form field as a row: the edited one marked ▌ on the surface color,
// its value bold and followed by the cursor, the end of a long value kept in sight.
func (s chromeStyles) renderFormField(f formField, width int) string {
	if width <= 0 {
		width = formFieldW
	}
	room := max(width-rowGutterW-f.labelW-1, 1)
	value := rowCell{text: f.value, style: f.valueStyle, styled: f.styled}
	valueW := lipgloss.Width(f.styled)
	if f.styled == "" {
		if f.active {
			value.suffix, value.suffixStyle = glyphCursor, s.accent
			if lipgloss.Width(f.value) >= room {
				value.text = "…" + tailText(f.value, room-2)
			}
		}
		valueW = lipgloss.Width(value.text) + lipgloss.Width(value.suffix)
	}
	value.width = min(valueW+1, room)
	cells := []rowCell{{text: f.label, style: s.muted, width: f.labelW}, value, {text: f.note, style: s.muted}}
	return s.renderRow("", s.muted, cells, f.active, width)
}

// tailText is the last width columns of plain text.
func tailText(s string, width int) string {
	runes := []rune(s)
	used, start := 0, len(runes)
	for start > 0 {
		w := lipgloss.Width(string(runes[start-1]))
		if used+w > width {
			break
		}
		used += w
		start--
	}
	return string(runes[start:])
}

// scrollOffset is the first of total lines to show in height lines so that the selected one is
// in sight, moving the window from offset as little as it can.
func scrollOffset(offset, selected, height, total int) int {
	switch {
	case selected < offset:
		offset = selected
	case selected >= offset+height:
		offset = selected - height + 1
	}
	return max(min(offset, total-height), 0)
}
