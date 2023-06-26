# go-kef-w2

Library, CLI and Apps for controlling KEFs W2 platform based speakers over the network.

## Usage

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

## License

MIT License

## Command line tool

### Plan

- [x] Set volume
- [x] Mute/unmute
- [x] Select source
- [x] Get status
- [x] Turn on/off
- [ ] Backup/restore settings to file
- [ ] Get settings
- [ ] Set settings

### Usage

```bash
```

## Web interface & HomeKit HUB

### Plan

- [ ] Turn on/off
- [ ] Set volume
- [ ] Mute/unmute
- [ ] Select source
- [ ] Status page, refreshing (web)
- [ ] Settings page, editing (web)
- [ ] Backup/restore settings to file download (web)
- [ ] ?? Streaming page, playing (web)

## Player

UI for controlling the speakers, show whats playing etc.

### Plan

- [x] Cross compilation of Fyne apps
- [ ] Input selection buttons
- [ ] Volume/mute controll
- [ ] Play/pause button for available targets