# go-kef-w2

CLI, library and apps (planned) for controlling KEFs W2 platform based speakers over the network.

![kefw2 demo](https://github.com/hilli/go-kef-w2/assets/11922/a79f17bc-9c27-4b79-9f59-7be626265483)

## Command line tool

### Installation

#### General

Grab a version for your OS from the [releases](https://github.com/hilli/go-kef-w2/releases) page.

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

If you want to control a speaker that is not the default use the `-s` global flag. Example:

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

Seek to a specific position in the current track or podcast

```shell
# Seek to 5 minutes and 30 seconds
kefw2 seek 5:30

# Seek to 1 hour, 23 minutes, 45 seconds
kefw2 seek 1:23:45

# Seek to 90 seconds
kefw2 seek 90
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

### Internet Radio

Play internet radio stations via KEF's Airable integration:

```shell
# Browse favorite stations
kefw2 radio favorites

# Play a station (with tab completion)
kefw2 radio play "BBC Radio 1"

# Browse by category
kefw2 radio popular
kefw2 radio local
kefw2 radio trending
kefw2 radio hq
kefw2 radio new

# Search for stations
kefw2 radio search "jazz"

# Interactive browser
kefw2 radio browse
```

### Podcasts

Browse and play podcasts:

```shell
# Browse favorite podcasts
kefw2 podcast favorites

# Play an episode (with tab completion - use "Show/Episode" format)
kefw2 podcast play "The Daily/Latest Episode"

# Browse by category
kefw2 podcast popular
kefw2 podcast trending
kefw2 podcast history

# Search for podcasts
kefw2 podcast search "technology"

# Interactive browser
kefw2 podcast browse
```

### UPnP/DLNA Media Servers

Play music from local network media servers:

```shell
# Configure default UPnP server
kefw2 config upnp server default "Plex Media Server: homesrv"

# Browse a server interactively
kefw2 upnp browse

# Browse a specific path (with tab completion)
kefw2 upnp browse "Music/Albums"

# Play media from server
kefw2 upnp play "Music/Jazz/Album/Track.flac"
```

#### UPnP Search

Search your entire music library instantly with a local index:

```shell
# Search for tracks by title, artist, or album
kefw2 upnp search "beatles"
kefw2 upnp search "abbey road"
kefw2 upnp search "come together beatles"

# Add search result to queue instead of playing
kefw2 upnp search -q "bohemian"
```

The search feature builds a local index of your UPnP music library for instant results.
The index is cached and automatically refreshes after 24 hours.

#### Index Configuration

For large libraries (like Plex), you can configure which folder to index to avoid
scanning duplicate views (By Genre, By Album, etc. contain the same tracks):

```shell
# Set the container path to index (with tab completion)
kefw2 config upnp index container "Music/Hilli's Music/By Folder"

# Show current index status
kefw2 upnp index

# Rebuild the index (uses configured container automatically)
kefw2 upnp index --rebuild

# Override container for a one-time rebuild
kefw2 upnp index --rebuild --container "Music/My Library/By Album"

# Clear the container setting (index entire server)
kefw2 config upnp index container ""
```

The container path uses `/` as separator and supports tab completion at each level.

### Queue Management

Manage the playback queue:

```shell
# Show current queue
kefw2 queue list

# Add items to queue
kefw2 queue add "Radio Station Name"

# Clear the queue
kefw2 queue clear

# Save current queue as a preset
kefw2 queue save "My Playlist"

# Load a saved queue
kefw2 queue load "My Playlist"

# Set playback mode
kefw2 queue mode shuffle
kefw2 queue mode repeat
```

### Favorites

Add and manage favorites:

```shell
# Add current playing item to favorites
kefw2 radio favorites add
kefw2 podcast favorites add

# Add a specific item to favorites
kefw2 radio favorites add "BBC Radio 1"
kefw2 podcast favorites add "The Daily"

# Remove from favorites
kefw2 radio favorites remove "BBC Radio 1"
```

### Cache Configuration

Configure caching for faster tab completion:

```shell
# Show all cache settings
kefw2 config cache

# Show specific setting
kefw2 config cache ttl-radio

# Enable/disable caching
kefw2 config cache enable
kefw2 config cache disable

# Set TTL per service (in seconds)
kefw2 config cache ttl-radio 600      # 10 minutes
kefw2 config cache ttl-podcast 1800   # 30 minutes
kefw2 config cache ttl-upnp 120       # 2 minutes
kefw2 config cache ttl-default 300    # 5 minutes (for future services)

# Clear cache
kefw2 cache clear

# View cache status
kefw2 cache status
```

### Event tracking

Track the event from the KEFs

```shell
kefw2 event

# Or as JSON
kefw2 events --json
```

### Plan

- [x] Set volume
- [x] Mute/unmute
- [x] Select source
- [x] Get status
- [x] Turn on/off
- [x] Track next/previous
- [x] Seek within tracks
- [x] Discover speakers automatically
- [x] Display cover art in ASCII (wifi media)
- [x] Backup speaker settings/eq profiles to file
- [x] Play Internet Radio
- [x] Play Podcasts
- [x] Play from UPnP/DLNA media servers
- [x] Queue management
- [x] UPnP library search with local indexing
- [ ] Restore speaker settings/eq profiles from file
- [ ] Play titles from built-in music streaming services (Amazon Music, Deezer, Qobuz, Spotify, Tidal)

## Library

### Installation

```shell
go get github.com/hilli/go-kef-w2/kefw2
```

### Basic Usage

```go
package main

import (
  "context"
  "fmt"
  "log"
  "time"

  "github.com/hilli/go-kef-w2/kefw2"
)

func main() {
  // Create a speaker connection
  speaker, err := kefw2.NewSpeaker("10.0.0.93")
  if err != nil {
    log.Fatal(err)
  }

  // Print speaker info
  fmt.Printf("Name: %s\n", speaker.Name)
  fmt.Printf("Model: %s\n", speaker.Model)
  fmt.Printf("Firmware: %s\n", speaker.FirmwareVersion)
  fmt.Printf("MAC: %s\n", speaker.MacAddress)

  ctx := context.Background()

  // Control volume
  volume, _ := speaker.GetVolume(ctx)
  fmt.Printf("Volume: %d\n", volume)
  speaker.SetVolume(ctx, 30)

  // Change source
  speaker.SetSource(ctx, kefw2.SourceWiFi)
}
```

### Configuration Options

```go
// With custom timeout
speaker, err := kefw2.NewSpeaker("10.0.0.93",
  kefw2.WithTimeout(5*time.Second),
)

// With custom HTTP client
customClient := &http.Client{Timeout: 10 * time.Second}
speaker, err := kefw2.NewSpeaker("10.0.0.93",
  kefw2.WithHTTPClient(customClient),
)
```

### Context Support

All operations have context support for cancellation and timeout control:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

volume, err := speaker.GetVolume(ctx)
if err != nil {
  log.Fatal(err)
}
```

### Speaker Discovery

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

speakers, err := kefw2.DiscoverSpeakers(ctx, 5*time.Second)
if err != nil {
  log.Fatal(err)
}

for _, s := range speakers {
  fmt.Printf("Found: %s at %s\n", s.Name, s.IPAddress)
}
```

### Event Streaming

Subscribe to real-time speaker events:

```go
client, err := speaker.NewEventClient()
if err != nil {
  log.Fatal(err)
}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go client.Start(ctx)

for event := range client.Events() {
  switch e := event.(type) {
  case *kefw2.VolumeEvent:
    fmt.Printf("Volume changed: %d\n", e.Volume)
  case *kefw2.SourceEvent:
    fmt.Printf("Source changed: %s\n", e.Source)
  case *kefw2.PlayerDataEvent:
    fmt.Printf("Now playing: %s - %s\n", e.Artist, e.Title)
  }
}
```

### Airable Streaming (Radio, Podcasts, UPnP)

```go
ctx := context.Background()

// Create Airable client for streaming services
client := kefw2.NewAirableClient(speaker)

// Get radio favorites
favorites, err := client.GetRadioFavorites(ctx)
for _, station := range favorites {
  fmt.Printf("Station: %s\n", station.Title)
}

// Play a radio station
err = client.PlayRadioStation(ctx, stationPath)

// Get podcast episodes
episodes, err := client.GetPodcastEpisodes(ctx, podcastPath)

// Browse UPnP servers
servers, err := client.GetMediaServers(ctx)
```

## License

MIT License
