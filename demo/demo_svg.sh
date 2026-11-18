#!/bin/bash
# Records the animated SVGs of the README: assets/demo.svg at the top and one per tab under it
# (demo_calendar, demo_reminders, demo_list, demo_analyze). Runs tock in a detached tmux on the
# sample data of demo_seed.sh, captures each screen with its colors and turns the captures into
# SVGs with demo/ansi_svg.py. demo.svg also holds the screens its links lead to, so it can be
# explored by clicking when opened on its own.
# Run from the repository root: demo/demo_svg.sh [out_dir]   (needs tmux and python3)
# DEMO_FRAMES=dir keeps the captured screens there.
set -euo pipefail

cols=140
rows=30
out="${1:-assets}"
frames="${DEMO_FRAMES:-$(mktemp -d)}"
mkdir -p "$frames"
frames="$(cd "$frames" && pwd)"
rm -f "$frames/ready" "$frames/home"
sock="tock-demo-$$"

tm() { tmux -u -L "$sock" -f /dev/null "$@"; }
cleanup() {
  tm kill-server 2>/dev/null || true
  [[ -n "${DEMO_FRAMES:-}" ]] || rm -rf "$frames"
}
trap cleanup EXIT

tm new-session -d -s demo -x "$cols" -y "$rows" -c "$PWD" \
  "$(printf '%q ' env -i HOME="$HOME" PATH="$PATH" TERM=xterm-256color LANG=en_US.UTF-8 bash --norc --noprofile)"
tm set-option -g remain-on-exit on

# The running activity starts a little before now, after the seeded ones end at 12:45.
start=$(date -v-25M +%H:%M 2>/dev/null || date -d '-25 min' +%H:%M)
[[ "$start" > "12:45" ]] || start=$(date +%H:%M)
tm send-keys -t demo "source demo/demo_seed.sh && \
tock remind add 'Team sync' -p Management --at 11:00 --every weekly:mon,thu >/dev/null && \
tock start -p 'Project Alpha' -d 'Release notes' -t $start >/dev/null && \
echo \"\$HOME\" > $frames/home && touch $frames/ready" Enter
for _ in $(seq 180); do [[ -e "$frames/ready" ]] && break; sleep 1; done
[[ -e "$frames/ready" ]] || { echo "demo: seeding timed out" >&2; exit 1; }
demo="$(cat "$frames/home")"

# run COMMAND...: restarts the pane with COMMAND in the temp HOME, so a tock.yaml in the
# repository root cannot reach the recording.
run() {
  tm respawn-pane -k -t demo "$(printf '%q ' env -i HOME="$demo" PATH="$demo:/usr/bin:/bin" \
    TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8 TOCK_FILE="$demo/tock.txt" TOCK_CHECK_UPDATES=false \
    sh -c 'cd "$HOME" && exec "$@"' sh "$@")"
  sleep 2.5
}
key() { tm send-keys -t demo "$@"; sleep 1.2; }
# into NAME: the next shots go to $frames/NAME.
into() {
  dir="$frames/$1"
  rm -rf "$dir" && mkdir -p "$dir"
}
# shot NAME SECONDS KEY CAPTION [LINKS]: the current screen becomes screen NAME, shown for SECONDS
# in the loop (0 leaves it to links) under the KEY that leads to it. LINKS: see ansi_svg.py.
shot() {
  tm capture-pane -t demo -e -p > "$dir/$1.ansi"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" "$1.ansi" "$2" "$3" "$4" "${5:-}" >> "$dir/screens.tsv"
}
# state NAME SECONDS COMMAND KEY CAPTION LINKS [KEYS...]: runs COMMAND, presses KEYS and shoots.
state() {
  local name=$1 seconds=$2 cmd=$3 key=$4 caption=$5 links=$6
  shift 6
  run $cmd
  for k in "$@"; do key "$k"; done
  sleep 0.8
  shot "$name" "$seconds" "$key" "$caption" "$links"
}

# A walk through each tab, for the sections under the demo.
into calendar
run tock calendar
shot 1 3.0 "tock calendar" "today: reminders, tracked time and the week"
key Left
shot 2 2.6 "←" "the day before"
key Right
key Right
shot 3 2.6 "→ →" "tomorrow: what is due and when"
key n
shot 4 2.4 "n" "next month"
key p
key a
shot 5 3.0 "a" "add a reminder"

into reminders
run tock remind
shot 1 2.8 "tock remind" "today's reminders, overdue first"
key Tab
shot 2 2.6 "tab" "the week ahead"
key Tab
shot 3 2.4 "tab" "only the overdue ones"
key Tab
shot 4 2.6 "tab" "all of them, with their repeats"
key j
key e
shot 5 3.0 "j e" "edit the next one"

