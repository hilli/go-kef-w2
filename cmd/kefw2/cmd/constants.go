/*
Copyright © 2025 Jens Hilligsøe <jens@hilli.dk>
Licensed under the MIT License
*/
package cmd

// Content type constants used across the codebase.
const (
	TypeAudio          = "audio"
	TypeContainer      = "container"
	TypeAudioBroadcast = "audioBroadcast"
	TypeQuery          = "query"
	TypeEpisodes       = "episodes"
)

// Key binding constants for TUI navigation.
const (
	KeyCtrlC = "ctrl+c"
	KeyEnter = "enter"
	KeyEsc   = "esc"
)

// Mode constants for browser states.
const (
	ModeFavorites = "favorites"
	ModePopular   = "popular"
	ModeTrending  = "trending"
)

// Playback mode constants.
const (
	PlayModeOff = "off"
)

// Title suffix constants for action modes.
const (
	SuffixRemoveMode = " (remove mode)"
	SuffixSaveMode   = " (save mode)"
	SuffixQueueMode  = " (queue mode)"
)
