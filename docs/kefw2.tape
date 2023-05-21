Set Shell zsh
Output kefw2.gif
Output kefw2.mp4
Require kefw2
Require osascript
Set FontSize 14

#Type "kefw2 completion zsh > /tmp/cmpzsh && source /tmp/cmpzsh"
#Enter
#Sleep 500ms
Type "kefw2 --help"
Enter
Sleep 2.5s
Type "kefw2 config speaker add 10.0.0.149"
Enter
Sleep 500ms
Type "kefw2 config speaker add 10.0.0.93"
Enter
Sleep 500ms
Type "kefw2 config speaker list"
Enter
Sleep 3ms
Type "kefw2 status"
Enter
Sleep 2s
Type "kefw2 source usb"
Enter
Sleep 500ms
Type "kefw2 status"
Enter
Sleep 2s
Type" kefw2 source wifi"
Enter
Sleep 500ms
Type "kefw2 source"
Enter
Sleep 2s
Type "echo Playing some music from Apple Music via AirPlay..."
Enter
Sleep 10ms
Type "./airplay-music.jxa"
Enter
Sleep 5s
Type "kefw2 status"
Enter
Sleep 5s
Type "kefw2 pause"
Enter
Sleep 1s
Type "kefw2 vol 30"
Enter
Sleep 1s
Type "kefw2 vol"
Enter
Sleep 500ms
Type "kefw2 off"
Enter
Sleep 2s
