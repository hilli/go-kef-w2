package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// volumeCmd represents the volume command
var maxVolumeCmd = &cobra.Command{
	Use:     "maxvolume",
	Aliases: []string{"maxvol"},
	Short:   "Get or adjust the max volume of the speakers",
	Long:    `Get or adjust the max volume of the speakers`,
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			volume, _ := currentSpeaker.GetMaxVolume()
			fmt.Printf("Volume is: %d%%\n", volume)
			return
		}
		volume, err := parseVolume(args[0])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = currentSpeaker.SetMaxVolume(volume)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
	ValidArgsFunction: VolumeCompletion,
}

func init() {
	rootCmd.AddCommand(maxVolumeCmd)
}
