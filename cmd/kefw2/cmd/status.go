package cmd

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"

	"github.com/qeesung/image2ascii/convert"
	"github.com/spf13/cobra"
)

// volumeCmd represents the volume command
var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"info", "state", "st"},
	Short:   "Status of the speakers",
	Long:    `Status of the speakers`,
	Args:    cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		source, err := currentSpeaker.Source()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		canControlPlayback, err := currentSpeaker.CanControlPlayback()
		if err != nil {
			fmt.Printf("Can't query source: %s\n", err.Error())
			os.Exit(1)
		}
		if canControlPlayback {
			pd, err := currentSpeaker.PlayerData()
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if playstate, err := currentSpeaker.IsPlaying(); err != nil {
				fmt.Println("error getting playstate:", err)
			} else {
				if playstate {
					playTime, _ := currentSpeaker.SongProgress()
					// Minimalistic output
					fmt.Println("Source:", source)
					fmt.Println("Audio Transport:", pd.MediaRoles.Title)
					fmt.Println("Artist:", pd.TrackRoles.MediaData.MetaData.Artist)
					fmt.Println("Album:", pd.TrackRoles.MediaData.MetaData.Album)
					fmt.Println("Track:", pd.TrackRoles.Title)
					if pd.Status.Duration == 0 {
						fmt.Printf("Duration: %s\n", playTime)
					} else {
						fmt.Printf("Duration: %s/%s\n", playTime, pd.Status)
					}
					// Not so minimalistic output
					if minimal, _ := cmd.Flags().GetBool("minimal"); !minimal {
						fmt.Print(imageArt2ASCII(pd.TrackRoles.Icon))
					}
				} else {
					fmt.Println("Audio Transport: stopped")
				}
			}
		} else {
			fmt.Println("Source:", source)
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.PersistentFlags().BoolP("minimal", "m", false, "Minimalistic output")
}

func imageArt2ASCII(imageURL string) string {
	if imageURL == "" {
		return ""
	}
	// Create convert options
	convertOptions := convert.DefaultOptions
	// convertOptions.FixedWidth = 80
	// convertOptions.FixedHeight = 40
	// convertOptions.FitScreen = true
	// convertOptions.Ratio = 0.2

	// Create the image converter
	converter := convert.NewImageConverter()

	// Fetch image from URL into an image instance
	artImage, err := fetchImageFromURL(imageURL)
	if err != nil {
		fmt.Println("Error fetching image:", err)
		return ""
	}

	// Convert image to ASCII string
	asciiString := converter.Image2ASCIIString(artImage, &convertOptions)
	return asciiString
}

func fetchImageFromURL(imageURL string) (image.Image, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	return img, nil
}
