# Records assets/demo.gif. Run from the repository root: vhs demo/demo.tape
# Needs vhs (https://github.com/charmbracelet/vhs) and Go; data lives in a temp dir.

Output assets/demo.gif

Require go

Set Shell bash
Set FontSize 15
Set Width 1400
Set Height 900
Set Padding 16
Set Framerate 15
Set TypingSpeed 35ms

Hide
Type "source demo/demo_seed.sh" Enter
Wait@180s
Show

Sleep 500ms
Type "tock start -p 'Project Alpha' -d 'Release notes'" Sleep 400ms Enter
Sleep 2s

Type "tock remind add 'Team sync' -p Management --at 17:30 --every weekly:mon,thu --notify 10m" Sleep 400ms Enter
Sleep 2s

Type "tock remind list" Sleep 300ms Enter
Sleep 4s

Ctrl+L
Type "tock remind" Sleep 300ms Enter
Sleep 2.5s
Down Sleep 700ms
Down Sleep 700ms
Space Sleep 1.5s
Tab Sleep 2s
Tab Sleep 2s
Type "q" Sleep 500ms

Type "tock calendar" Sleep 300ms Enter
Sleep 3s
Tab Sleep 1.5s
Down Sleep 700ms
Space Sleep 1.5s
Tab Sleep 700ms
Right Sleep 1.5s
Right Sleep 2s
Type "q" Sleep 500ms

Type "tock stop" Sleep 300ms Enter
Sleep 2.5s
