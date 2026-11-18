// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
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
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	ImageURL string `json:"image_url"`
}

func runStatusJSON(cmd *cobra.Command) error {
	snapshot, err := readStatus(cmd.Context(), currentSpeaker)
	if err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, snapshot.Data)
}

type statusSnapshot struct {
	Data   statusResponse
	Player kefw2.PlayerData
}

func readStatus(ctx context.Context, speaker *kefw2.KEFSpeaker) (statusSnapshot, error) {
	var snapshot statusSnapshot
	source, err := speaker.Source(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("getting source: %w", err)
	}
	power, err := speaker.SpeakerState(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("getting power state: %w", err)
	}
	volume, err := speaker.GetVolume(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("getting volume: %w", err)
	}
	muted, err := speaker.IsMuted(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("getting mute state: %w", err)
	}
	result := statusResponse{Source: source, Power: power, Volume: volume, Muted: muted}
	if source == kefw2.SourceWiFi || source == kefw2.SourceBluetooth {
		pd, err := speaker.PlayerData(ctx)
		if err != nil {
			return snapshot, fmt.Errorf("getting playback: %w", err)
		}
		if pd.State == "" {
			return snapshot, fmt.Errorf("speaker returned an empty playback state")
		}
		snapshot.Player = pd
		result.Playback = &playbackStatus{State: pd.State}
		if pd.State == kefw2.PlayerStatePlaying || pd.State == kefw2.PlayerStatePaused {
			position, err := speaker.SongProgressMS(ctx)
			if err != nil {
				return snapshot, fmt.Errorf("getting playback position: %w", err)
			}
			if position >= 0 {
				result.Playback.PositionMS = &position
			}
			duration := pd.Status.Duration
			if duration <= 0 && len(pd.TrackRoles.MediaData.Resources) > 0 {
				duration = pd.TrackRoles.MediaData.Resources[0].Duration
			}
			if duration > 0 {
				result.Playback.DurationMS = &duration
			}
			result.Playback.Track = &trackStatus{
				Title: pd.TrackRoles.Title, Artist: pd.TrackRoles.MediaData.MetaData.Artist,
				Album: pd.TrackRoles.MediaData.MetaData.Album, ImageURL: pd.TrackRoles.Icon,
			}
		}
	}
	snapshot.Data = result
	return snapshot, nil
}
