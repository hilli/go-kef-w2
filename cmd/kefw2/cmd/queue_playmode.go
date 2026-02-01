/*
Copyright © 2023-2026 Jens Hilligsoe

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
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// queueShuffleCmd controls shuffle mode
var queueShuffleCmd = &cobra.Command{
	Use:   "shuffle [on|off]",
	Short: "Get or set shuffle mode",
	Long: `Get or set the shuffle mode for queue playback.

Without arguments, shows the current shuffle state.

Examples:
  kefw2 queue shuffle          # Show current state
  kefw2 queue shuffle on       # Enable shuffle
  kefw2 queue shuffle off      # Disable shuffle`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"on", "off"},
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		if len(args) == 0 {
			// Get current shuffle state
			enabled, err := client.IsShuffleEnabled()
			if err != nil {
				errorPrinter.Printf("Failed to get shuffle state: %v\n", err)
				os.Exit(1)
			}

			headerPrinter.Print("Shuffle: ")
			if enabled {
				contentPrinter.Println("on")
			} else {
				contentPrinter.Println("off")
			}
			return
		}

		// Set shuffle state
		arg := strings.ToLower(args[0])
		var enable bool
		switch arg {
		case "on", "true", "1", "yes":
			enable = true
		case "off", "false", "0", "no":
			enable = false
		default:
			errorPrinter.Printf("Invalid value: %s (use 'on' or 'off')\n", args[0])
			os.Exit(1)
		}

		if err := client.SetShuffle(enable); err != nil {
			errorPrinter.Printf("Failed to set shuffle: %v\n", err)
			os.Exit(1)
		}

		if enable {
			taskConpletedPrinter.Println("Shuffle enabled.")
		} else {
			taskConpletedPrinter.Println("Shuffle disabled.")
		}
	},
}

// queueRepeatCmd controls repeat mode
var queueRepeatCmd = &cobra.Command{
	Use:   "repeat [off|one|all]",
	Short: "Get or set repeat mode",
	Long: `Get or set the repeat mode for queue playback.

Without arguments, shows the current repeat mode.

Modes:
  off  - No repeat (play queue once)
  one  - Repeat current track
  all  - Repeat entire queue

Examples:
  kefw2 queue repeat          # Show current mode
  kefw2 queue repeat off      # Disable repeat
  kefw2 queue repeat one      # Repeat current track
  kefw2 queue repeat all      # Repeat entire queue`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"off", "one", "all"},
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		if len(args) == 0 {
			// Get current repeat mode
			mode, err := client.GetRepeatMode()
			if err != nil {
				errorPrinter.Printf("Failed to get repeat mode: %v\n", err)
				os.Exit(1)
			}

			headerPrinter.Print("Repeat: ")
			contentPrinter.Println(mode)
			return
		}

		// Set repeat mode
		mode := strings.ToLower(args[0])
		switch mode {
		case "off", "one", "all":
			// Valid modes
		case "none":
			mode = "off"
		case "track", "single":
			mode = "one"
		case "queue", "playlist":
			mode = "all"
		default:
			errorPrinter.Printf("Invalid mode: %s (use 'off', 'one', or 'all')\n", args[0])
			os.Exit(1)
		}

		if err := client.SetRepeat(mode); err != nil {
			errorPrinter.Printf("Failed to set repeat mode: %v\n", err)
			os.Exit(1)
		}

		taskConpletedPrinter.Printf("Repeat mode set to: %s\n", mode)
	},
}

// queueModeCmd shows the current play mode
var queueModeCmd = &cobra.Command{
	Use:     "mode",
	Aliases: []string{"status"},
	Short:   "Show current play mode (shuffle/repeat)",
	Long:    `Show the current shuffle and repeat settings for queue playback.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		shuffleEnabled, err := client.IsShuffleEnabled()
		if err != nil {
			errorPrinter.Printf("Failed to get play mode: %v\n", err)
			os.Exit(1)
		}
		repeatMode, _ := client.GetRepeatMode()

		headerPrinter.Println("Play Mode:")
		headerPrinter.Print("  Shuffle:   ")
		if shuffleEnabled {
			contentPrinter.Println("on")
		} else {
			contentPrinter.Println("off")
		}
		headerPrinter.Print("  Repeat:    ")
		contentPrinter.Println(repeatMode)
	},
}

func init() {
	queueCmd.AddCommand(queueShuffleCmd)
	queueCmd.AddCommand(queueRepeatCmd)
	queueCmd.AddCommand(queueModeCmd)
}
