# Takes assets/demo.png, the calendar screenshot at the top of the README.
# Run from the repository root: vhs demo/screenshot.tape (data lives in a temp dir, see demo_seed.sh).

Output demo/screenshot.gif

Require go

Set Shell bash
Set FontSize 26
Set Width 2600
Set Height 1580
Set Padding 32
Set Framerate 10

Hide
Type "source demo/demo_seed.sh" Enter
Wait@180s
Type "tock remind add 'Team sync' -p Management --at 11:00 --every weekly:mon,thu >/dev/null && tock remind done 7 >/dev/null && clear" Enter
Wait@60s
Type "tock calendar" Enter
Sleep 3s
Show

Sleep 1s
Screenshot assets/demo.png
Sleep 1s
