#!/bin/bash

# Auto-start asciinema recording if not already running
if [ -z "$ASCIINEMA_REC" ]; then
    OUTPUT_FILE="./demo/demo.cast"
    
    # Remove existing recording file
    rm -f "$OUTPUT_FILE"
    echo "Starting asciinema recording to $OUTPUT_FILE..."

    # Re-run this script inside asciinema, setting the flag to prevent infinite loop
    asciinema rec -c "env ASCIINEMA_REC=1 $0" "$OUTPUT_FILE"
    echo "Recording finished. Saved to $OUTPUT_FILE"
    exit 0
fi

# Configuration
# Run from the root of the repository
BINARY="./bin/tock"
DATA_FILE="./demo/demo_data.txt"
export TOCK_FILE="$DATA_FILE"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color
YELLOW='\033[0;33m'
WHITE='\033[1;37m'

# Helper function to print command and wait for enter
p() {
    echo -e "${GREEN}$ $1${NC}"
    read -r
}

# Wait for enter then clear
c() {
    read -r
    clear
}

run() {
    p "$1"
    eval "$1"
    echo ""
}

# Setup
mkdir -p demo
rm -f "$DATA_FILE"

# Calculate dates for sample data (macOS compatible)
D0=$(date +%Y-%m-%d)        # Today
D1=$(date -v-1d +%Y-%m-%d)  # Yesterday
D2=$(date -v-2d +%Y-%m-%d)  # 2 days ago
D3=$(date -v-3d +%Y-%m-%d)  # 3 days ago

echo "Injecting sample data for the current week..."
cat <<EOF >> "$DATA_FILE"
$D3 09:00 - $D3 12:00 | Project Alpha | Scoping
$D3 13:00 - $D3 17:00 | Project Beta | API Design
$D2 10:00 - $D2 11:30 | Management | Team Sync
$D2 12:30 - $D2 16:00 | Project Alpha | Implementation
$D1 09:00 - $D1 13:00 | Project Beta | Database Migration
$D1 14:00 - $D1 17:30 | Support | Incident #123
$D0 08:00 - $D0 09:30 | Admin | Email & Planning
EOF

echo "Building tock..."
go build -o "$BINARY" cmd/tock/main.go

clear
echo "Welcome to Tock Demo Session"
echo "Using data file: $TOCK_FILE"
echo "Press ENTER to execute each step."
echo ""

c
# 1. Start a task with positional arguments (New Feature)
echo -e "${WHITE}# First: Start a new activity with project and description as positional arguments.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock start                                   # Interactive mode${NC}"
echo -e "${GREEN}#   tock start 'Project Name' 'Task description' # Positional arguments${NC}"
echo -e "${GREEN}#   tock start -p 'Project' -d 'Task' -t 14:30   # Start at specific time${NC}"
echo -e ""
run "$BINARY start 'Project Alpha' 'Initial Research'"

c
# 2. Show current status in JSON (New Feature)
echo -e "${WHITE}# Next: Show current activity in different formats.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock current                                 # Standard output${NC}"
echo -e "${GREEN}#   tock current --json                          # Machine-readable JSON${NC}"
echo -e "${GREEN}#   tock current --format '{{.Project}}'         # Custom Go template${NC}"
echo -e ""
run "$BINARY current"

c
# 3. Stop the task
echo -e "${WHITE}# Next: Stop the current activity.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock stop                                    # Stop now${NC}"
echo -e "${GREEN}#   tock stop -t 17:00                           # Stop at specific time${NC}"
echo -e ""
run "$BINARY stop"

c
# 3a. Continue the last task (New Feature)
echo -e "${WHITE}# Next: Continue the last activity.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock continue                                # Continue last activity${NC}"
echo -e "${GREEN}#   tock continue 1                              # Continue 2nd to last${NC}"
echo -e "${GREEN}#   tock continue -d 'New Task'                  # Continue with new details${NC}"
echo -e ""
run "$BINARY continue"

c
# 4. Watch mode with auto-stop (New Feature)
echo -e "${WHITE}# Next: Use watch mode to monitor the current activity.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock watch                                   # Standard stopwatch${NC}"
echo -e "${GREEN}#   tock watch --stop                            # Stop activity on exit${NC}"
echo -e ""
run "$BINARY watch --stop"

c
# 5. Add a past entry interactively (New Feature)
echo -e "${WHITE}# Next: Add a past activity entry.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock add                                     # Interactive wizard${NC}"
echo -e "${GREEN}#   tock add -p 'Proj' -d 'Task' -s 10:00 -e 11:00  # Manual entry${NC}"
echo -e ""
run "$BINARY add"

c
# 6. Generate Report (New Feature: Description Filter)
echo -e "${WHITE}# Next: Report filtering.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock report                                  # Report for today${NC}"
echo -e "${GREEN}#   tock report --description 'Task'             # Filter by description${NC}"
echo -e "${GREEN}#   tock report --summary                        # Project totals only${NC}"
echo -e "${GREEN}#   tock report --json                           # JSON output${NC}"
echo -e ""
run "$BINARY report --description 'Initial Research'"

c
# 7. Calendar View
echo -e "${WHITE}# Next: Calendar view of activities.${NC}"
echo -e "${GREEN}# Variations available:${NC}"
echo -e "${GREEN}#   tock calendar                                # TUI Calendar view${NC}"
echo -e ""
run "$BINARY calendar"

# To end the recording session simply exit the shell. This can be done by pressing ctrl+d or entering exit.
run "exit"

echo -e "${GREEN}Demo Complete!${NC}"
