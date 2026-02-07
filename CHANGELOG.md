# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.6] - 2026-02-06

### Added

- **Stop command**: New `kefw2 stop` command to stop playback entirely
  - Unlike pause, stop ends the current stream completely
  - Useful for radio and live streams where pause is not meaningful
- **Library: Stop method**: New `Stop(ctx)` method for programmatic stream termination
- **Browse container configuration**: Set a default starting folder for UPnP browsing
  - `config upnp container browse <path>` - Set the starting container for browsing
  - Skips parent containers and other servers for a cleaner navigation experience
- **Browse cache**: New browse cache system for faster navigation and tab completion
  - Caches container listings with configurable TTLs per service type
  - Automatic cache persistence and cleanup
- **Cache search**: Search cached entries for quick offline lookups
- **Library: Track indexing moved to kefw2 package**: Track index functionality is now part of the library
  - `kefw2.LoadTrackIndex()`, `kefw2.SaveTrackIndex()`, `kefw2.BuildTrackIndex()`
  - `kefw2.SearchTracks()`, `kefw2.TrackIndexPath()`, `kefw2.IsTrackIndexFresh()`
  - `kefw2.FindContainerByPath()`, `kefw2.ListContainersAtPath()`

### Changed

- **Reorganized UPnP container config**: Moved from `config upnp index container` to `config upnp container index`
  - `config upnp container browse` - Configure starting folder for browsing
  - `config upnp container index` - Configure folder to index for search
- **Improved queue playback**: Fixed queue track playback to properly handle metadata and resources
- **Enhanced UPnP track metadata handling**: Preserve original serviceID and support Airable-specific metadata fields

### Fixed

- **Speaker discovery goroutine leak**: Fixed dnssd goroutine leak by using context.WithCancel instead of context.WithTimeout
- **Player event duration fallback**: Duration now correctly falls back to MediaData.Resources or ActiveResource when Status.Duration is zero
- **Podcast playback**: Improved handling of Airable podcast authentication by playing through parent containers
- **Queue index playback**: Queue items now fetch track details when not provided

## [0.2.5] - 2026-02-05

### Added

- **Seek Command**: New `kefw2 seek <position>` command for jumping to a specific position in the current track
  - Supports multiple time formats: `hh:mm:ss`, `mm:ss`, or seconds
  - Examples: `seek 1:30` (1 min 30 sec), `seek 1:23:45` (1 hr 23 min 45 sec), `seek 90` (90 seconds)
- **Library: SeekTo method**: New `SeekTo(ctx, positionMS)` method for programmatic seeking within tracks

## [0.2.4] - 2026-02-04

### Fixed

- Improve station selection logic for radio playback
- `radio browse` with no arguments now shows the interactive picker instead of attempting to auto

## [0.2.3] - 2025-02-04

### Fixed

- Fixed playback from `upnp search` results - tracks now play and add to queue correctly
  - The search index was missing audio file URIs required for playback
  - Index version bumped to v2; run `kefw2 upnp index --rebuild` after updating

## [0.2.2] - 2025-02-03

### Added

- **UPnP Library Search**: New local search index for instant track searching
  - `upnp search [query]` - Search by title, artist, or album
  - `upnp search` (no query) - Browse full library with interactive filter
  - `upnp index` - View index status
  - `upnp index --rebuild` - Rebuild the search index
  - `upnp index --container "path"` - Index from specific folder
  - `config upnp index container` - Set default container for indexing
  - Ranked search results with exact matches first
  - Multi-word queries work across fields (e.g., `public enemy uzi`)

- **Content Picker Improvements**
  - Page Up/Page Down navigation for faster scrolling
  - Filter now matches on artist/album in addition to title

### Changed

- `upnp play` now recursively scans sub-containers (plays all albums under an artist)
- `upnp search` accepts multiple arguments without quotes

### Fixed

- Fixed `cache status` to show correct cache file and entry count

## [0.2.1] - 2026-02-02

### Added

