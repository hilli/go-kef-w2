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
	_ "image/jpeg" // Required for image decoding
	_ "image/png"  // Required for image decoding
	"os"

	"github.com/hilli/icat"
	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// volumeCmd represents the volume command.
var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"state", "st"},
	Short:   "Status of the speakers",
	Long:    `Status of the speakers`,
	Args:    cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()
		source, err := currentSpeaker.Source(ctx)
		if err != nil {
			errorPrinter.Println(err)
			os.Exit(1)
		}
		canControlPlayback, err := currentSpeaker.CanControlPlayback(ctx)
		if err != nil {
			errorPrinter.Printf("Can't show status: %s\n", err.Error())
			os.Exit(1)
		}
		if canControlPlayback {
			pd, err := currentSpeaker.PlayerData(ctx)
			if err != nil {
				errorPrinter.Println(err)
				os.Exit(1)
			}
			if playstate, err := currentSpeaker.IsPlaying(ctx); err != nil {
				errorPrinter.Println("error getting playstate:", err)
			} else {
				if playstate {
					playTime, _ := currentSpeaker.SongProgress(ctx)
					// Minimalistic output
					headerPrinter.Print("Source: ")
					contentPrinter.Println(source)
					if source == kefw2.SourceWiFi {
						headerPrinter.Print("Audio Transport: ")
						contentPrinter.Println(pd.MediaRoles.Title)
						if pd.TrackRoles.MediaData.MetaData.Artist != "" {
							headerPrinter.Print("Artist: ")
							contentPrinter.Println(pd.TrackRoles.MediaData.MetaData.Artist)
						}
						if pd.TrackRoles.MediaData.MetaData.Album != "" {
							headerPrinter.Print("Album: ")
							contentPrinter.Println(pd.TrackRoles.MediaData.MetaData.Album)
						}
						if pd.TrackRoles.Title != "" {
							headerPrinter.Print("Track: ")
							contentPrinter.Println(pd.TrackRoles.Title)
						}
						headerPrinter.Print("Duration: ")
						if pd.Status.Duration == 0 {
							contentPrinter.Printf("%s\n", playTime)
						} else {
							contentPrinter.Printf("%s/%s\n", playTime, pd.Status)
						}
					}
					// Not so minimalistic output
					if minimal, _ := cmd.Flags().GetBool("minimal"); !minimal {
						forceASCII, _ := cmd.Flags().GetBool("ascii")
						_ = icat.PrintImageURL(pd.TrackRoles.Icon, forceASCII)
						fmt.Println() // newline so shell prompt does not appear to the right of the image
					}
				} else {
					headerPrinter.Print("Audio Transport: ")
					contentPrinter.Println("Stopped")
				}
			}
		} else {
			headerPrinter.Print("Source: ")
			contentPrinter.Println(source)
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.PersistentFlags().BoolP("minimal", "m", false, "Minimalistic output")
	statusCmd.PersistentFlags().BoolP("ascii", "a", false, "Force ASCII cover art output")
}
