package cmd

import (
	"fmt"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
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
					fmt.Println("Duration:", pd.Status.Duration)
					fmt.Println("PlayID:", pd.PlayID.TimeStamp)
					fmt.Println("Album Art:", pd.TrackRoles.Icon)
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
