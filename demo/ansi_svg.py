#!/usr/bin/env python3
"""Turns tmux screen captures into an animated SVG, like assets/demo.svg at the top of the README.

Usage: ansi_svg.py screens.tsv [--type COMMAND] [--title TITLE] > demo.svg

Each line of screens.tsv is a screen: a name, a capture (tmux capture-pane -e -p) relative to
the tsv, the seconds it stays on screen in the loop (0 keeps it out), the key that leads to it,
a caption and its links. The SVG cuts from screen to screen in a loop, after typing COMMAND at
a prompt if given. Without animation (reduced motion, static renderers) it shows the first one.

Links make the SVG explorable when it is opened on its own (an <img> gets no clicks): each is
"label=target", separated by ";". The label is text on the screen, searched from the bottom
row up, or in the top row with a leading ^; [brackets] mark the part that is clickable. A click
goes to #target, and CSS :target shows that screen instead of the loop. No script is needed,
so it works where scripts are blocked, as on raw.githubusercontent.com.

Text keeps the terminal grid: every run sits at its column and is stretched to its width, so
any monospace font lines up. Lines, bars and blocks become shapes, so they join without the
gaps fonts leave between cells.
"""

import argparse
import html
import re
import sys
import unicodedata
from collections import namedtuple
from pathlib import Path

INTRO_CAPTION = "a time tracker that lives in your terminal"
MONO = "ui-monospace,'SF Mono',SFMono-Regular,Menlo,Consolas,'Liberation Mono','DejaVu Sans Mono',monospace"
SANS = "-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif"

FS, CW, CH = 14, 8.4, 17  # font size, cell width and height
BASELINE = 12.6  # text baseline within a cell
BAR_H = 10  # height of ▇ bars
PAD = 20  # around the terminal
TITLE_H, CAPTION_H = 34, 46  # window title bar and caption bar

BG, FG = "#16171d", "#d0d0d0"
CHROME, EDGE, MUTED, ACCENT = "#1e1f27", "#2c2e3a", "#9a9caa", "#5f5fff"
LIGHTS = ("#ff5f57", "#febc2e", "#28c840")

INTRO_S, TYPE_FROM, TYPE_STEP = 2.2, 0.5, 0.07  # intro length, typing start, seconds per key

ANSI16 = (
    "#1d1f21", "#cc6666", "#b5bd68", "#f0c674", "#81a2be", "#b294bb", "#8abeb7", "#c5c8c6",
    "#666666", "#d54e53", "#b9ca4a", "#e7c547", "#7aa6da", "#c397d8", "#70c0b1", "#eaeaea",
)

SGR = re.compile(r"\x1b\[([0-9;:]*)m")
Style = namedtuple("Style", "fg bg bold dim italic underline reverse")
PLAIN = Style(None, None, False, False, False, False, False)
Screen = namedtuple("Screen", "name rows seconds key caption links")
SGR_FLAGS = {
    1: ("bold", True), 2: ("dim", True), 3: ("italic", True), 4: ("underline", True), 7: ("reverse", True),
    23: ("italic", False), 24: ("underline", False), 27: ("reverse", False),
}


