/*
Copyright © 2023-2026 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// seekCmd allows seeking to a specific position in the current track.
var seekCmd = &cobra.Command{
	Use:   "seek <position>",
	Short: "Seek to a position in the current track or podcast",
	Long: `Seek to a specific position in the currently playing track or podcast.

Position can be specified in the following formats:
  hh:mm:ss  - Hours, minutes, and seconds (e.g., 1:23:45)
  mm:ss     - Minutes and seconds (e.g., 5:30)
  seconds   - Just seconds (e.g., 90)

Examples:
  kefw2 seek 1:23:45   # Seek to 1 hour, 23 minutes, 45 seconds
  kefw2 seek 5:30      # Seek to 5 minutes, 30 seconds
  kefw2 seek 90        # Seek to 90 seconds (1:30)`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()

		// Check if we can control playback
		canControlPlayback, err := currentSpeaker.CanControlPlayback(ctx)
		exitOnError(err, "Can't query source")
		if !canControlPlayback {
			exitWithError("Can only seek on WiFi/Bluetooth source.")
		}

		// Check if something is playing
		isPlaying, err := currentSpeaker.IsPlaying(ctx)
		exitOnError(err, "Can't check playback state")
		if !isPlaying {
			exitWithError("Nothing is currently playing.")
		}

		// Parse the position argument
		positionMS, err := parseTimePosition(args[0])
		exitOnError(err, "Invalid time format")

		// Get track duration for validation
		pd, err := currentSpeaker.PlayerData(ctx)
		exitOnError(err, "Can't get player data")

		// Validate position against duration (skip for live streams where duration is 0)
		if pd.Status.Duration > 0 && int(positionMS) > pd.Status.Duration {
			exitWithError("Cannot seek to %s - track is only %s long.",
				formatDuration(int(positionMS)),
				formatDuration(pd.Status.Duration))
		}

		// Perform the seek
		err = currentSpeaker.SeekTo(ctx, positionMS)
		exitOnError(err, "Failed to seek")

		taskConpletedPrinter.Printf("Seeked to %s\n", formatDuration(int(positionMS)))
	},
}

func init() {
	rootCmd.AddCommand(seekCmd)
}

// parseTimePosition parses a time string in formats hh:mm:ss, mm:ss, or seconds
// and returns the position in milliseconds.
func parseTimePosition(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty time value")
	}

	parts := strings.Split(s, ":")

	switch len(parts) {
	case 1:
		// Just seconds
		sec, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid time: %s", s)
		}
		if sec < 0 {
			return 0, fmt.Errorf("time cannot be negative")
		}
		return sec * 1000, nil

	case 2:
		// mm:ss
		mins, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid minutes: %s", parts[0])
		}
		sec, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid seconds: %s", parts[1])
		}
		if mins < 0 {
			return 0, fmt.Errorf("minutes cannot be negative")
		}
		if sec < 0 || sec >= 60 {
			return 0, fmt.Errorf("seconds must be 0-59")
		}
		return (mins*60 + sec) * 1000, nil

	case 3:
		// hh:mm:ss
		hours, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid hours: %s", parts[0])
		}
		mins, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid minutes: %s", parts[1])
		}
		sec, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid seconds: %s", parts[2])
		}
		if hours < 0 {
			return 0, fmt.Errorf("hours cannot be negative")
		}
		if mins < 0 || mins >= 60 {
			return 0, fmt.Errorf("minutes must be 0-59")
		}
		if sec < 0 || sec >= 60 {
			return 0, fmt.Errorf("seconds must be 0-59")
		}
		return (hours*3600 + mins*60 + sec) * 1000, nil

	default:
		return 0, fmt.Errorf("invalid time format: %s (use hh:mm:ss, mm:ss, or seconds)", s)
	}
}

// formatDuration formats milliseconds to a human-readable time string.
// Returns h:mm:ss for durations >= 1 hour, otherwise mm:ss.
func formatDuration(ms int) string {
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
