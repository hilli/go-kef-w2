/*
Copyright © 2023-2026 Jens Hilligsøe

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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

var speakerCmd = &cobra.Command{
	Use:   "speaker",
	Short: "Manage speakers: discover, add, remove, list, default",
	Long:  `Manage speakers`,
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
}

func init() {
	speakerCmd.AddCommand(speakerAddCmd)
	speakerCmd.AddCommand(speakerRemoveCmd)
	speakerCmd.AddCommand(speakerListCmd)
	speakerCmd.AddCommand(speakerSetDefaultCmd)
	speakerCmd.AddCommand(speakerDiscoverCmd)
	speakerDiscoverCmd.Flags().Bool("save", false, "Save the discovered speakers to config file")
	speakerDiscoverCmd.Flags().IntP("timeout", "t", 1, "Set the timeout for speaker discovery (seconds)")
	addJSONFlag(speakerListCmd)
	addJSONFlag(speakerDiscoverCmd)
}

var discoverSpeakers = kefw2.DiscoverSpeakers

type listedSpeaker struct {
	speakerIdentity
	Configured bool `json:"configured"`
	Default    bool `json:"default"`
}

func describeSpeakers(items []*kefw2.KEFSpeaker) []listedSpeaker {
	result := make([]listedSpeaker, 0, len(items))
	for _, s := range items {
		result = append(result, listedSpeaker{
			speakerIdentity: identifySpeaker(s),
			Configured:      speakerDefined(s.IPAddress),
			Default:         defaultSpeaker != nil && defaultSpeaker.IPAddress == s.IPAddress,
		})
	}
	return result
}

var speakerDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover speakers",
	Long:  `Discover speakers with mDNS`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		save, _ := cmd.Flags().GetBool("save")
		timeout, _ := cmd.Flags().GetInt("timeout")
		if timeout < 1 || timeout > 300 {
			return fmt.Errorf("discovery timeout must be between 1 and 300 seconds")
		}

		newSpeakers, err := discoverSpeakers(cmd.Context(), time.Duration(timeout)*time.Second)
		if err != nil {
			return fmt.Errorf("discovering speakers: %w", err)
		}
		slices.SortFunc(newSpeakers, func(a, b *kefw2.KEFSpeaker) int {
			return strings.Compare(a.IPAddress, b.IPAddress)
		})
		if save && len(newSpeakers) > 0 {
			for _, s := range newSpeakers {
				if !speakerDefined(s.IPAddress) {
					speakers = append(speakers, *s)
				}
			}
			viper.Set("speakers", speakers)
			if len(speakers) == 1 && viper.GetString("defaultSpeaker") == "" {
				defaultSpeaker = &speakers[0]
				viper.Set("defaultSpeaker", defaultSpeaker.IPAddress)
			}
			if err := saveConfig(); err != nil {
				return err
			}
		}
		if jsonOutput(cmd) {
			return writeJSON(cmd, struct {
				Items []listedSpeaker `json:"items"`
				Saved bool            `json:"saved"`
			}{describeSpeakers(newSpeakers), save && len(newSpeakers) > 0})
		}
		if len(newSpeakers) == 0 {
			fmt.Println("No new speakers found.")
			fmt.Println("Make sure the speakers are connected to the same network as this computer.")
			fmt.Println("Try extending the discovery timeout with the --timeout flag.")
			fmt.Println("Ie:")
			fmt.Println()
			fmt.Println("    kefw2 config speaker discover --timeout 5 [--save]")
			fmt.Println()
			fmt.Println("Or try adding the speaker manually with:")
			fmt.Println()
			fmt.Println("    kefw2 config speaker add <ip-address>")
			return nil
		}
		for _, speaker := range newSpeakers {
			_, _ = headerPrinter.Print("Found speaker: ")
			_, _ = contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
		}
		return nil
	},
}

var speakerAddCmd = &cobra.Command{
	Use:   "add <ip-address>",
	Short: "Add a speaker",
	Long:  `Add a speaker`,
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return addSpeaker(args[0])
	},
}

var speakerRemoveCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove a speaker",
	Long:    `Remove a speaker`,
	Run: func(_ *cobra.Command, args []string) {
		if len(args) != 1 {
			_, _ = errorPrinter.Println("Error: missing speaker IP address")
			return
		}
		removeSpeaker(args[0])
	},
}

var speakerListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List speakers",
	Long:    `List speakers`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if jsonOutput(cmd) {
			items := make([]*kefw2.KEFSpeaker, 0, len(speakers))
			for i := range speakers {
				items = append(items, &speakers[i])
			}
			return writeJSON(cmd, struct {
				Items []listedSpeaker `json:"items"`
			}{describeSpeakers(items)})
		}
		for _, speaker := range speakers {
			if defaultSpeaker != nil && speaker.IPAddress == defaultSpeaker.IPAddress {
				_, _ = contentPrinter.Printf("%s (%s) [default]\n", speaker.Name, speaker.IPAddress)
			} else {
				_, _ = contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
			}
		}
		return nil
	},
	ValidArgsFunction: cobra.NoFileCompletions,
}

var speakerSetDefaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Set default speaker",
	Long:  "Set default speaker",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			if defaultSpeaker == nil {
				return fmt.Errorf("no default speaker configured")
			}
			_, _ = headerPrinter.Print("Default speaker: ")
			_, _ = contentPrinter.Printf("%s (%s)\n", defaultSpeaker.Name, defaultSpeaker.IPAddress)
			return nil
		}
		return setDefaultSpeaker(args[0])
	},
	ValidArgsFunction: ConfiguredSpeakersCompletion,
}

func addSpeaker(host string) (err error) {
	speaker, err := kefw2.NewSpeaker(host)
	if err != nil {
		return fmt.Errorf("error adding speaker: %w", err)
	}
	if speakerDefined(speaker.IPAddress) {
		return nil
	}
	speakers = append(speakers, *speaker)
	viper.Set("speakers", speakers)

	if len(speakers) == 1 {
		viper.Set("defaultSpeaker", speaker.IPAddress)
	}
	if err := saveConfig(); err != nil {
		return err
	}
	_, _ = taskConpletedPrinter.Print("Added speaker: ")
	_, _ = contentPrinter.Printf("%s (%s)\n", speaker.Name, speaker.IPAddress)
	return nil
}

func saveConfig() error {
	if err := os.MkdirAll(filepath.Dir(viper.ConfigFileUsed()), 0750); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}
	return nil
}

func removeSpeaker(host string) {
	for i, speaker := range speakers {
		if speaker.IPAddress == host {
			speakers = append(speakers[:i], speakers[i+1:]...)
			viper.Set("speakers", speakers)
			_, _ = taskConpletedPrinter.Printf("Removed speaker: %s (%s)\n", speaker.Name, speaker.IPAddress)
			_ = viper.WriteConfig()
			return
		}
	}
}

func setDefaultSpeaker(host string) (err error) {
	for _, speaker := range speakers {
		if speaker.IPAddress == host || speaker.Name == host {
			viper.Set("defaultSpeaker", speaker.IPAddress)
			return saveConfig()
		}
	}
	return errors.New("speaker not found")
}

func ConfiguredSpeakersCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	result := make([]string, 0, len(speakers)*2)
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
