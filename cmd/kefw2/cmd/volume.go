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
}

func init() {
	rootCmd.AddCommand(volumeCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// volumeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// volumeCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
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
