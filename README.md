# go-kef-w2

CLI, library and apps (planed) for controlling KEFs W2 platform based speakers over the network.

![kefw2 demo](https://github.com/hilli/go-kef-w2/assets/11922/a79f17bc-9c27-4b79-9f59-7be626265483)

**Acutal cover art displayed if the terminal supports images**

![Acutal cover art displayed if the terminal supports images](https://private-user-images.githubusercontent.com/11922/423059134-2740f445-2451-42b3-9638-e65025024b33.png?jwt=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJnaXRodWIuY29tIiwiYXVkIjoicmF3LmdpdGh1YnVzZXJjb250ZW50LmNvbSIsImtleSI6ImtleTUiLCJleHAiOjE3NDIwMjk1NDMsIm5iZiI6MTc0MjAyOTI0MywicGF0aCI6Ii8xMTkyMi80MjMwNTkxMzQtMjc0MGY0NDUtMjQ1MS00MmIzLTk2MzgtZTY1MDI1MDI0YjMzLnBuZz9YLUFtei1BbGdvcml0aG09QVdTNC1ITUFDLVNIQTI1NiZYLUFtei1DcmVkZW50aWFsPUFLSUFWQ09EWUxTQTUzUFFLNFpBJTJGMjAyNTAzMTUlMkZ1cy1lYXN0LTElMkZzMyUyRmF3czRfcmVxdWVzdCZYLUFtei1EYXRlPTIwMjUwMzE1VDA5MDA0M1omWC1BbXotRXhwaXJlcz0zMDAmWC1BbXotU2lnbmF0dXJlPTUwOTZlYTc3NWUwNDhhNTU4OThmNWYzMjIzYjNjMDEwOTQ4ZDFjZjVhZjVkYTVjNjQ5ZGNkNjcwMTVjNmQ1NDgmWC1BbXotU2lnbmVkSGVhZGVycz1ob3N0In0.ffWRzYj346vlnaUsMyUFHbhJfh3g6v_j8gt2t_al2nU)

## Command line tool

### Installation

#### General

Grap a version for your OS from the [releases](https://github.com/hilli/go-kef-w2/releases) page.

#### macOS

Install with Homebrew:

```shell
brew install hilli/tap/kefw2
```

#### Linux

Install with Homebrew:

```shell
brew install hilli/tap/kefw2
```

#### Windows

Install with [Scoop](https://scoop.sh/)

In a Windows Powershell window:

```shell
# Install Scoop if it's not already on your system
Set-ExecutionPolicy RemoteSigned -Scope CurrentUser
Invoke-RestMethod -Uri https://get.scoop.sh | Invoke-Expression
scoop install git
# Add repo for kefw2
scoop bucket add hilli https://github.com/hilli/scoop-bucket.git
# Install kefw2
scoop install hilli/go-kef-w2
```

Note that speaker discovery might not work in Windows. 

### Usage

Setup the speakers

```shell
# Auto discovery
kefw2 config speaker discover --save
# Manually add a speaker
kefw2 config speaker add 10.0.0.149
```

If you only have one set of speakers, then that will be the default, otherwise configure that with

```shell
kefw2 config speaker default <name or IP>
```

If you want to control a speaker that is not the default use the `-s` global flag. Eksample:

```shell
kefw2 -s 10.0.0.93 status
```

Get status of the default speaker

```shell
kefw2 status
```

Get detailed info

```shell
kefw2 info
```

Get volume

```shell
kefw2 volume
# or
kefw2 vol
```

Set volume, 35%

```shell
kefw2 vol 35
```

Skip to next track if in wifi mode

```shell
kefw2 next
```

Select source

```shell
kefw2 source wifi
# or just display current source
kefw2 source
```

Play and pause in wifi mode

```shell
kefw2 play
kefw2 pause
```

Turn the speakers off

```shell
kefw2 off
```

Backup the current EQ Profile

```shell
kefw2 config eq_profile > my_profile.json
```

Set the max volume limit

```shell
kefw2 config maxvol 65
```

All with tab completion available of the options, where applicable.

### Plan

- [x] Set volume
- [x] Mute/unmute
- [x] Select source
- [x] Get status
- [x] Turn on/off
- [x] Track next/previous
- [x] Discover speakers automatically
- [x] Display cover art in ASCII (wifi media)
- [x] Backup speaker settings/eq profiles to file
- [ ] Restore speaker settings/eq profiles from file
- [ ] Play Podcasts/Radio
- [ ] Play titles from built-in music streaming services (Amazon Music, Deezer, Qobus, Tidal)

## Library

### Usage

```go
package main

import (
  "fmt"
  "log"

  "github.com/hilli/go-kef-w2/kefw2"
)

func main() {
  speaker, err := kefw2.NewSpeaker("10.0.0.93")
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println(speaker.Name)
  fmt.Println(speaker.Model)
  fmt.Println(speaker.MacAddress)
  fmt.Println(speaker.IPAddress)
  fmt.Println(speaker.Version)
  fmt.Println(speaker.SerialNumber)
  fmt.Println(speaker.MacAddress)
}
```

## Player

UI for controlling the speakers, show whats playing etc.

The idea is to create a [Fyne](https://fyne.io/) App that will let you select inputs, show whats playing etc.
My own needs is to have a Raspberry Pi with a touch screen interact with the speakers and not least control the brigtness of the screen.

### Plan

- [ ] Cross compilation of Fyne apps
- [ ] Input selection buttons
- [ ] Volume/mute controll
- [ ] Play/pause button for available targets
- [ ] Display artwork and track info in wifi mode
- [ ] ?? Streaming page, playing Tidal, Qobus, podcasts, radio

## Web interface & HomeKit HUB

Not there yet.

### Ideas/Plan

- [ ] Turn on/off
- [ ] Set volume
- [ ] Mute/unmute
- [ ] Select source
- [ ] Status page, refreshing, display artwork and track info in wifi mode (web)
- [ ] Settings page, editing (web)
- [ ] Backup/restore settings to/from file up/download (web)
- [ ] ?? Streaming page, playing Tidal, Qobus, podcasts, radio, etc (web)

## License

MIT License
