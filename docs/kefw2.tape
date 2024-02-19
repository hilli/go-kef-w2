Set Shell zsh
Set FontSize 14
Set WindowBar Colorful
Set Height 1000
Set Width 800
Output kefw2.gif
Output kefw2.mp4
#Output kefw2.webm
Require kefw2
Require osascript

Hide
Type@0ms "source ~/.zshrc && clear"
Enter
Show
Type "kefw2 --help"
Enter
Sleep 2.5s
Type "kefw2 config speaker discover --save"
Enter
Sleep 2s
Type "kefw2 config speaker list"
Enter
Sleep 3ms
Type "kefw2 config speaker default "
Tab@500ms 4
Enter
Sleep 200ms
Enter
Sleep 500ms
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
Hide
Type "echo Playing some music from Apple Music via AirPlay..."
Enter
Show
Sleep 1s
Type "./airplay-music.jxa"
Enter
Sleep 5s
Type "kefw2 status"
Enter
Sleep 5s
Type "kefw2 next"
Enter
Sleep 500ms
Type "kefw2 status"
Enter
Sleep 1s
Type "kefw2 pause"
Enter
Sleep 1s
Type "kefw2 vol "
Tab@500ms 6
Enter
Enter
Sleep 500ms
Type "kefw2 vol"
Enter
Sleep 500ms
Type "kefw2 off"
Enter
Sleep 2s
