package cmd

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
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
		fmt.Println("Source:", source)

		if source == kefw2.SourceWiFi {
			pd, err := currentSpeaker.PlayerData()
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if playstate, err := currentSpeaker.IsPlaying(); err != nil {
				fmt.Println("error getting playstate:", err)
			} else {
				if playstate {
					fmt.Println("Audio Transport:", pd.MediaRoles.Title)
					fmt.Println("Artist:", pd.TrackRoles.MediaData.MetaData.Artist)
					fmt.Println("Album:", pd.TrackRoles.MediaData.MetaData.Album)
					fmt.Println("Track:", pd.TrackRoles.Title)
					fmt.Println("Duration:", pd.Status)
					// fmt.Println("PlayID:", pd.PlayID.TimeStamp)
					fmt.Println(imageArt2ASCII(pd.TrackRoles.Icon))
				} else {
					fmt.Println("Audio Transport: stopped")
				}
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func imageArt2ASCII(imageURL string) string {
	// Create convert options
	convertOptions := convert.DefaultOptions
	// convertOptions.FixedWidth = 80
	// convertOptions.FixedHeight = 40

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
