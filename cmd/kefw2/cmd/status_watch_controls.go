// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"fmt"

	"github.com/hilli/go-kef-w2/kefw2"
)

func controlWatch(ctx context.Context, speaker *kefw2.KEFSpeaker, key string) error {
	snapshot, err := readStatus(ctx, speaker)
	if err != nil {
		return fmt.Errorf("checking current state: %w", err)
	}
	if snapshot.Data.Power != kefw2.SpeakerStatusOn || snapshot.Data.Source == kefw2.SourceStandby {
		return fmt.Errorf("controls do not wake a standby speaker")
	}
	switch key {
	case "+", "-", "=":
		maximum, err := speaker.GetMaxVolume(ctx)
		if err != nil {
			return fmt.Errorf("getting maximum volume: %w", err)
		}
		if maximum < 0 || maximum > 100 {
			return fmt.Errorf("speaker returned invalid maximum volume %d", maximum)
		}
		step := 1
		if key == "-" {
			step = -1
		}
		volume := max(0, min(maximum, snapshot.Data.Volume+step))
		if volume == snapshot.Data.Volume {
			return nil
		}
		return speaker.SetVolume(ctx, volume)
	case "m":
		if snapshot.Data.Muted {
			return speaker.Unmute(ctx)
		}
		return speaker.Mute(ctx)
	default:
		return controlWatchPlayback(ctx, speaker, snapshot, key)
	}
}

func controlWatchPlayback(ctx context.Context, speaker *kefw2.KEFSpeaker, snapshot statusSnapshot, key string) error {
	playback := snapshot.Data.Playback
	if playback == nil || (playback.State != kefw2.PlayerStatePlaying && playback.State != kefw2.PlayerStatePaused) {
		return fmt.Errorf("control requires existing WiFi/Bluetooth playback")
	}
	switch key {
	case " ":
		if !snapshot.Player.Controls.Pause {
			return fmt.Errorf("play/pause is unavailable for this source")
		}
		return speaker.PlayPause(ctx)
	case "n":
		if !snapshot.Player.Controls.Next {
			return fmt.Errorf("next track is unavailable")
		}
		return speaker.NextTrack(ctx)
	case "p":
		if !snapshot.Player.Controls.Previous {
			return fmt.Errorf("previous track is unavailable")
		}
		return speaker.PreviousTrack(ctx)
	case watchKeyLeft, "right":
		if playback.State != kefw2.PlayerStatePlaying || playback.PositionMS == nil ||
			playback.DurationMS == nil || snapshot.Player.MediaRoles.MediaData.MetaData.Live {
			return fmt.Errorf("seeking requires playing media with a known position and duration")
		}
		delta := int64(10_000)
		if key == watchKeyLeft {
			delta = -delta
		}
		position := max(int64(0), min(int64(*playback.DurationMS), int64(*playback.PositionMS)+delta))
		return speaker.SeekTo(ctx, position)
	default:
		return fmt.Errorf("unsupported watch action %q", key)
	}
}
