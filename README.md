# go-kef-w2

CLI, library and apps (planned) for controlling KEFs W2 platform based speakers over the network.

![kefw2 demo](https://github.com/hilli/go-kef-w2/assets/11922/a79f17bc-9c27-4b79-9f59-7be626265483)

## Command line tool

### Installation

#### General

Grab a version for your OS from the [releases](https://github.com/hilli/go-kef-w2/releases) page.

If you are a developer and have [Go](https://go.dev/) 1.27.3 or newer installed, you can install the latest version directly:

```shell
go install github.com/hilli/go-kef-w2/cmd/kefw2@latest
````

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

Stop playback entirely (useful for radio/live streams)

```shell
kefw2 stop
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

Save the current EQ settings to the local profile library, then load them later
with the intended audio source selected. The speaker exposes only its active EQ
profile; saved profiles are local snapshots in the user config directory.

```shell
kefw2 --speaker "Office" config eq_profile save "Night listening"
kefw2 config eq_profile list
kefw2 config eq_profile show "Night listening"
kefw2 --speaker "Office" config eq_profile load "Night listening"
kefw2 config eq_profile export "Night listening" night-listening.json
kefw2 config eq_profile import "Night listening" night-listening.json
kefw2 config eq_profile delete "Night listening"
```

Use `--force` with `save`, `import`, or `export NAME FILE` to replace an
existing file. Imports accept either a profile JSON file produced by the legacy
`config eq_profile` command or a versioned library export. The legacy command
still reads the active profile with no argument and restores a profile from a
JSON file argument.

Set the max volume limit

```shell
kefw2 config maxvol 65
```

All with tab completion available of the options, where applicable.

### AI agents and scripting

Install the bundled agent skill:

```shell
kefw2 skill setup
```

This installs `~/.agents/skills/kefw2/SKILL.md`. The small bootstrap tells agents
to run `kefw2 skill show`, which prints the guide bundled with the installed
binary, so instructions stay aligned with that version. No network or speaker
configuration is needed. Setup is repeatable and refuses to overwrite an
unrecognized or modified skill.
Rerun setup to upgrade an unmodified earlier bootstrap to the current
description, including music/UPnP, podcast, radio, queue, and favorites workflows.

Setup also installs into the following agents' skill directories when their
configuration directories already exist (the `skills` subdirectory may be new):

| Agent | Skills root |
| --- | --- |
| Claude Code | `~/.claude/skills` |
| GitHub Copilot | `~/.copilot/skills` |
| Codex (legacy location) | `~/.codex/skills` |
| Cursor | `~/.cursor/skills` |
| Gemini CLI | `~/.gemini/skills` |
| OpenCode | `$XDG_CONFIG_HOME/opencode/skills`, or `~/.config/opencode/skills` |

Current Codex and other agents supporting the shared directory can use the
`~/.agents/skills` install directly. Setup does not create configuration
directories for absent agents or install into project-local directories.

```shell
kefw2 skill show
kefw2 skill setup --skills-dir /path/to/skills
kefw2 skill setup -v
kefw2 skill remove
```

`--skills-dir` disables automatic detection and uses only that skills root;
the installer appends `kefw2/SKILL.md`. Removal checks the same detected locations,
preserves unrelated files, and refuses to delete a modified skill. Symlink paths
are refused. A conflict at one skills root is reported without preventing
other selected roots from being processed; the command exits nonzero if any fail.
Both setup and removal accept `-v`/`--verbose` to show each selected skill path
and its progress. They remain quiet by default.

Machine-readable output is available on these commands:

| Commands | Behavior with `--json` |
| --- | --- |
| `config speaker list` | Configured speakers and effective default |
| `config speaker discover` | Discovered speakers; only saves with `--save` |
| `status` | Source, power, volume, mute, and applicable playback metadata |
| `status watch` | Event-driven JSON Lines stream of complete status snapshots, including artwork URLs |
| `volume`, `mute`, `source` | Read a value or request an absolute setting |
| `play`, `pause`, `stop`, `off` | Request playback/power actions or report a no-op |
| `radio search QUERY` | Return candidates without opening a picker or playing |
| `radio play --path PATH` | Play an exact path returned by search on the same speaker |
| `radio`, categories, favorites, `browse` | List content; explicit selections can play or change favorites |
| `podcast`, search, categories, filter, browse, episodes | List shows/episodes without playing; explicit playback/queue/favorite actions |
| `upnp`, browse, play, search, index | Browse/play exact paths, queue tracks, and inspect/rebuild/search the local index |
| `queue`, list, play, remove, move, clear | Inspect or explicitly change the queue using 1-based positions |
| `queue mode`, shuffle, repeat | Inspect or set playback modes |
| `queue save`, load, saved, delete-saved | Save/replace queues or manage local saved queues |
| `info`, version, next, previous, seek | Structured metadata and navigation |
| Remaining speaker/UPnP/cache configuration, EQ, maxvolume, reboot/schedule | Structured getters and explicit validated actions |

These finite commands emit one JSON object with a `data` field, plus `speaker`
identity for speaker-specific operations. JSON mode does not prompt, render
artwork, or print progress or ANSI colors. Failures exit with status 1 and send
diagnostics to stderr rather than returning fabricated values. Operational
commands support `--json`; the interactive player, help-only command groups,
shell completion and skill installation/display are not JSON APIs. Existing
human-facing commands remain interactive by default. `status watch --json`
emits complete snapshots, while `events --json` emits individual events; both
are unbounded JSON Lines streams.

For example:

```shell
kefw2 config speaker list --json
kefw2 --speaker "Office" status --json
kefw2 --speaker "Office" volume 25 --json
kefw2 --speaker "Office" volume --json
kefw2 --speaker "Office" radio search "BBC Radio 6" --json
# Copy an exact data.items[].path from the search response:
kefw2 --speaker "Office" radio play --path 'PATH_FROM_SEARCH' --json
```

A volume request returns:

```json
{"speaker":{"name":"Office","ip_address":"192.0.2.10","model":"KEF LS60 Wireless"},"data":{"action":"set_volume","accepted":true,"volume":25}}
```

`accepted: true` means the request was accepted, not that the physical transition
has completed. Read the relevant state afterward; allow time for asynchronous
changes. `accepted: false` indicates a no-op. Status uses percent volume and
millisecond playback times; `playback` is `null` on standby/physical inputs and
unknown or inapplicable playback times are `null`.
Track metadata includes `image_url`, with an empty string when no artwork URL
is available.

### Interactive player

The player controls one speaker at a time in a terminal, with keyboard and
mouse support. Listening Desk is the default layout; Discovery Browser gives
more space to search. Both layouts share the same controls and capabilities.
For scriptable live status, see [Live status](#live-status); the player itself
has no JSON interface.

```shell
kefw2 player
kefw2 player --layout listening-desk
kefw2 --speaker "Living Room" player --layout discovery-browser
```

`--layout` accepts `listening-desk` or `discovery-browser`. Omitting it always
starts Listening Desk, even after using Discovery Browser. Pane arrangements
are saved separately for each layout. Launching the player does not change the
speaker's input or playback.

At startup, `--speaker NAME_OR_IP` takes precedence over `KEFW2_SPEAKER` or
the `speaker` configuration value, then the configured default or sole
configured speaker. If several speakers have no default, the player opens a
chooser. With no configured or explicit target, first use
`kefw2 config speaker discover --save` or `kefw2 config speaker add IP`.
Use an IP to disambiguate duplicate names; an unconfigured explicit target is
for the current session only.

Press uppercase `S` or click the speaker name to choose another configured
speaker; lowercase `s` opens the input chooser. Changing which speaker is
controlled does not change the saved default, transfer playback, or copy
source, volume or queue state. If the selected speaker is unavailable, it
remains selected: retry or choose another rather than silently controlling the
previous speaker. An action already accepted by the previous speaker cannot be
recalled, and failed actions are not automatically replayed. On failed status
reads, the selected speaker's last confirmed state is marked stale. If the
event stream disconnects, writes pause until a fresh status poll succeeds;
unsent actions are discarded, and uncertain dispatched actions identify their
original target.

The player shows the speaker's confirmed power and actual input; the input is
not the service being browsed. Radio and Podcasts are Wi-Fi services; browsing
them does not switch inputs.
The narrow Sources sidebar lists physical inputs, **Off / Standby**, and Wi-Fi
browsing shortcuts. Click an input to select it, then use Enter, Open or a
double-click to activate it. A single click opens a Wi-Fi service. `*` marks
the confirmed input; Off puts only the selected speaker into standby.
Unsupported inputs are dimmed and marked `-`.
Active panes use cyan double borders, selected rows have filled highlights,
and keyboard hints are amber. Saved custom widths remain unchanged; `p`, then
`R` restores the narrower default.

The bottom row has separate **Now Playing** and **Volume** panes. Now Playing
shows the actual input, playback details and transport controls; Volume holds
the slider and mute control. Click a pane title or use Tab to focus its controls.

Search stations or podcasts, open a series to browse episodes, or browse UPnP
and add supported tracks to the active queue. Selecting a result previews it;
playing a station or episode is explicit and can change playback. Radio play
requires confirmation (`y` to confirm) and can switch to Wi-Fi and replace
the queue. TV, Optical, Coaxial, Aux and USB show `external audio` when
playback data is unavailable; transport/seek depend on the input and media. In
the player, UPnP supports browsing and enqueueing tracks, not search,
favorites or direct playback from a result. To play a UPnP track, enqueue it,
select it in the queue and press Enter; enqueueing alone does not start playback.

| Action | Keyboard |
| --- | --- |
| Focus and navigate | `Tab` / `Shift+Tab`; arrows or `j` / `k` |
| Speaker / input | `S` / `s`, then arrows and Enter; Esc cancels |
| Sources sidebar | `Alt+1` by default, or Tab to focus; arrows or `j` / `k` select; Enter opens/switches |
| Browse Wi-Fi | Click Radio, Podcasts or UPnP to open its menu in Discover; select it again to return to the menu |
| Confirmation dialogs | Tab/Shift+Tab or Left/Right selects; Enter activates; `y` confirms, `n`/Esc cancels; Cancel is the default |
| Search / details | `/` to search; Enter submits or opens/plays supported results; `i` opens details |
| Queue | `Q` focuses queue; queue focus: `Ctrl+k` / `Ctrl+j` move row |
| Panes | `p` opens Move, Resize, Zoom and Reset; `z` toggles zoom on the focused pane |
| Playback / volume | Space toggles; `[` / `]` skip; `,` / `.` seek; `+` / `-` adjust volume; `m` toggles mute |
| Errors | `R` retries a failed volume change when offered; `D` dismisses control/volume errors |
| Help / quit | `?`; `q` or Ctrl+C |

Printable shortcuts apply outside text fields; pane and menu keys depend on
focus. In Discover, `1` / `2` / `3` select Radio / Podcasts / UPnP, `<` / `>`
page results, and `e` enqueues a supported UPnP track. `r` retries an offered
failed discovery read, not a playback, favorite or enqueue action.

In Discovery Browser, `Q` also returns from the queue to discovery. With queue
focus, Enter plays, Delete removes, `c` prompts to clear (`y` confirms), `h`
toggles shuffle, and `t` cycles repeat. Use `r` to reload an uncertain queue
and `a` to acknowledge it after reviewing the refreshed result. Moves check
for intervening changes but cannot guarantee atomicity against another controller.

In the `p` pane menu, `m` moves, `r` resizes, `z` zooms, uppercase `R`
resets the active layout, `b` swaps Now Playing and Volume, and `a` toggles
reduced motion (panes snap instead of gliding into place). Use Left/Right, Enter
and Esc to preview, commit or cancel changes. In Discovery Browser, `p` then
`d` docks or undocks the queue in place
of the inspector; the choice is saved with that layout. Reset
preserves the other valid layout when only the active arrangement is invalid
and the file's version and schema are supported. Resetting a malformed,
unsupported or otherwise invalid file may discard other arrangements.

With the mouse, click controls or rows, double-click a navigable container to
open it or a station/episode to play. Drag a pane's `::` title grip over another
visible pane in the same row to reorder; this also works for Now Playing and
Volume. Dropped panes glide into place using Bloom layout animation. Drag upper-pane dividers to resize and queue-row grips to reorder.
Layout changes are saved separately for each layout; Esc, losing terminal
focus or dropping outside the row cancels a pane move. Collapsed single-pane
rows cannot be reordered by dragging. Click or drag the volume slider,
or use Left/Right while it has focus; `+` / `-` adjust by 1. Volume respects the
selected speaker's maximum, and increases are disabled if that limit cannot
be read. Seek moves by 10 seconds and requires supported, currently playing
non-live media with a known position and duration. Review the speaker and
current state before retrying an uncertain action.

### Live status

```shell
kefw2 --speaker "Office" status watch
kefw2 --speaker "Office" status watch --minimal
kefw2 --speaker "Office" status watch --ascii
kefw2 --speaker "Office" status watch --json
```

Watch follows speaker events with no interval flag or healthy-stream polling.
Human mode updates an alternate screen without filling scrollback, retaining
unchanged cover art across text updates. Native artwork supports
Kitty-compatible terminals and iTerm2 outside tmux; other terminals fall back to ASCII.
Use `--ascii` to force ASCII or `--minimal` to hide artwork.

Space toggles playback, `n`/`p` change track, `+`/`-` change volume by 1, `m`
toggles mute, and Left/Right seek 10 seconds. Controls respect source
capabilities, the configured volume maximum and known track duration; they
never wake standby speakers or start a stopped queue. Quit with `q` or Ctrl+C.

Use `--json` for pipes or external UIs: it has no keyboard controls, artwork
downloads or terminal decoration. Each newline-terminated object uses the
same status envelope, including `data.playback.track.image_url`.
Events trigger full snapshots, not partial deltas or periodic heartbeats.
If events disconnect, watch reports the problem and polls internally every
5 seconds while reconnecting. Human mode marks failed status reads stale;
JSON emits diagnostics on stderr and skips failed reads. Polling stops when
event delivery and status recover. Output-write errors terminate watch.

Radio search supports `--offset` (default 0) and `--limit` (default 24, maximum
100). `total_rows` and offsets refer to raw service rows, not just playable
stations. Continue with `next_offset` while `has_more` is true; stop if the offset
does not advance. Paths are speaker-specific and may expire. JSON name-based
radio playback rejects ambiguous or incomplete search results; prefer exact
paths. Radio playback replaces the current queue, so do not blindly retry it
after a failure: the queue may already have been cleared.

On speakers that reject plain-HTTP control writes with HTTP 401, the CLI uses
the existing authenticated TLS channel. This applies to both the picker and
JSON commands; Airable paths remain unchanged.

More content workflows:

```shell
kefw2 --speaker "Office" podcast search "science" --json
kefw2 --speaker "Office" podcast episodes --path 'SHOW_PATH' --json
kefw2 --speaker "Office" podcast play --episode-path 'EPISODE_PATH' --json
kefw2 --speaker "Office" upnp --json
kefw2 --speaker "Office" upnp browse --path 'CONTAINER_PATH' --json
kefw2 --speaker "Office" upnp play --path 'TRACK_PATH' --queue --json
kefw2 upnp search "artist or track" --json
kefw2 --speaker "Office" queue list --json
kefw2 --speaker "Office" queue move 3 before 1 --json
kefw2 queue saved --json
```

Podcast show-only playback requires explicit `--latest`. Podcast `--queue`
appends without starting playback; UPnP `--queue` starts only if the queue was
empty. UPnP search/index inspection and saved-queue listing/deletion are local
and need no configured speaker. Rebuild the UPnP index explicitly with
`upnp index --rebuild --json` on the intended speaker.

Lists use content summaries and, where paginated, expose `next_offset` and
`has_more`. Duplicate-name selections fail instead of choosing the first item.
Queue positions are 1-based snapshots: read the queue again after edits.
Loading a saved queue replaces the queue and starts playback.

Use `kefw2 skill show` for response shapes, exact-path selection, action flags,
pagination, and retry guidance.

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
kefw2 config upnp container index "Music/Hilli's Music/By Folder"

# Set a default starting folder for browsing
kefw2 config upnp container browse "Music/Hilli's Music"

# Show current index status
kefw2 upnp index

# Rebuild the index (uses configured container automatically)
kefw2 upnp index --rebuild

# Override container for a one-time rebuild
kefw2 upnp index --rebuild --container "Music/My Library/By Album"

# Clear the container setting (index entire server)
kefw2 config upnp container index ""
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
- [x] Manage local EQ profile snapshots (save/load/import/export)
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
