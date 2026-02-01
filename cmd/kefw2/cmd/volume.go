/*
Copyright © 2023-2026 Jens Hilligsøe

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

// volumeCmd represents the volume command.
var volumeCmd = &cobra.Command{
	Use:     "volume",
	Aliases: []string{"vol"},
	Short:   "Get or adjust the volume of the speakers",
	Long:    `Get or adjust the volume of the speakers`,
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		if len(args) != 1 {
			volume, _ := currentSpeaker.GetVolume(ctx)
			headerPrinter.Printf("Volume is: ")
			contentPrinter.Printf("%d%%\n", volume)
			return
		}
		volume, err := parseVolume(args[0])
		if err != nil {
			errorPrinter.Println(err)
			os.Exit(1)
		}
		err = currentSpeaker.SetVolume(ctx, volume)
		if err != nil {
			errorPrinter.Println(err)
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

func VolumeCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"0", "10", "20", "30", "40", "50", "60", "70", "80", "90", "100"}, cobra.ShellCompDirectiveNoFileComp
}
