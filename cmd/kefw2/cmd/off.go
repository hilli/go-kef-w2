package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var offCmd = &cobra.Command{
	Use:   "off",
	Short: "Turns the speakers off",
	Long:  `Turns the speakers off`,
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		err := currentSpeaker.PowerOff()
		if err != nil {
			fmt.Println(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(offCmd)
}
