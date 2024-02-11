package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var eqProfileCmd = &cobra.Command{
	Use:   "eq_profile",
	Short: "Get the equaliser Profile of the speakers",
	Long:  `Get the equaliser Profile of the speakers`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		eqProfile, _ := currentSpeaker.GetEQProfileV2()
		fmt.Println(eqProfile)
	},
}

func init() {
	rootCmd.AddCommand(eqProfileCmd)
}
