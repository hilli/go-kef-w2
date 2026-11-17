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
)

// stopCmd stops playback entirely (useful for radio/live streams where pause is not meaningful).
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop playback when on WiFi/BT source",
	Long:  `Stop playback entirely. Unlike pause, this ends the current stream and is particularly useful for radio and live streams.`,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		canControlPlayback, err := currentSpeaker.CanControlPlayback(ctx)
		if err != nil {
			return fmt.Errorf("checking source: %w", err)
		}
		if !canControlPlayback {
			return fmt.Errorf("can't stop speaker: not on WiFi/BT source")
		}
		err = currentSpeaker.Stop(ctx)
		if err != nil {
			return fmt.Errorf("stopping playback: %w", err)
		}
		if jsonOutput(cmd) {
			return writeSpeakerJSON(cmd, actionResult{"stop", true})
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
	addJSONFlag(stopCmd)
}
