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

// stopCmd stops playback entirely (useful for radio/live streams where pause is not meaningful).
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop playback when on WiFi/BT source",
	Long:  `Stop playback entirely. Unlike pause, this ends the current stream and is particularly useful for radio and live streams.`,
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()
		canControlPlayback, err := currentSpeaker.CanControlPlayback(ctx)
		exitOnError(err, "Can't stop speaker")
		if !canControlPlayback {
			headerPrinter.Println("Can't stop speaker: Not on WiFi/BT source.")
			return
		}
		err = currentSpeaker.Stop(ctx)
		exitOnError(err, "Can't stop playback")
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
