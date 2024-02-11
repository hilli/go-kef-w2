# go-kef-w2

Library, CLI and Apps for controlling KEFs W2 platform based speakers over the network.

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
  speaker, err := kef.NewSpeaker("10.0.0.93")
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

## Command line tool

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
- [ ] Restore speaker settings/eq profiles to file
- [ ] Play Podcasts/Radio
- [ ] Play titles from built-in music streaming services (Amazon Music, Deezer, Qobus, Spotify, Tidal)

### Usage

Setup the speakers

```shell
kefw2 config speaker discover --save
```

If you only have one set of speakers, then that will be the default, otherwise configure that with

```shell
kefw2 config speaker default <name or IP>
```

If you want to controll a speaker that is not the default use the `-s` global flag

```shell
kefw2 -s 10.0.0.93 status
```

Get status of the default speaker

```shell
kefw2 status
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
$ kefw2 config maxvol 65
$ kefw2 config maxvol
65
```


All with tab completion available of the options.

## Player

UI for controlling the speakers, show whats playing etc.

### Plan

- [ ] Cross compilation of Fyne apps
- [ ] Input selection buttons
- [ ] Volume/mute controll
- [ ] Play/pause button for available targets
- [ ] Display artwork and track info in wifi mode
- [ ] ?? Streaming page, playing Tidal, Qobus, podcasts, radio

## Web interface & HomeKit HUB

Not there yet.

### Plan

- [ ] Turn on/off
- [ ] Set volume
- [ ] Mute/unmute
- [ ] Select source
- [ ] Status page, refreshing, display artwork and track info in wifi mode (web)
- [ ] Settings page, editing (web)
- [ ] Backup/restore settings to file download (web)
- [ ] ?? Streaming page, playing Tidal, Qobus, podcasts, radio, etc (web)

## License

MIT License
