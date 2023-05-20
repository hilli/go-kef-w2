package cmd

import (
	"fmt"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var playCmd = &cobra.Command{
	Use:   "play",
	Short: "Resume playback when on WiFi source if paused",
	Long:  `Resume playback when on WiFi source if paused`,
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		currentSource, err := currentSpeaker.Source()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if currentSource != kefw2.SourceWiFi {
			fmt.Println("Not on WiFi source, not resuming playback")
			os.Exit(0)
		}
		err = currentSpeaker.PlayPause()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(playCmd)
}
