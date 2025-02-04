package cmd

import (
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/hilli/icat"
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
					playTime, _ := currentSpeaker.SongProgress()
					// Minimalistic output
					fmt.Println("Source:", source)
					fmt.Println("Audio Transport:", pd.MediaRoles.Title)
					fmt.Println("Artist:", pd.TrackRoles.MediaData.MetaData.Artist)
					fmt.Println("Album:", pd.TrackRoles.MediaData.MetaData.Album)
					fmt.Println("Track:", pd.TrackRoles.Title)
					fmt.Printf("Duration: %s/%s\n", playTime, pd.Status)
					// Not so minimalistic output
					if minimal, _ := cmd.Flags().GetBool("minimal"); !minimal {
						icat.PrintImageURL(pd.TrackRoles.Icon)
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

