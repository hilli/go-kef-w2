package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause playback when on WiFi source",
	Long:  `Pause playback when on WiFi source`,
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
		if isPlaying, err := currentSpeaker.IsPlaying(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		} else if !isPlaying {
			fmt.Println("Not playing, not pausing")
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
	rootCmd.AddCommand(pauseCmd)
}
