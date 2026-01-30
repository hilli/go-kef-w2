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

	"github.com/hilli/go-kef-w2/kefw2"
)

// volumeCmd represents the volume command.
var sourceCmd = &cobra.Command{
	Use:     "source",
	Aliases: []string{"src"},
	Short:   "Get or change the source of the speakers",
	Long:    `Get or change the source of the speakers`,
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		if len(args) != 1 {
			source, _ := currentSpeaker.Source(ctx)
			headerPrinter.Print("Source: ")
			contentPrinter.Printf("%s\n", source.String())
			return
		}
		source, err := parseSource(args[0])
		if err != nil {
			errorPrinter.Println(err)
			os.Exit(1)
		}
		err = currentSpeaker.SetSource(ctx, source)
		if err != nil {
			errorPrinter.Println(err)
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
	case "analog", "aux":
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
		return kefw2.SourceStandby, nil
	default:
		return "", fmt.Errorf("source must be one of: analog, aux, bluetooth, coaxial, optical, tv, usb, wifi, standby")
	}
}

func SourceCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"analog", "aux", "bluetooth", "coaxial", "optical", "tv", "usb", "wifi", "standby"}, cobra.ShellCompDirectiveNoFileComp
}
