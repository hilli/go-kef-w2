package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var muteCmd = &cobra.Command{
	Use:   "mute",
	Short: "Get or adjust the mute state of the speakers",
	Long:  `Get or adjust the mute state of the speakers`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			mute, _ := currentSpeaker.IsMuted()
			fmt.Printf("Speakers are muted: %t\n", mute)
			return
		}
		mute, err := parseMuteArg(args[0])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if mute {
			err = currentSpeaker.Mute()
		} else {
			err = currentSpeaker.Unmute()
		}
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
	ValidArgsFunction: MuteCompletion,
}

func init() {
	rootCmd.AddCommand(muteCmd)
}

func MuteCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"on", "off", "true", "false", "0", "1", "muted", "unmute", "unmuted"}, cobra.ShellCompDirectiveNoFileComp
}

func parseMuteArg(mute string) (bool, error) {
	switch mute {
	case "on", "true", "1", "muted":
		return true, nil
	case "off", "false", "0", "unmute", "unmuted":
		return false, nil
	default:
		return false, fmt.Errorf("mute must be one of: on, off, true, false, 0, 1, muted, unmute, unmuted")
	}
}