into list
run tock list
shot 1 2.6 "tock list" "the day's activities"
key e
shot 2 3.0 "e" "fix an entry in place"
key Escape
key Tab
shot 3 2.6 "tab" "the week, by project"
key Tab
shot 4 2.4 "tab" "the month"
key Tab
shot 5 2.4 "tab" "the year"

into analyze
state 1 4.0 "tock calendar" "4" "how you really work" "" 4

# The demo: a loop through all tabs, then the screens its links lead to. The loop goes first,
# because it marks a reminder done and the screens after it show that.
into demo
tabs="^calendar=cal;^reminders=rem;^list=list;^analyze=ana"
cal_keys="[←]→ day=cal-prev;←[→ day]=cal-next;↑↓ week=cal-down;n/p month=cal-n;j/k select=cal-sel;tab section=cal-sel"
cal_keys+=";a add reminder=cal-add"
sel_keys="j/k row=cal-sel2;e edit=cal-edit;esc back=cal"

run tock calendar
shot tour-cal 4.0 "enter" "month, day and what's coming up" "$tabs;$cal_keys"
key Tab
shot tour-sel 2.6 "tab" "select a reminder" "$tabs;$sel_keys;space done=tour-done"
key Space
shot tour-done 2.4 "space" "mark it done" "$tabs;j/k row=cal-sel;esc back=cal"

state cal 0 "tock calendar" "1" "the calendar: today, reminders and the week" "$tabs;$cal_keys"
state cal-prev 0 "tock calendar" "←" "the day before" "$tabs;←[→ day]=cal" Left
state cal-next 0 "tock calendar" "→" "tomorrow: what is due and when" "$tabs;[←]→ day=cal" Right
state cal-down 0 "tock calendar" "↓" "a week later" "$tabs;↑↓ week=cal" Down
state cal-n 0 "tock calendar" "n" "next month" "$tabs;n/p month=cal" n
state cal-sel 0 "tock calendar" "tab" "select a reminder" "$tabs;$sel_keys" Tab
state cal-sel2 0 "tock calendar" "j" "the next one" "$tabs;j/k row=cal-sel;esc back=cal" Tab j
state cal-edit 0 "tock calendar" "e" "edit it in place" "$tabs;esc cancel=cal-sel" Tab e
state cal-add 0 "tock calendar" "a" "add a reminder" "$tabs;esc cancel=cal" a
state rem 2.6 "tock remind" "2" "reminders, overdue first" "$tabs;tab view=rem-week;e edit=rem-edit"
state rem-week 2.8 "tock remind" "tab" "the week ahead" "$tabs;tab view=rem-over;e edit=rem-edit" Tab
state rem-over 0 "tock remind" "tab" "only the overdue ones" "$tabs;tab view=rem-all" Tab Tab
state rem-all 0 "tock remind" "tab" "all of them, with their repeats" "$tabs;tab view=rem" Tab Tab Tab
state rem-edit 0 "tock remind" "e" "edit it in place" "$tabs;esc cancel=rem" e
state list 2.6 "tock list" "3" "the day's activities" "$tabs;tab view=list-week;e edit=list-edit;[←]→ day=list-prev"
state list-prev 0 "tock list" "←" "the day before" "$tabs;←[→ day]=list" Left
state list-edit 0 "tock list" "e" "fix an entry in place" "$tabs;esc back=list" e
state list-week 2.8 "tock list" "tab" "the week, by project" "$tabs;tab view=list-month" Tab
state list-month 0 "tock list" "tab" "the month" "$tabs;tab view=list-year" Tab Tab
state list-year 0 "tock list" "tab" "the year" "$tabs;tab view=list" Tab Tab Tab
state ana 4.0 "tock calendar" "4" "how you really work" "$tabs" 4

svg() {
  local name="$1" file="$2"
  shift 2
  python3 demo/ansi_svg.py "$frames/$name/screens.tsv" "$@" > "$out/$file"
  echo "wrote $out/$file"
}
svg demo demo.svg --type "tock calendar" --title "tock: calendar, reminders, list and analyze in the terminal"
svg calendar demo_calendar.svg --title "tock calendar: days, months and adding a reminder"
svg reminders demo_reminders.svg --title "tock remind: today, the week, overdue and all reminders"
svg list demo_list.svg --title "tock list: a day, a week, a month and a year of activities"
svg analyze demo_analyze.svg --title "tock analyze: focus, rhythm and where the time goes"
