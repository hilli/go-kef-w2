/*
Copyright © 2023-2024 Jens Hilligsøe

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
	"runtime/debug"

	"github.com/hilli/go-kef-w2/kefw2"
	log "github.com/sirupsen/logrus"

	cc "github.com/ivanpirog/coloredcobra"
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

var VersionCmd = &cobra.Command{
	Use:  "version",
	Long: "Print the version number of kefw2",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("kefw2: Command line tool for controlling KEF's W2 platform speakers")
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, v := range info.Settings {
				switch v.Key {
				case "vcs.revision":
					fmt.Printf("Version: %s\n", v.Value)
				case "vcs.time":
					fmt.Printf("Build date: %s\n", v.Value)
				}
			}
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cc.Init(&cc.Config{
		RootCmd:  rootCmd,
		Headings: cc.HiCyan + cc.Bold + cc.Underline,
		Commands: cc.HiYellow + cc.Bold,
		Example:  cc.Italic,
		ExecName: cc.Bold,
		Flags:    cc.Bold,
	})

	// Pre-run check to ensure we have a speaker configured except for config and version commands
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Skip check for these commands
		if commandRequiresAsSpeaker(cmd) {
			return
		}

		if currentSpeaker == nil && len(speakers) == 0 {
			fmt.Fprintf(os.Stderr, "No speakers configured. Please configure a speaker first:\n")
			fmt.Fprintf(os.Stderr, "- Discover speakers automatically:\n")
			fmt.Fprintf(os.Stderr, "    kefw2 config speaker discover --save\n")
			fmt.Fprintf(os.Stderr, "- Manually add a speaker:\n")
			fmt.Fprintf(os.Stderr, "    kefw2 config speaker add IP_ADDRESS\n")
			os.Exit(1)
		}
	}

	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(ConfigCmd, VersionCmd)
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
		// Output here interferes with the completion cmd if there is no config file.
		// fmt.Fprintln(os.Stderr, "Couldn't read config file:", viper.ConfigFileUsed(), " Create one by adding a speaker: `kefw2 config speaker add IP_ADDRESS`")
		return
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

	// If no default speaker is set but we have a speaker, use the first one
	if defaultSpeaker == nil && len(speakers) == 1 {
		defaultSpeaker = &speakers[0]
		viper.Set("defaultSpeaker", defaultSpeaker.IPAddress)
		viper.WriteConfig()
		fmt.Printf("No default speaker was set. Using first available speaker as default: %s (%s)\n",
			defaultSpeaker.Name, defaultSpeaker.IPAddress)
	}

	if currentSpeakerParam != "" {
		newSpeaker, err := kefw2.NewSpeaker(currentSpeakerParam)
		if err != nil {
			fmt.Printf("Hmm, %s does not look like it is a KEF W2 speaker:\n%s\n", currentSpeakerParam, err.Error())
		}
		currentSpeaker = &newSpeaker
	} else {
		currentSpeaker = defaultSpeaker
	}
}

func commandRequiresAsSpeaker(cmd *cobra.Command) bool {
	if cmd.Name() == "config" {
		configSubCmd := cmd.Parent()
		if configSubCmd != nil {
			if configSubCmd.Name() == "config" {
				return false
			}
			configSubSubCmd := configSubCmd.Parent()
			if configSubSubCmd != nil {
				if configSubSubCmd.Name() == "config" {
					return false
				}
			}
		}
	}
	if cmd.Name() == "version" {
		return false
	}
	return true
}
