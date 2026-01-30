# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/hilli/go-kef-w2/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/hilli/go-kef-w2/releases/tag/v0.1.0