- Full UPnP pagination support with `BrowseContainerAll()` for fetching all items in large directories
- `AddContainerToQueue` callback for recursively adding all tracks from containers
- `GetContainerTracksRecursive()` for finding all tracks in nested folders
- `BrowseUPnPByDisplayPathAll()` for full pagination on display paths
- Enhanced keyboard shortcuts for queue management

### Changed

- Interactive content picker now uses `BrowseContainerAll()` to fetch all items
- Improved recursive track addition from containers
- More accurate item filtering and selection in content picker
- Consistent use of full container browsing across UPnP and podcast features

### Fixed

- Fixed podcast play/search handling
- Fixed queue picker's delete and clear commands
- Fixed UPnP `IsPlayable` to treat all containers as navigable (not playable)

### Removed

- Removed `queue add` command (use interactive content picker instead)

## [0.2.0] - 2026-02-01

### Added

#### Internet Radio Support

New `kefw2 radio` command for streaming internet radio via KEF's Airable integration:

- `radio play <station>` - Play a radio station with full tab completion
- `radio favorites` - List and play favorite stations
- `radio popular`, `local`, `trending`, `hq`, `new` - Browse radio categories
- `radio search <query>` - Search for stations
- `radio browse` - Interactive TUI browser

#### Podcast Support

New `kefw2 podcast` command for podcast playback:

- `podcast play <show/episode>` - Play episodes with hierarchical tab completion (`Show Name/Episode`)
- `podcast favorites` - List and play favorite podcasts
- `podcast popular`, `trending`, `history` - Browse podcast categories
- `podcast search <query>` - Search for podcasts
- `podcast browse` - Interactive TUI browser

#### UPnP/DLNA Media Server Support

New `kefw2 upnp` command for playing from local network media servers:

- `upnp browse [path]` - Browse server contents with tab completion
- `upnp play <path>` - Play media files
- `config upnp server <name>` - Set default UPnP server

#### Queue Management

New `kefw2 queue` command for managing playback queue:

- `queue list` - Show current queue contents
- `queue add <item>` - Add items to queue
- `queue clear` - Clear the queue
- `queue save <name>` - Save queue as preset
- `queue load <name>` - Load saved queue preset
- `queue mode <mode>` - Set repeat/shuffle mode

#### Cache System

New caching system for faster tab completion with configurable TTL:

- `config cache` - Show all cache settings
- `config cache enable/disable` - Toggle caching
- `config cache ttl-default` - TTL for new/unknown services (default: 300s)
- `config cache ttl-radio` - TTL for radio cache (default: 300s)
- `config cache ttl-podcast` - TTL for podcast cache (default: 300s)
- `config cache ttl-upnp` - TTL for UPnP cache (default: 60s)
- `cache status` - View cache statistics
- `cache clear` - Clear cached data

#### Library: Airable Client

New `AirableClient` for programmatic access to streaming services:

```go
client := kefw2.NewAirableClient(speaker)

// Radio
favorites, _ := client.GetRadioFavorites(ctx)
client.PlayRadioStation(ctx, stationPath)

// Podcasts
episodes, _ := client.GetPodcastEpisodes(ctx, podcastPath)
client.PlayPodcastEpisode(ctx, episodePath)

// UPnP
servers, _ := client.GetMediaServers(ctx)
client.BrowseContainer(ctx, serverPath)
```

#### Tab Completion Enhancements

- Hierarchical path completion for radio, podcasts, and UPnP (`Parent/Child/Item`)
- Colon escaping for zsh compatibility (`:` → `%3A`)
- Real-time current value display in completions
- Pagination support for large result sets (`*All()` methods)

### Changed

- `config cache` command redesigned: shows all settings when called without arguments
- Moved `cache config` to `config cache` for consistency
- Improved error messages for missing speaker configuration

### Fixed

- Tab completion now uses `*All()` methods consistently to avoid truncated results
- Fixed colon characters breaking tab completion in zsh

## [0.1.0] - 2026-01-30

### Breaking Changes

#### Library API Changes

