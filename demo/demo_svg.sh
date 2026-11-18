#!/bin/bash
# Records assets/demo.svg, the animated TUI at the top of the README: runs tock in a detached
# tmux on the sample data of demo_seed.sh, captures each screen with its colors and turns the
# captures into one SVG with demo/ansi_svg.py.
# Run from the repository root: demo/demo_svg.sh [out.svg]   (needs tmux and python3)
# DEMO_FRAMES=dir keeps the captured screens there.
set -euo pipefail

cols=140
rows=30
out="${1:-assets/demo.svg}"
frames="${DEMO_FRAMES:-$(mktemp -d)}"
mkdir -p "$frames"
rm -f "$frames"/*.ansi "$frames/scenes.tsv" "$frames/ready"
sock="tock-demo-$$"

tm() { tmux -u -L "$sock" -f /dev/null "$@"; }
cleanup() {
  tm kill-server 2>/dev/null || true
  [[ -n "${DEMO_FRAMES:-}" ]] || rm -rf "$frames"
}
trap cleanup EXIT

tm new-session -d -s demo -x "$cols" -y "$rows" -c "$PWD" \
  "$(printf '%q ' env -i HOME="$HOME" PATH="$PATH" TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8 bash --norc --noprofile)"

# The running activity starts a little before now, after the seeded ones end at 12:45.
start=$(date -v-25M +%H:%M 2>/dev/null || date -d '-25 min' +%H:%M)
[[ "$start" > "12:45" ]] || start=$(date +%H:%M)
tm send-keys -t demo "source demo/demo_seed.sh && \
tock remind add 'Team sync' -p Management --at 11:00 --every weekly:mon,thu >/dev/null && \
tock start -p 'Project Alpha' -d 'Release notes' -t $start >/dev/null && clear && touch $frames/ready" Enter
for _ in $(seq 180); do [[ -e "$frames/ready" ]] && break; sleep 1; done
[[ -e "$frames/ready" ]] || { echo "demo: seeding timed out" >&2; exit 1; }

n=0
# shot SECONDS KEY CAPTION: the current screen becomes the next scene, shown for SECONDS
# under the KEY that led to it.
shot() {
  n=$((n + 1))
  tm capture-pane -t demo -e -p > "$frames/$n.ansi"
  printf '%s\t%s\t%s\t%s\n' "$n.ansi" "$1" "$2" "$3" >> "$frames/scenes.tsv"
}
key() { tm send-keys -t demo "$@"; sleep 1.2; }

key "tock calendar" Enter
sleep 2
shot 4.0 "enter" "month, day and what's coming up"
key Tab
shot 2.6 "tab" "select a reminder"
key Space
shot 2.4 "space" "mark it done"
key 2
shot 2.6 "2" "reminders, overdue first"
key Tab
shot 2.8 "tab" "the week ahead"
key 3
shot 2.6 "3" "the day's activities"
key Tab
shot 2.8 "tab" "the week, by project"
key 4
sleep 1
shot 4.0 "4" "how you really work"
key q

python3 demo/ansi_svg.py "$frames/scenes.tsv" > "$out"
echo "wrote $out"
