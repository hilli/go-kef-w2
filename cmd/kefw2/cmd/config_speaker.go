package cmd

import (
	"fmt"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage speakers: add, remove, list",
	Long:  `Manage speakers`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	speakerCmd.AddCommand(speakerAddCmd)
	speakerCmd.AddCommand(speakerRemoveCmd)
	speakerCmd.AddCommand(speakerListCmd)
}

var speakerAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a speaker",
	Long:  `Add a speaker`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := addSpeaker(args[0]); err != nil {
			fmt.Printf("Error adding speaker (%s): %s\n", args[0], err)
		}
	},
}

var speakerRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a speaker",
	Long:  `Remove a speaker`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("remove speaker")
	},
}

var speakerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List speakers",
	Long:  `List speakers`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Speakers:")
		for _, speaker := range speakers {
			fmt.Printf("- %s\n", speaker.Name)
		}
	},
}

func addSpeaker(host string) (err error) {
	speaker, err := kefw2.NewSpeaker(host)
	if err != nil {
		fmt.Println(err)
		return
	}
	speakers = append(speakers, speaker)
	viper.Set("speakers", speakers)
	fmt.Printf("Added speaker: %s (%s)\n", speaker.Name, speaker.IPAddress)
	viper.WriteConfig()
	return
}