- **Context-first API**: All speaker methods now require `context.Context` as their first parameter. The `*Context` variant methods have been removed.
  
  ```go
  // Old
  volume, err := speaker.GetVolume()
  err = speaker.SetVolume(50)
  err = speaker.Mute()
  source, err := speaker.Source()
  
  // New
  ctx := context.Background()
  volume, err := speaker.GetVolume(ctx)
  err = speaker.SetVolume(ctx, 50)
  err = speaker.Mute(ctx)
  source, err := speaker.Source(ctx)
  ```
  
  Methods updated:
  - `GetVolume(ctx)`, `SetVolume(ctx, volume)`, `Mute(ctx)`, `Unmute(ctx)`, `IsMuted(ctx)`
  - `Source(ctx)`, `SetSource(ctx, source)`, `PowerOff(ctx)`, `SpeakerState(ctx)`, `IsPoweredOn(ctx)`
  - `PlayPause(ctx)`, `NextTrack(ctx)`, `PreviousTrack(ctx)`, `IsPlaying(ctx)`
  - `GetMaxVolume(ctx)`, `SetMaxVolume(ctx, volume)`, `SongProgress(ctx)`, `SongProgressMS(ctx)`
  - `CanControlPlayback(ctx)`, `NetworkOperationMode(ctx)`, `UpdateInfo(ctx)`
  - `PlayerData(ctx)`, `GetEQProfileV2(ctx)`

- **Removed `*Context` variant methods**: Methods like `GetVolumeContext`, `SetVolumeContext`, `MuteContext`, etc. have been removed. Use the standard methods with context instead.

- **`NewSpeaker` now returns a pointer**: `NewSpeaker()` returns `(*KEFSpeaker, error)` instead of `(KEFSpeaker, error)`.
  
  ```go
  // Old
  speaker, err := kefw2.NewSpeaker("192.168.1.100")
  
  // New
  speaker, err := kefw2.NewSpeaker("192.168.1.100")  // Returns *KEFSpeaker
  ```

- **`DiscoverSpeakers` signature changed**: Now accepts `context.Context` and `time.Duration` instead of an `int` for timeout seconds.
  
  ```go
  // Old
  speakers, err := kefw2.DiscoverSpeakers(5)  // 5 seconds
  
  // New
  ctx := context.Background()
  speakers, err := kefw2.DiscoverSpeakers(ctx, 5*time.Second)
  ```
  
  A legacy wrapper `DiscoverSpeakersLegacy(int)` is available for backward compatibility but is deprecated.

- **Renamed `KEFSpeaker.Id` to `KEFSpeaker.ID`**: Following Go naming conventions.

- **Renamed `KEFGroupingmember` to `KEFGroupingMember`**: Fixed casing to follow Go naming conventions.

- **Renamed `EQProfileV2.ProfileId` to `EQProfileV2.ProfileID`**: Following Go naming conventions for acronyms. JSON serialization unchanged (`profileId`).

- **Renamed `PlayerPlayID.SystemMemberId` to `PlayerPlayID.SystemMemberID`**: Following Go naming conventions for acronyms. JSON serialization unchanged (`systemMemberId`).

### Added

#### Functional Options Pattern

`NewSpeaker` now supports functional options for configuration:

```go
speaker, err := kefw2.NewSpeaker("192.168.1.100",
    kefw2.WithTimeout(5*time.Second),
    kefw2.WithHTTPClient(customClient),
)
```

#### Context Support

All speaker methods now accept `context.Context` for better control over request cancellation and timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

