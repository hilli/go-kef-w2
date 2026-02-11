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

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// playCmd resumes or starts playback on the speaker.
var playCmd = &cobra.Command{
	Use:   "play",
	Short: "Resume or start playback",
	Long: `Resume or start playback.

If the speaker is in standby, it will be woken up by switching to WiFi
before starting playback. If paused, playback is resumed. If stopped and
the queue has tracks, playback starts from the top of the queue (or a
random track if shuffle is enabled). If the queue is empty, a message is
shown.`,
	Args: cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()

		// Check current source - refuse if on a non-streamable physical input
		// (optical, coaxial, etc.) but allow standby since PlayOrResumeFromQueue
		// will wake the speaker by switching to WiFi.
		source, err := currentSpeaker.Source(ctx)
		exitOnError(err, "Can't query source")
		if source != kefw2.SourceWiFi && source != kefw2.SourceBluetooth && source != kefw2.SourceStandby {
			headerPrinter.Printf("Can only play on WiFi/BT source (current: %s).\n", source)
			return
		}

		client := kefw2.NewAirableClient(currentSpeaker)
		result, err := client.PlayOrResumeFromQueue(ctx)
		exitOnError(err, "Can't play")

		switch result.Action {
		case kefw2.PlayActionStartedFromQueue:
			if result.WokeFromStandby {
				headerPrinter.Print("Woke speaker from standby. ")
			}
			if result.Shuffled {
				headerPrinter.Print("Shuffling queue, playing: ")
			} else {
				headerPrinter.Print("Playing from queue: ")
			}
			contentPrinter.Printf("%s", result.Track.Title)
			if result.Track.MediaData != nil && result.Track.MediaData.MetaData.Artist != "" {
				contentPrinter.Printf(" - %s", result.Track.MediaData.MetaData.Artist)
			}
			fmt.Println()
		case kefw2.PlayActionNothingToPlay:
			headerPrinter.Println("Nothing to play - queue is empty.")
		}
	},
}

func init() {
	rootCmd.AddCommand(playCmd)
}
