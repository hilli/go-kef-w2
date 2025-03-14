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

// infoCmd represents the info command
var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Display detailed information about the speaker",
	Long:  `Display detailed information about the speaker including software version, IP address, and other available information.`,
	Args:  cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		// Update speaker info to ensure we have the latest data
		err := currentSpeaker.UpdateInfo()
		if err != nil {
			errorPrinter.Printf("Error updating speaker information: %s\n", err.Error())
			os.Exit(1)
		}

		// Display speaker information
		headerPrinter.Print("Speaker Name: ")
		contentPrinter.Println(currentSpeaker.Name)

		headerPrinter.Print("Model: ")
		contentPrinter.Println(currentSpeaker.Model)

		headerPrinter.Print("Firmware Version: ")
		contentPrinter.Println(currentSpeaker.FirmwareVersion)

		headerPrinter.Print("IP Address: ")
		contentPrinter.Println(currentSpeaker.IPAddress)

		headerPrinter.Print("MAC Address: ")
		contentPrinter.Println(currentSpeaker.MacAddress)

		headerPrinter.Print("Maximum Volume: ")
		contentPrinter.Printf("%d\n", currentSpeaker.MaxVolume)

		// Get network operation mode
		networkMode, err := currentSpeaker.NetworkOperationMode()
		if err == nil {
			headerPrinter.Print("Network Mode: ")
			contentPrinter.Println(networkMode)
		}

		// Get speaker power state
		speakerState, err := currentSpeaker.SpeakerState()
		if err == nil {
			headerPrinter.Print("Speaker State: ")
			contentPrinter.Println(speakerState)
		}

		// Get current source
		source, err := currentSpeaker.Source()
		if err == nil {
			headerPrinter.Print("Current Source: ")
			contentPrinter.Println(source)
		}

		// Get mute status
		muted, err := currentSpeaker.IsMuted()
		if err == nil {
			headerPrinter.Print("Muted: ")
			contentPrinter.Println(muted)
		}

		// Get current volume
		volume, err := currentSpeaker.GetVolume()
		if err == nil {
			headerPrinter.Print("Current Volume: ")
			contentPrinter.Printf("%d\n", volume)
		}
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
