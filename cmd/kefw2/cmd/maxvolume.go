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
			headerPrinter.Print("Max volume: ")
			contentPrinter.Printf("%d%%\n", volume)
			return
		}
		volume, err := parseVolume(args[0])
		if err != nil {
			errorPrinter.Println(err)
			os.Exit(1)
		}
		err = currentSpeaker.SetMaxVolume(volume)
		if err != nil {
			errorPrinter.Println(err)
			os.Exit(1)
		}
	},
	ValidArgsFunction: VolumeCompletion,
}

func init() {
	ConfigCmd.AddCommand(maxVolumeCmd)
}
