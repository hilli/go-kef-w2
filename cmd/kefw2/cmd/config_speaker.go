package cmd

import (
	"errors"
	"fmt"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage speakers: add, remove, list, default",
	Long:  `Manage speakers`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	speakerCmd.AddCommand(speakerAddCmd)
	speakerCmd.AddCommand(speakerRemoveCmd)
	speakerCmd.AddCommand(speakerListCmd)
	speakerCmd.AddCommand(speakerSetDefaultCmd)
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
	Use:     "remove",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a speaker",
	Long:    `Remove a speaker`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			fmt.Println("Error: missing speaker IP address")
			return
		}
		if err := removeSpeaker(args[0]); err != nil {
			fmt.Printf("Error removing speaker (%s): %s\n", args[0], err)
		}
	},
}

var speakerListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List speakers",
	Long:    `List speakers`,
	Run: func(cmd *cobra.Command, args []string) {
		for _, speaker := range speakers {
			fmt.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
		}
	},
}

var speakerSetDefaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Set default speaker",
	Long:  "Set default speaker",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			fmt.Printf("Default speaker is: %s (%s)\n", defaultSpeaker.Name, defaultSpeaker.IPAddress)
			return
		}
		if err := setDefaultSpeaker(args[0]); err != nil {
			fmt.Printf("Error setting default speaker (%s): %s\n", args[0], err)
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

func removeSpeaker(host string) (err error) {
	for i, speaker := range speakers {
		if speaker.IPAddress == host {
			speakers = append(speakers[:i], speakers[i+1:]...)
			viper.Set("speakers", speakers)
			fmt.Printf("Removed speaker: %s (%s)\n", speaker.Name, speaker.IPAddress)
			viper.WriteConfig()
			return
		}
	}
	return
}

func setDefaultSpeaker(host string) (err error) {
	found := false
	for _, speaker := range speakers {
		if speaker.IPAddress == host {
			viper.Set("defaultSpeaker", speaker.IPAddress)
			viper.WriteConfig()
			found = true
			return
		}
	}
	if !found {
		return errors.New("speaker not found")
	}
	return
}
