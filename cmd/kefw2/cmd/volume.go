package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// volumeCmd represents the volume command
var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Get or adjust the volume of the speakers",
	Long:  `Get or adjust the volume of the speakers`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			volume, _ := currentSpeaker.GetVolume()
			fmt.Printf("Volume is: %d%%\n", volume)
			return
		}
		volume, err := parseVolume(args[0])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = currentSpeaker.SetVolume(volume)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
	ValidArgsFunction: VolumeCompletion,
}

func init() {
	rootCmd.AddCommand(volumeCmd)
}

func parseVolume(volume string) (int, error) {
	var v int
	_, err := fmt.Sscanf(volume, "%d", &v)
	if err != nil {
		return 0, err
	}
	if v < 0 || v > 100 {
		return 0, fmt.Errorf("volume must be between 0%% and 100%%")
	}
	return v, nil
}

func VolumeCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"0", "10", "20", "30", "40", "50", "60", "70", "80", "90", "100"}, cobra.ShellCompDirectiveNoFileComp
}
