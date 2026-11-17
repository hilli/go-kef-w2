// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

type statusResponse struct {
	Source   kefw2.Source        `json:"source"`
	Power    kefw2.SpeakerStatus `json:"power"`
	Volume   int                 `json:"volume"`
	Muted    bool                `json:"muted"`
	Playback *playbackStatus     `json:"playback"`
}

type playbackStatus struct {
	State      string       `json:"state"`
	PositionMS *int         `json:"position_ms"`
	DurationMS *int         `json:"duration_ms"`
	Track      *trackStatus `json:"track"`
}

type trackStatus struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

func runStatusJSON(cmd *cobra.Command) error {
	ctx := cmd.Context()
	source, err := currentSpeaker.Source(ctx)
	if err != nil {
		return fmt.Errorf("getting source: %w", err)
	}
	power, err := currentSpeaker.SpeakerState(ctx)
	if err != nil {
		return fmt.Errorf("getting power state: %w", err)
	}
	volume, err := currentSpeaker.GetVolume(ctx)
	if err != nil {
		return fmt.Errorf("getting volume: %w", err)
	}
	muted, err := currentSpeaker.IsMuted(ctx)
	if err != nil {
		return fmt.Errorf("getting mute state: %w", err)
	}
	result := statusResponse{Source: source, Power: power, Volume: volume, Muted: muted}
	if source == kefw2.SourceWiFi || source == kefw2.SourceBluetooth {
		pd, err := currentSpeaker.PlayerData(ctx)
		if err != nil {
			return fmt.Errorf("getting playback: %w", err)
		}
		if pd.State == "" {
			return fmt.Errorf("speaker returned an empty playback state")
		}
		result.Playback = &playbackStatus{State: pd.State}
		if pd.State == kefw2.PlayerStatePlaying || pd.State == kefw2.PlayerStatePaused {
			position, err := currentSpeaker.SongProgressMS(ctx)
			if err != nil {
				return fmt.Errorf("getting playback position: %w", err)
			}
			result.Playback.PositionMS = &position
			if pd.Status.Duration > 0 {
				result.Playback.DurationMS = &pd.Status.Duration
			}
			result.Playback.Track = &trackStatus{
				Title: pd.TrackRoles.Title, Artist: pd.TrackRoles.MediaData.MetaData.Artist,
				Album: pd.TrackRoles.MediaData.MetaData.Album,
			}
		}
	}
	return writeSpeakerJSON(cmd, result)
}
