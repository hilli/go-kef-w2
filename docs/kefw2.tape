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
Type@0ms "source ~/.zshrc"
Enter
Type@0ms "clear"
Enter
Env PS1 "$> "
Show

# Introduction
Type "kefw2 --help"
Enter
Sleep 2.5s

# Speaker discovery and configuration
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

# Basic status and source control
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

# AirPlay demo
Hide
Type "echo Playing some music from Apple Music via AirPlay..."
Enter
Show
Sleep 1s
Type "./airplay-music.jxa"
Enter
Sleep 5s
Type "kefw2 status -a"
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

# Volume control with tab completion
Type "kefw2 vol "
Tab@500ms 6
Enter
Enter
Sleep 500ms
Type "kefw2 vol"
Enter
Sleep 1s

# --- NEW FEATURES ---

# Internet Radio with tab completion
Type "# Internet Radio"
Enter
Sleep 500ms
Type "kefw2 radio --help"
Enter
Sleep 2s
Type "kefw2 radio popular"
Enter
Sleep 3s
Type "kefw2 radio browse "
Tab@500ms 4
Enter
Sleep 2s
Type "kefw2 pause"
Enter
Sleep 500ms

# Podcast browsing
Type "# Podcasts"
Enter
Sleep 500ms
Type "kefw2 podcast --help"
Enter
Sleep 2s
Type "kefw2 podcast trending"
Enter
Sleep 3s
Type "kefw2 podcast browse "
Tab@500ms 4
Enter
Sleep 2s
Type "kefw2 pause"
Enter
Sleep 500ms

# UPnP/DLNA media servers
Type "# UPnP/DLNA Media Servers"
Enter
Sleep 500ms
Type "kefw2 upnp --help"
Enter
Sleep 2s
Type "kefw2 upnp browse "
Tab@500ms 4
Enter
Sleep 2s
Escape
Sleep 500ms

# Cache management
Type "# Cache Management for Tab Completion"
Enter
Sleep 500ms
Type "kefw2 cache status"
Enter
Sleep 1.5s
Type "kefw2 cache clear"
Enter
Sleep 1s

# Event streaming
Type "# Real-time Event Streaming"
Enter
Sleep 500ms
Type "kefw2 events --help"
Enter
Sleep 2s
Type "kefw2 events --timeout 5"
Enter
Sleep 6s

# Wrap up
Type "kefw2 off"
Enter
Sleep 2s
