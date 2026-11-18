#!/bin/bash
# Records the animated SVGs of the README: assets/demo.svg at the top and one per tab under it
# (demo_calendar, demo_reminders, demo_list, demo_analyze). Runs tock in a detached tmux on the
# sample data of demo_seed.sh, captures each screen with its colors and turns the captures into
# SVGs with demo/ansi_svg.py.
# Run from the repository root: demo/demo_svg.sh [out_dir]   (needs tmux and python3)
# DEMO_FRAMES=dir keeps the captured screens there.
set -euo pipefail

cols=140
rows=30
out="${1:-assets}"
frames="${DEMO_FRAMES:-$(mktemp -d)}"
mkdir -p "$frames"
frames="$(cd "$frames" && pwd)"
rm -f "$frames/ready"
sock="tock-demo-$$"

tm() { tmux -u -L "$sock" -f /dev/null "$@"; }
cleanup() {
  tm kill-server 2>/dev/null || true
  [[ -n "${DEMO_FRAMES:-}" ]] || rm -rf "$frames"
}
trap cleanup EXIT

tm new-session -d -s demo -x "$cols" -y "$rows" -c "$PWD" \
  "$(printf '%q ' env -i HOME="$HOME" PATH="$PATH" TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8 bash --norc --noprofile)"

# The running activity starts a little before now, after the seeded ones end at 12:45. tock then
# runs in the temp HOME, so a tock.yaml in the repository root cannot reach the recording.
start=$(date -v-25M +%H:%M 2>/dev/null || date -d '-25 min' +%H:%M)
[[ "$start" > "12:45" ]] || start=$(date +%H:%M)
tm send-keys -t demo "source demo/demo_seed.sh && \
tock remind add 'Team sync' -p Management --at 11:00 --every weekly:mon,thu >/dev/null && \
tock start -p 'Project Alpha' -d 'Release notes' -t $start >/dev/null && cd \"\$HOME\" && clear && touch $frames/ready" Enter
for _ in $(seq 180); do [[ -e "$frames/ready" ]] && break; sleep 1; done
[[ -e "$frames/ready" ]] || { echo "demo: seeding timed out" >&2; exit 1; }

# take NAME COMMAND: starts a recording of COMMAND; its screens go to $frames/NAME.
take() {
  dir="$frames/$1"
  n=0
  rm -rf "$dir" && mkdir -p "$dir"
  key "clear; $2" Enter
  sleep 2
}
# shot SECONDS KEY CAPTION: the current screen becomes the next scene, shown for SECONDS
# under the KEY that led to it.
shot() {
  n=$((n + 1))
  tm capture-pane -t demo -e -p > "$dir/$n.ansi"
  printf '%s\t%s\t%s\t%s\n' "$n.ansi" "$1" "$2" "$3" >> "$dir/scenes.tsv"
}
key() { tm send-keys -t demo "$@"; sleep 1.2; }

# The tabs first: they leave the data as it was.
take calendar "tock calendar"
shot 3.0 "tock calendar" "today: reminders, tracked time and the week"
key Left
shot 2.6 "←" "the day before"
key Right
key Right
shot 2.6 "→ →" "tomorrow: what is due and when"
key n
shot 2.4 "n" "next month"
key p
key a
shot 3.0 "a" "add a reminder"
key Escape
key q

take reminders "tock remind"
shot 2.8 "tock remind" "today's reminders, overdue first"
key Tab
shot 2.6 "tab" "the week ahead"
key Tab
shot 2.4 "tab" "only the overdue ones"
key Tab
shot 2.6 "tab" "all of them, with their repeats"
key j
key e
shot 3.0 "j e" "edit the next one"
key Escape
key q

take list "tock list"
shot 2.6 "tock list" "the day's activities"
key e
shot 3.0 "e" "fix an entry in place"
key Escape
key Tab
shot 2.6 "tab" "the week, by project"
key Tab
shot 2.4 "tab" "the month"
key Tab
shot 2.4 "tab" "the year"
key q

take analyze "tock calendar"
key 4
sleep 1
shot 4.0 "4" "how you really work"
key q

# The walk through all tabs last: it marks a reminder done.
take demo "tock calendar"
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

svg() {
  local name="$1" file="$2"
  shift 2
  python3 demo/ansi_svg.py "$frames/$name/scenes.tsv" "$@" > "$out/$file"
  echo "wrote $out/$file"
}
svg demo demo.svg --type "tock calendar" --title "tock: calendar, reminders, list and analyze in the terminal"
svg calendar demo_calendar.svg --title "tock calendar: days, months and adding a reminder"
svg reminders demo_reminders.svg --title "tock remind: today, the week, overdue and all reminders"
svg list demo_list.svg --title "tock list: a day, a week, a month and a year of activities"
svg analyze demo_analyze.svg --title "tock analyze: focus, rhythm and where the time goes"
