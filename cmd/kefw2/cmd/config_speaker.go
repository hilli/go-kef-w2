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
	"errors"
	"fmt"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage speakers: discover, add, remove, list, default",
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
	speakerCmd.AddCommand(speakerDiscoverCmd)
	speakerDiscoverCmd.PersistentFlags().BoolP("save", "", false, "Save the discovered speakers to config file")
	speakerDiscoverCmd.PersistentFlags().IntP("timeout", "t", 1, "Set the timeout for speaker discovery (seconds)")
}

var speakerDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover speakers",
	Long:  `Discover speakers with mDNS`,
	Run: func(cmd *cobra.Command, args []string) {
		save, _ := cmd.Flags().GetBool("save")
		timeout, _ := cmd.Flags().GetInt("timeout")

		newSpeakers, err := kefw2.DiscoverSpeakers(timeout)
		if err != nil {
			fmt.Println(err)
			return
		}
		if len(newSpeakers) == 0 {
			fmt.Println("No new speakers found.")
			fmt.Println("Make sure the speakers are connected to the same network as this computer.")
			fmt.Println("Try extending the discovery timeout with the --timeout flag.")
			fmt.Println("Ie:")
			fmt.Println()
			fmt.Println("    kefw2 speaker discover --timeout 5 [--save]")
			fmt.Println()
			fmt.Println("Or try adding the speaker manually with:")
			fmt.Println()
			fmt.Println("    kefw2 config speaker add <ip-address>")
			return
		}
		for _, speaker := range newSpeakers {
			headerPrinter.Print("Found speaker: ")
			contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
			if save {
				if err := addSpeaker(speaker.IPAddress); err != nil {
					errorPrinter.Printf("Error adding speaker (%s): %s\n", speaker.IPAddress, err)
				}
			}
		}
	},
}

var speakerAddCmd = &cobra.Command{
	Use:   "add <ip-address>",
	Short: "Add a speaker",
	Long:  `Add a speaker`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := addSpeaker(args[0]); err != nil {
			errorPrinter.Printf("Error adding speaker (%s): %s\n", args[0], err)
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
			errorPrinter.Println("Error: missing speaker IP address")
			return
		}
		if err := removeSpeaker(args[0]); err != nil {
			errorPrinter.Printf("Error removing speaker (%s): %s\n", args[0], err)
		}
	},
}

var speakerListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List speakers",
	Long:    `List speakers`,
	Run: func(cmd *cobra.Command, args []string) {
		defaultSpeakerIP := viper.GetString("defaultSpeaker")
		for _, speaker := range speakers {
			if speaker.IPAddress == defaultSpeakerIP {
				contentPrinter.Printf("%s (%s) [default]\n", speaker.Name, speaker.IPAddress)
			} else {
				contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
			}
		}
	},
	ValidArgsFunction: cobra.NoFileCompletions,
}

var speakerSetDefaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Set default speaker",
	Long:  "Set default speaker",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			headerPrinter.Print("Default speaker: ")
			contentPrinter.Printf("%s (%s)\n", defaultSpeaker.Name, defaultSpeaker.IPAddress)
			return
		}
		if err := setDefaultSpeaker(args[0]); err != nil {
			errorPrinter.Printf("Error setting default speaker (%s): %s\n", args[0], err)
		}
	},
	ValidArgsFunction: ConfiguredSpeakersCompletion,
}

func addSpeaker(host string) (err error) {
	speaker, err := kefw2.NewSpeaker(host)
	if err != nil {
		return fmt.Errorf("error adding speaker: %s", err)
	}
	if speakerDefined(speaker.IPAddress) {
		return nil
	}
	speakers = append(speakers, speaker)
	viper.Set("speakers", speakers)
	taskConpletedPrinter.Print("Added speaker: ")
	contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)

	if len(speakers) == 1 {
		viper.Set("defaultSpeaker", speaker.IPAddress)
		taskConpletedPrinter.Printf("Saved default speaker: ")
		contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
	}
	viper.WriteConfig()
	return
}

func removeSpeaker(host string) (err error) {
	for i, speaker := range speakers {
		if speaker.IPAddress == host {
			speakers = append(speakers[:i], speakers[i+1:]...)
			viper.Set("speakers", speakers)
			taskConpletedPrinter.Printf("Removed speaker: %s (%s)\n", speaker.Name, speaker.IPAddress)
			viper.WriteConfig()
			return
		}
	}
	return
}

func setDefaultSpeaker(host string) (err error) {
	found := false
	for _, speaker := range speakers {
		if speaker.IPAddress == host || speaker.Name == host {
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

func ConfiguredSpeakersCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	result := []string{}
	for _, speaker := range speakers {
		result = append(result, speaker.IPAddress)
		result = append(result, speaker.Name)
	}
	return result, cobra.ShellCompDirectiveNoFileComp
}

func speakerDefined(host string) bool {
	for _, speaker := range speakers {
		if speaker.IPAddress == host {
			return true
		}
	}
	return false
}
