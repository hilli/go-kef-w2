/*
Copyright © 2023 Jens Hilligsøe

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
	"os"
	"path/filepath"

	"github.com/hilli/go-kef-w2/kefw2"
	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile             string
	currentSpeakerParam string
	speakers            []kefw2.KEFSpeaker
	defaultSpeaker      *kefw2.KEFSpeaker
	currentSpeaker      *kefw2.KEFSpeaker
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kefw2",
	Short: "kefw2 is a CLI tool for controlling KEF's W2 platform speakers",
	Long:  ``,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Find home directory.
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)
	cfgPath := filepath.Join(home, ".config", "kefw2")
	err = os.MkdirAll(cfgPath, 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatal(err)
	}

	viper.SetConfigType("yaml")
	viper.SetConfigName("kefw2")
	viper.SetConfigFile(filepath.Join(cfgPath, "kefw2.yaml"))

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", viper.ConfigFileUsed(), "config file")
	rootCmd.PersistentFlags().StringVarP(&currentSpeakerParam, "speaker", "s", "", "speaker to operate on. Default speaker will be used if not specified")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(ConfigCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	}

	viper.SetEnvPrefix("kefw2")
	viper.AutomaticEnv() // read in environment variables that match KEFW2_*

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
	// Unmarshal speakers
	if err := viper.UnmarshalKey("speakers", &speakers); err != nil {
		log.Fatal(err)
	}
	// Unmarshal default speaker and set it up
	defaultSpeakerIP := viper.GetString("defaultSpeaker")
	for _, s := range speakers {
		if s.IPAddress == defaultSpeakerIP {
			defaultSpeaker = &s
			break
		}
	}
	if currentSpeakerParam != "" {
		for _, s := range speakers {
			if s.IPAddress == currentSpeakerParam {
				currentSpeaker = &s
				break
			}
		}
	} else {
		if defaultSpeaker == nil {
			log.Fatal("Default speaker not found. Set it with `kefw2 config speaker default` or specify it with the --speaker (-s) flag")
		}
		currentSpeaker = defaultSpeaker
	}
}
