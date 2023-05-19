package cmd

import (
	"fmt"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
)

// volumeCmd represents the volume command
var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Get or change the source of the speakers",
	Long:  `Get or change the source of the speakers`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			volume, _ := currentSpeaker.Source()
			fmt.Printf("Source is: %s\n", volume.String())
			return
		}
		source, err := parseSource(args[0])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		err = currentSpeaker.SetSource(kefw2.Source(source))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
	ValidArgsFunction: SourceCompletion,
}

func init() {
	rootCmd.AddCommand(sourceCmd)
}

func parseSource(source string) (kefw2.Source, error) {
	switch source {
	case "analog":
		return kefw2.SourceAux, nil
	case "bluetooth":
		return kefw2.SourceBluetooth, nil
	case "coaxial":
		return kefw2.SourceCoaxial, nil
	case "optical":
		return kefw2.SourceOptical, nil
	case "tv":
		return kefw2.SourceTV, nil
	case "usb":
		return kefw2.SourceUSB, nil
	case "wifi":
		return kefw2.SourceWiFi, nil
	case "standby":
		return kefw2.Source(source), nil
	default:
		return "", fmt.Errorf("source must be one of: analog, bluetooth, coaxial, optical, tv, usb, wifi, standby")
	}
}

func SourceCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"analog", "bluetooth", "coaxial", "optical", "tv", "usb", "wifi", "standby"}, cobra.ShellCompDirectiveNoFileComp
}
