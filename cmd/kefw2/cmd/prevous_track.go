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
	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers.
var previousTrackCmd = &cobra.Command{
	Use:     "previous",
	Aliases: []string{"prev"},
	Short:   "Play previous track when on WiFi source",
	Long:    `Play previous track when on WiFi source`,
	Args:    cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()
		canControlPlayback, err := currentSpeaker.CanControlPlayback(ctx)
		exitOnError(err, "Can't skip back")
		if !canControlPlayback {
			headerPrinter.Println("Not on WiFi/BT source.")
			return
		}
		err = currentSpeaker.PreviousTrack(ctx)
		exitOnError(err, "Can't skip to previous track")
	},
}

func init() {
	rootCmd.AddCommand(previousTrackCmd)
}
