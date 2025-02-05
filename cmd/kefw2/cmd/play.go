package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var playCmd = &cobra.Command{
	Use:   "play",
	Short: "Resume playback when on WiFi/BT source if paused",
	Long:  `Resume playback when on WiFi/BT source if paused`,
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		canControlPlayback, err := currentSpeaker.CanControlPlayback()
		if err != nil {
			fmt.Printf("Can't query source: %s\n", err.Error())
			os.Exit(1)
		}
		if !canControlPlayback {
			fmt.Println("Not on WiFi/BT source.")
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
