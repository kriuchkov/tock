# Sourced by demo/*.tape and demo/demo_svg.sh: builds tock into a temp dir and seeds sample data there.
# HOME points at the temp dir before anything runs, so even a failed build never lets
# another tock on PATH read or write real data.

# Go keeps its caches and settings from the real HOME.
GOPATH="$(go env GOPATH)" GOMODCACHE="$(go env GOMODCACHE)" GOCACHE="$(go env GOCACHE)" GOENV="$(go env GOENV)"
export GOPATH GOMODCACHE GOCACHE GOENV

DEMO_DIR="$(mktemp -d)"
export HOME="$DEMO_DIR"
export PATH="$DEMO_DIR:$PATH"
export TOCK_FILE="$DEMO_DIR/tock.txt"
export TOCK_CHECK_UPDATES=false

# -mod=mod: a stale local vendor/ (it is gitignored) must not break the build.
if ! go build -mod=mod -o "$DEMO_DIR/tock" ./cmd/tock; then
  echo "demo: go build failed" >&2
  return 1
fi

day() { date -v"$1"d +%F 2>/dev/null || date -d "$1 day" +%F; }

{
  for back in 4 3 2 1; do
    d=$(day "-$back")
    tock add -p "Project Alpha" -d "Scoping"    -s "$d 09:00" -e "$d 12:00"
    tock add -p "Management"    -d "Team sync"  -s "$d 12:30" -e "$d 13:15"
    tock add -p "Project Beta"  -d "API design" -s "$d 14:00" -e "$d 17:30"
  done

  tock remind add "Standup" -p Management --at 09:30 --every weekdays --notify 5m
  tock remind add "Send invoice" -p Admin --on "$(day -31)" --at 13:00 --every monthly --tag money
  tock remind add "Renew domain" --every yearly
  tock remind add "Dentist" --on "$(day +1)" --at 15:00 --notify 1h
  tock remind add "Release v1.9" -p "Project Beta" --on "$(day +3)" --at 10:00
  tock remind add "Water plants" --on "$(day -2)" --every 3d

  today=$(day -0)
  tock add -p "Admin" -d "Email & planning" -s "$today 08:30" -e "$today 09:15"
  tock remind start 1 -t 09:30 && tock stop -t 09:45
  tock add -p "Project Alpha" -d "Implementation" -s "$today 10:00" -e "$today 12:45"
} >/dev/null

unset -f day
clear