def xterm(n):
    if n < 16:
        return ANSI16[n]
    if n < 232:
        n -= 16
        level = (0, 95, 135, 175, 215, 255)
        return "#%02x%02x%02x" % (level[n // 36], level[n // 6 % 6], level[n % 6])
    v = 8 + 10 * (n - 232)
    return "#%02x%02x%02x" % (v, v, v)


def extended_color(codes, i):
    """Reads 5;n or 2;r;g;b after 38/48; returns the color and the index of its last code."""
    if codes[i + 1 : i + 2] == [5] and i + 2 < len(codes):
        return xterm(codes[i + 2]), i + 2
    if codes[i + 1 : i + 2] == [2] and i + 4 < len(codes):
        r, g, b = codes[i + 2 : i + 5]
        return "#%02x%02x%02x" % (r, g, b), i + 4
    return None, len(codes)


def apply_sgr(style, params):
    codes = [int(p) if p else 0 for p in re.split("[;:]", params)] if params else [0]
    i = 0
    while i < len(codes):
        c = codes[i]
        if c == 0:
            style = PLAIN
        elif c in SGR_FLAGS:
            style = style._replace(**{SGR_FLAGS[c][0]: SGR_FLAGS[c][1]})
        elif c == 22:
            style = style._replace(bold=False, dim=False)
        elif 30 <= c <= 37 or 90 <= c <= 97:
            style = style._replace(fg=xterm(c - 30 if c < 90 else c - 82))
        elif 40 <= c <= 47 or 100 <= c <= 107:
            style = style._replace(bg=xterm(c - 40 if c < 100 else c - 92))
        elif c == 39:
            style = style._replace(fg=None)
        elif c == 49:
            style = style._replace(bg=None)
        elif c in (38, 48):
            color, i = extended_color(codes, i)
            style = style._replace(**{"fg" if c == 38 else "bg": color})
        i += 1
    return style


def read_screen(path):
    """Returns the rows of a capture, each a list of (char, style); a wide char is followed by ("", style)."""
    lines = Path(path).read_text(encoding="utf-8").split("\n")
    if lines and lines[-1] == "":
        lines.pop()
    rows, style = [], PLAIN  # tmux carries the style over from the line before
    for line in lines:
        row, parts = [], SGR.split(line)
        for n, part in enumerate(parts):
            if n % 2:
                style = apply_sgr(style, part)
                continue
            for ch in part:
                if unicodedata.combining(ch):
                    continue
                row.append((ch, style))
                if unicodedata.east_asian_width(ch) in "WF":
                    row.append(("", style))
        rows.append(row)
    return rows


def mix(a, b, t):
    pa, pb = (int(a[i : i + 2], 16) for i in (1, 3, 5)), (int(b[i : i + 2], 16) for i in (1, 3, 5))
    return "#%02x%02x%02x" % tuple(round(x + (y - x) * t) for x, y in zip(pa, pb))


def colors(style):
    fg, bg = style.fg or FG, style.bg
    if style.reverse:
        fg, bg = bg or BG, fg
    if style.dim:
        fg = mix(fg, BG, 0.45)
    return fg, bg


def num(v):
    s = "%.2f" % v
    return s.rstrip("0").rstrip(".") if "." in s else s


class Classes:
    """Hands out short CSS class names for repeated styles."""

    def __init__(self, prefix):
        self.prefix, self.names = prefix, {}

    def __call__(self, key):
        return self.names.setdefault(key, "%s%d" % (self.prefix, len(self.names)))


H_LINES, V_LINES = {"─": 1, "━": 2}, {"│": 1, "┃": 2}
BARS, SLIVER, TRACK, SELECTED = "▇█", "▏", "░", "▌"
CORNERS = {
    "╭": "M{cx},{b}V{lo}A{r},{r} 0 0 1 {rx},{cy}H{R}",
    "╮": "M{L},{cy}H{lx}A{r},{r} 0 0 1 {cx},{lo}V{b}",
    "╰": "M{cx},{t}V{hi}A{r},{r} 0 0 0 {rx},{cy}H{R}",
    "╯": "M{L},{cy}H{lx}A{r},{r} 0 0 0 {cx},{hi}V{t}",
}


def cell_shape(ch, x, y, fill):
    """Returns the rect for a line or block glyph as (x, y, w, h, fill, rx, faint), or None."""
    if ch in H_LINES:
        w = H_LINES[ch]
        return (x, y + CH / 2 - w / 2, CW, w, fill, 0, False)
    if ch in V_LINES:
        w = V_LINES[ch]
        return (x + CW / 2 - w / 2, y, w, CH, fill, 0, False)
    bar_y = y + (CH - BAR_H) / 2
    if ch in BARS:
        return (x, bar_y, CW, BAR_H, fill, 1.5, False)
    if ch == TRACK:
        return (x, bar_y, CW, BAR_H, fill, 1.5, True)
    if ch == SLIVER:
        return (x, bar_y, 2, BAR_H, fill, 0.5, False)
    if ch == SELECTED:
        return (x + 1, y + 2, 3, CH - 4, fill, 1.5, False)
    return None


def merge(rects):
    """Joins rects that touch side by side, then ones that touch top to bottom."""
    out = []
    for r in sorted(rects, key=lambda r: (r[4], r[5], r[6], r[1], r[3], r[0])):
        p = out[-1] if out else None
        if p and p[4:] == r[4:] and p[1] == r[1] and p[3] == r[3] and abs(p[0] + p[2] - r[0]) < 0.01:
            out[-1] = (p[0], p[1], r[0] + r[2] - p[0], p[3]) + p[4:]
        else:
            out.append(r)
    joined = []
    for r in sorted(out, key=lambda r: (r[4], r[5], r[6], r[0], r[2], r[1])):
        p = joined[-1] if joined else None
        if p and p[4:] == r[4:] and p[0] == r[0] and p[2] == r[2] and abs(p[1] + p[3] - r[1]) < 0.01:
            joined[-1] = (p[0], p[1], p[2], r[1] + r[3] - p[1]) + p[4:]
        else:
            joined.append(r)
    return joined


def text_runs(row):
    """Splits a row into runs of one text style as (column, cells, text, style key)."""
    runs, cur = [], None
    for col, (ch, style) in enumerate(row):
        if ch == "":  # the second cell of a wide char
            if cur:
                cur[1][-1] += 1
            continue
        fg, _ = colors(style)
        key = (fg, style.bold, style.italic, style.underline)
        if ch == " " and cur and not style.underline:
            cur[0].append(col)
            cur[1].append(col + 1)
            cur[2] += " "
            continue
        if ch == " " or is_shape(ch):
            cur = None
            continue
        if not cur or cur[3] != key:
            cur = [[], [], "", key]
            runs.append(cur)
        cur[0].append(col)
        cur[1].append(col + 1)
        cur[2] += ch
    chunks = []
    for starts, ends, text, key in runs:
        # Long gaps split a run, so a font slightly off the grid cannot drift far.
        for m in re.finditer(r"\S+(?: \S+)*", text):
            first, last = starts[m.start()], ends[m.end() - 1]
            chunks.append((first, last - first, m.group(), key))
    return chunks


def is_shape(ch):
    return ch in H_LINES or ch in V_LINES or ch in CORNERS or ch in BARS or ch in (SLIVER, TRACK, SELECTED)


def render_screen(rows, tclass, fclass, sclass):
    rects, corners, texts = [], [], []
    for r, row in enumerate(rows):
        y = r * CH
        for c, (ch, style) in enumerate(row):
            x = c * CW
            fg, bg = colors(style)
            if bg:
                rects.append((x, y, CW, CH, bg, 0, False))
            shape = cell_shape(ch, x, y, fg)
            if shape:
                rects.append(shape)
            elif ch in CORNERS:
                cx, cy, rad = x + CW / 2, y + CH / 2, CW / 2
                d = CORNERS[ch].format(cx=num(cx), cy=num(cy), r=num(rad), t=num(y), b=num(y + CH), L=num(x),
                                       R=num(x + CW), lo=num(cy + rad), hi=num(cy - rad), lx=num(cx - rad),
                                       rx=num(cx + rad))
                corners.append((sclass(fg), d))
        for col, cells, text, key in text_runs(row):
            fit = ' textLength="%s"' % num(cells * CW) if cells > 1 else ""
            texts.append('<text class="%s" x="%s" y="%s"%s>%s</text>'
                         % (tclass(key), num(col * CW), num(y + BASELINE), fit, html.escape(text, quote=False)))
    out = []
    for x, y, w, h, fill, rx, faint in merge(rects):
        cls = fclass(fill) + (" fa" if faint else "")
        corner = ' rx="%s"' % num(rx) if rx and w > 2 * rx else ""
        out.append('<rect class="%s" x="%s" y="%s" width="%s" height="%s"%s/>' % (cls, num(x), num(y), num(w), num(h), corner))
    out += ['<path class="%s" d="%s"/>' % c for c in corners]
    return out + texts


def pct(t, total):
    return "%.3f%%" % (100 * t / total)


def find_label(screen, spec):
    """Returns the row, first column and width of the clickable part of a link label."""
    header = spec.startswith("^")
    spec = spec[1:] if header else spec
    m = re.fullmatch(r"(.*)\[(.*)\](.*)", spec)
    text, lo, width = (m[1] + m[2] + m[3], len(m[1]), len(m[2])) if m else (spec, 0, len(spec))
    rows = [0] if header else range(len(screen.rows) - 1, -1, -1)
    for r in rows:
        col = "".join(ch or "\0" for ch, _ in screen.rows[r]).find(text)
        if col >= 0:
            return r, col + lo, width
    sys.exit("ansi_svg: %s: %r is not on the screen" % (screen.name, spec))


def render_links(screen, by_name):
    out = []
    for spec, target in screen.links:
        r, col, width = find_label(screen, spec)
        dest = by_name[target]
        tip = "%s: %s" % (dest.key, dest.caption) if dest.key else dest.caption
        out.append('<a href="#%s"><title>%s</title><rect class="hit" x="%s" y="%s" width="%s" height="%s" rx="3"/></a>'
                   % (target, html.escape(tip, quote=False), num((col - 0.5) * CW), num(r * CH), num((width + 1) * CW), CH))
        out.append('<rect class="ul" x="%s" y="%s" width="%s" height="1"/>' % (num(col * CW), num(r * CH + CH - 1.5), num(width * CW)))
    return out


def window(width, height, cap_y):
    return [
        '<rect width="%s" height="%s" rx="10" fill="%s" stroke="%s"/>' % (num(width), num(height), BG, EDGE),
        '<path d="M0.5,%s V10.5 A10,10 0 0 1 10.5,0.5 H%s A10,10 0 0 1 %s,10.5 V%s Z" fill="%s"/>'
        % (TITLE_H, num(width - 10.5), num(width - 0.5), TITLE_H, CHROME),
        '<path d="M0.5,%s V%s A10,10 0 0 0 10.5,%s H%s A10,10 0 0 0 %s,%s V%s Z" fill="%s"/>'
        % (num(cap_y), num(height - 10.5), num(height - 0.5), num(width - 10.5), num(width - 0.5), num(height - 10.5),
           num(cap_y), CHROME),
        '<path d="M0,%sH%sM0,%sH%s" stroke="%s"/>' % (TITLE_H, num(width), num(cap_y), num(width), EDGE),
        *('<circle cx="%s" cy="%s" r="6" fill="%s"/>' % (20 + 20 * i, TITLE_H / 2, c) for i, c in enumerate(LIGHTS)),
        '<text class="ti" x="%s" y="%s" text-anchor="middle">tock</text>' % (num(width / 2), num(TITLE_H / 2 + 4.5)),
    ]


def build(screens, command, title):
    by_name = {s.name: s for s in screens}
    tour = [s for s in screens if s.seconds > 0] or screens[:1]
    targets = list(dict.fromkeys(t for s in screens for _, t in s.links))
    for t in targets:
        if t not in by_name:
            sys.exit("ansi_svg: no screen %r to link to" % t)

    cols = max(len(row) for s in screens for row in s.rows)
    lines = max(len(s.rows) for s in screens)
    width = cols * CW + 2 * PAD
    term_y = TITLE_H + PAD * 0.7
    cap_y = term_y + lines * CH + PAD * 0.7
    height = cap_y + CAPTION_H
    intro = INTRO_S if command else 0
    total = intro + sum(s.seconds for s in tour)
    looping = command or len(tour) > 1

    tclass, fclass, sclass = Classes("t"), Classes("f"), Classes("s")
    css = []
    # Markers come first: a link targets one, and CSS reaches the rest through it (#x:target ~ ...).
    body = ['<g id="%s" class="mk"/>' % t for t in targets]
    body += window(width, height, cap_y)

    if targets:
        hint_y = num(TITLE_H / 2 + 4)
        body.append('<g class="hint"><a href="#%s"><text class="hn he" x="%s" y="%s" text-anchor="end">\u25b8 click to explore</text></a>'
                    '<a href="#tour"><text class="hn hp" x="%s" y="%s" text-anchor="end">\u25b6 play the tour</text></a></g>'
                    % (targets[0], num(width - PAD), hint_y, num(width - PAD), hint_y))
        body.append('<text class="cap ex" x="%s" y="%s" text-anchor="end">click a tab or an underlined key</text>'
                    % (num(width - PAD), num(cap_y + CAPTION_H / 2 + 5)))

    starts, t = {}, intro
    for s in tour:
        starts[s.name] = t
        t += s.seconds

    # Progress: a segment per screen of the loop that fills while it is on screen.
    if len(tour) > 1:
        seg_w, gap = 26, 6
        seg_x = width - PAD - len(tour) * (seg_w + gap) + gap
        seg_y = cap_y + CAPTION_H / 2 - 2
        body.append('<g class="tour">')
        for i, s in enumerate(tour):
            x = seg_x + i * (seg_w + gap)
            a, b = starts[s.name], starts[s.name] + s.seconds
            body.append('<rect x="%s" y="%s" width="%s" height="4" rx="2" fill="%s"/>' % (num(x), num(seg_y), seg_w, EDGE))
            body.append('<rect class="pg p%d" x="%s" y="%s" width="%s" height="4" rx="2"/>' % (i, num(x), num(seg_y), seg_w))
            css.append("@keyframes p%d{0%%,%s{transform:scaleX(0)}%s,100%%{transform:scaleX(1)}}.p%d{animation-name:p%d}"
                       % (i, pct(a, total), pct(b, total), i, i))
        body.append("</g>")

    if command:
        body += intro_scene(command, term_y, cap_y)
        typed = len(command) * TYPE_STEP
        css.append("@keyframes intro{0%%{visibility:visible}%s,100%%{visibility:hidden}}.intro{animation-name:intro}"
                   % pct(intro, total))
        css.append("@keyframes ty{0%%,%s{transform:translateX(0);animation-timing-function:steps(%d,end)}%s,100%%"
                   "{transform:translateX(%spx)}}.ty{animation:ty %ss linear infinite}"
                   % (pct(TYPE_FROM, total), len(command), pct(TYPE_FROM + typed, total), num(len(command) * CW), num(total)))

    for i, s in enumerate(screens):
        cls = ["sc"]
        if s is tour[0]:
            cls.append("poster")
        if s.name in starts and looping:
            a, b = starts[s.name], starts[s.name] + s.seconds
            shown = "%s{visibility:visible}" % pct(a, total) if a else ""
            css.append("@keyframes k%d{0%%{visibility:%s}%s%s,100%%{visibility:hidden}}.k%d{animation-name:k%d}"
                       % (i, "hidden" if a else "visible", shown, pct(b, total), i, i))
            cls.append("k%d" % i)
        body.append('<g id="s-%s" class="%s">' % (s.name, " ".join(cls)))
        body.append('<g transform="translate(%s,%s)">' % (PAD, num(term_y)))
        body += render_screen(s.rows, tclass, fclass, sclass)
        body += render_links(s, by_name)
        body.append("</g>")
        body.append(caption_bar(s.key, s.caption, cap_y))
        body.append("</g>")

    style = [
        "text{font-family:%s;font-size:%spx;white-space:pre}" % (MONO, FS),
        ".ti{fill:%s;font-size:13px}" % MUTED,
        ".pr{fill:#87ff87}.cm{fill:%s}.cur{fill:%s}" % (FG, FG),
        ".cap{font-family:%s;font-size:14px;fill:%s}" % (SANS, MUTED),
        ".key{fill:%s;stroke:%s}.kt{fill:#fff;font-size:13px;font-weight:700}" % (EDGE, "#40435a"),
        ".fa{opacity:.45}",
        ".sc{visibility:hidden;animation-duration:%ss;animation-timing-function:step-end;animation-iteration-count:infinite}"
        % num(total),
        ".poster{visibility:visible}",
        ".pg{fill:%s;transform:scaleX(0);transform-box:fill-box;transform-origin:0 50%%;"
        "animation-duration:%ss;animation-timing-function:linear;animation-iteration-count:infinite}" % (ACCENT, num(total)),
        ".cur{animation:blink 1s step-end infinite}@keyframes blink{50%{opacity:0}}",
    ]
    if targets:
        style += [
            "a{cursor:pointer}.hit{fill:%s;fill-opacity:0;pointer-events:visibleFill}a:hover .hit{fill-opacity:.3}" % ACCENT,
            ".ul{fill:%s;display:none}" % ACCENT,
            ".hn{font-family:%s;font-size:14px;font-weight:600;fill:#9d9dff}a:hover .hn{fill:#fff}.hp,.ex{display:none}" % SANS,
            # A screen picked by a link replaces the loop; #tour (no such id) starts the loop again.
            ".mk:target~.sc{animation:none;visibility:hidden}.mk:target~.intro .ty{animation:none}",
            ".mk:target~.tour,.mk:target~.hint .he{display:none}",
            ".mk:target~.sc .ul,.mk:target~.hint .hp,.mk:target~.ex{display:inline}",
            "".join("#%s:target~#s-%s{visibility:visible}" % (t, t) for t in targets),
        ]
    style += [".%s{fill:%s%s%s%s}" % (name, k[0], ";font-weight:700" if k[1] else "", ";font-style:italic" if k[2] else "",
                                       ";text-decoration:underline" if k[3] else "") for k, name in tclass.names.items()]
    style += [".%s{fill:%s}" % (name, fill) for fill, name in fclass.names.items()]
    style += [".%s{fill:none;stroke:%s}" % (name, stroke) for stroke, name in sclass.names.items()]
    style += css
    style.append("@media (prefers-reduced-motion:reduce){.sc,.pg,.ty,.cur{animation:none}}")

    return "\n".join([
        '<svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="0 0 %s %s" role="img">'
        % (num(width), num(height), num(width), num(height)),
        "<title>%s</title>" % html.escape(title, quote=False),
        "<style>%s</style>" % "".join(style),
        *body,
        "</svg>",
        "",
    ])


def intro_scene(command, term_y, cap_y):
    """A prompt that types the command: the text sits under a cover that steps right."""
    x, y = PAD + CW, term_y + BASELINE
    cmd_x, cmd_w = x + 2 * CW, len(command) * CW
    return [
        '<g class="sc intro">',
        '<text class="pr" x="%s" y="%s">$</text>' % (num(x), num(y)),
        '<text class="cm" x="%s" y="%s" textLength="%s">%s</text>'
        % (num(cmd_x), num(y), num(cmd_w), html.escape(command, quote=False)),
        '<g class="ty"><rect class="cur" x="%s" y="%s" width="%s" height="%s"/>'
        '<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/></g>'
        % (num(cmd_x), num(term_y + 1), num(CW), CH - 2, num(cmd_x + CW), num(term_y), num(cmd_w + CW), CH, BG),
        caption_bar(None, INTRO_CAPTION, cap_y),
        "</g>",
    ]


def caption_bar(key, caption, cap_y):
    mid = cap_y + CAPTION_H / 2
    x, out = PAD, []
    if key:
        w = len(key) * CW + 18
        out.append('<rect class="key" x="%s" y="%s" width="%s" height="24" rx="5"/>' % (num(x), num(mid - 12), num(w)))
        out.append('<text class="kt" x="%s" y="%s" text-anchor="middle">%s</text>'
                   % (num(x + w / 2), num(mid + 4.5), html.escape(key, quote=False)))
        x += w + 12
    out.append('<text class="cap" x="%s" y="%s">%s</text>' % (num(x), num(mid + 5), html.escape(caption, quote=False)))
    return "".join(out)


def read_screens(tsv):
    screens = []
    for line in tsv.read_text(encoding="utf-8").splitlines():
        name, capture, seconds, key, caption, links = line.split("\t")
        pairs = [tuple(item.rsplit("=", 1)) for item in links.split(";") if item]
        screens.append(Screen(name, read_screen(tsv.parent / capture), float(seconds), key, caption, pairs))
    return screens


def main():
    parser = argparse.ArgumentParser(description="Turns tmux screen captures into an animated SVG.")
    parser.add_argument("screens", type=Path, help="tsv: name, capture, seconds, key, caption, links")
    parser.add_argument("--type", metavar="COMMAND", help="type this command at a prompt first")
    parser.add_argument("--title", default="tock in the terminal", help="the SVG title")
    args = parser.parse_args()
    sys.stdout.write(build(read_screens(args.screens), args.type, args.title))


if __name__ == "__main__":
    main()