volume, err := speaker.GetVolume(ctx)
if err != nil {
    // Handle timeout or cancellation
}
```

#### Custom Error Types

New sentinel errors for better error handling:

- `ErrConnectionRefused` - Speaker not responding (powered off or unreachable)
- `ErrConnectionTimeout` - Request timed out
- `ErrHostNotFound` - Invalid IP address or hostname
- `ErrEmptyData` - Empty response from speaker
- `ErrNoValue` - No value in API response
- `ErrUnknownType` - Unknown value type in response
- `ErrInvalidFormat` - Malformed JSON response

#### TypeEncoder Interface

New `TypeEncoder` interface for KEF-specific types, allowing custom types to be used with `setTypedValue`:

```go
type TypeEncoder interface {
    KEFTypeInfo() (typeName string, value string)
}
```

Implemented by: `Source`, `SpeakerStatus`, `CableMode`

#### Comprehensive Documentation

- Added package-level documentation with usage examples
- Added godoc comments to all exported types, constants, and functions
- Documented all struct fields

#### Unit Tests

- Added 160+ unit tests covering:
  - JSON parsing functions
  - HTTP client operations
  - Speaker operations (volume, mute, source, power, playback)
  - Type encoding and string conversion
  - Event types and parsing
  - Context cancellation and timeout handling
  - Error handling edge cases

### Changed

#### Internal Refactoring

- **Unified HTTP client**: Single shared `*http.Client` per speaker instance
- **Centralized request handling**: All HTTP requests go through `doRequest()` method
- **Replaced `logrus` with `slog`**: Using standard library structured logging in the library (CLI still uses logrus)
- **Improved JSON parsing**: New internal functions with safe type assertions:
  - `parseJSONString()`
  - `parseJSONInt()`
  - `parseJSONBool()`
  - `parseJSONValue()`
- **Better error wrapping**: Using `%w` verb consistently for error chains
- **Channel-based discovery**: `DiscoverSpeakers` uses proper channel synchronization instead of `time.Sleep`

### Deprecated

- `JSONStringValue(data []byte, err error)` - Use `parseJSONString()` (internal) or handle errors separately
- `JSONIntValue(data []byte, err error)` - Use `parseJSONInt()` (internal)
- `JSONUnmarshalValue(data []byte, err error)` - Use `parseJSONValue()` (internal)
- `DiscoverSpeakersLegacy(timeout int)` - Use `DiscoverSpeakers(ctx, duration)`

### Fixed

- Fixed go vet warning in `cmd/kef-virtual-hub/kef-virtual-hub.go` (buffered signal channel)
- Fixed inconsistent pointer/value receiver usage on methods

## Migration Guide

### From Previous Version

1. **Add context to all speaker method calls**: All speaker methods now require `context.Context` as the first parameter.
   ```go
   // Before
   volume, err := speaker.GetVolume()
   err = speaker.SetVolume(50)
   err = speaker.Mute()
   
   // After
   ctx := context.Background()
   volume, err := speaker.GetVolume(ctx)
   err = speaker.SetVolume(ctx, 50)
   err = speaker.Mute(ctx)
   ```

2. **Remove `*Context` method calls**: If you were using methods like `GetVolumeContext`, rename them to the standard method name.
   ```go
   // Before
   volume, err := speaker.GetVolumeContext(ctx)
   
   // After
   volume, err := speaker.GetVolume(ctx)
   ```

3. **Update `NewSpeaker` calls**: The return type is now a pointer, but if you were already using the result directly, no changes are needed since methods work on pointer receivers.

4. **Update `DiscoverSpeakers` calls**:
   ```go
   // Before
   speakers, err := kefw2.DiscoverSpeakers(5)
   
   // After
   speakers, err := kefw2.DiscoverSpeakers(context.Background(), 5*time.Second)
   ```

5. **Update field access**: If you access `speaker.Id`, change it to `speaker.ID`.

6. **Update EQ profile field access**: If you access `eqProfile.ProfileId`, change it to `eqProfile.ProfileID`.

7. **Update player ID field access**: If you access `playId.SystemMemberId`, change it to `playId.SystemMemberID`.

[Unreleased]: https://github.com/hilli/go-kef-w2/compare/v0.2.6...HEAD
[0.2.6]: https://github.com/hilli/go-kef-w2/compare/v0.2.5...v0.2.6
[0.2.5]: https://github.com/hilli/go-kef-w2/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/hilli/go-kef-w2/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/hilli/go-kef-w2/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/hilli/go-kef-w2/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/hilli/go-kef-w2/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/hilli/go-kef-w2/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/hilli/go-kef-w2/releases/tag/v0.1.0
