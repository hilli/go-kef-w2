/*
Copyright © 2023-2025 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// muteCmd toggles the mute state of the speakers
var muteCmd = &cobra.Command{
	Use:   "mute on/off",
	Short: "Get or adjust the mute state of the speakers",
	Long:  `Get or adjust the mute state of the speakers`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			mute, _ := currentSpeaker.IsMuted()
			if mute {
				headerPrinter.Print("Speakers are ")
				contentPrinter.Println("muted")
			} else {
				headerPrinter.Print("Speakers are ")
				contentPrinter.Println("not muted")
			}
			return
		}
		mute, err := parseMuteArg(args[0])
		if err != nil {
			errorPrinter.Println(err)
			os.Exit(1)
		}
		if mute {
			err = currentSpeaker.Mute()
		} else {
			err = currentSpeaker.Unmute()
		}
		if err != nil {
			errorPrinter.Println(err)
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
