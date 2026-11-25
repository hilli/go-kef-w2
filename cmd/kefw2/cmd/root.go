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

	"github.com/hilli/go-kef-w2/kefw2"

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
	Version             string // Git Tag
	GitCommit           string // Git commit SHA
	GitBranch           string // Git branch
	BuildDate           string // Build date
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:               "kefw2",
	Short:             "kefw2 is a CLI tool for controlling KEF's W2 platform speakers",
	SilenceErrors:     true,
	SilenceUsage:      true,
	PersistentPreRunE: prepareCommand,
}

// noFileCompletion is a ValidArgsFunction that disables file completion.
func noFileCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func withCompletionContext(complete cobra.CompletionFunc, needsSpeaker func(*cobra.Command, []string) bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if !isUtilityCommand(cmd) && cfgFile != "" && cfgFile != viper.ConfigFileUsed() {
			if err := initConfig(); err != nil {
				cobra.CompErrorln(err.Error())
				return nil, cobra.ShellCompDirectiveError
			}
		}
		if needsSpeaker != nil && needsSpeaker(cmd, append(slices.Clone(args), toComplete)) {
			target := currentSpeakerParam
			if target == "" {
				target = viper.GetString("speaker")
			}
			if target != "" {
				if err := resolveSpeaker(target); err != nil {
					cobra.CompErrorln(err.Error())
					return nil, cobra.ShellCompDirectiveError
				}
			}
			if currentSpeaker == nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		return complete(cmd, args, toComplete)
	}
}

// disableFileCompletionRecursive disables file completion on all commands
// that don't have a custom ValidArgsFunction set.
func disableFileCompletionRecursive(cmd *cobra.Command) {
	if cmd.ValidArgsFunction == nil {
		cmd.ValidArgsFunction = noFileCompletion
	} else {
		cmd.ValidArgsFunction = withCompletionContext(cmd.ValidArgsFunction, func(cmd *cobra.Command, args []string) bool {
			switch cmd {
			case eqProfileCmd, eqProfileShowCmd, eqProfileSaveCmd, eqProfileLoadCmd,
				eqProfileDeleteCmd, eqProfileImportCmd, eqProfileExportCmd:
				return false
			}
			return cmd == upnpContainerBrowseCmd || cmd == upnpContainerIndexCmd || commandRequiresSpeaker(cmd, args)
		})
	}
	for _, child := range cmd.Commands() {
		disableFileCompletionRecursive(child)
	}
}

var VersionCmd = &cobra.Command{
	Use:  "version",
	Long: "Print the version number of kefw2",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("kefw2: Command line tool for controlling KEF's W2 platform speakers")
		headerPrinter.Print("Version: ")
		contentPrinter.Printf("%s\n", Version)
		headerPrinter.Print("Git commit: ")
		contentPrinter.Printf("%s\n", GitCommit)
		headerPrinter.Print("Git branch: ")
		contentPrinter.Printf("%s\n", GitBranch)
		headerPrinter.Print("Build date: ")
		contentPrinter.Printf("%s\n", BuildDate)
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

	// Disable file completion on all commands that don't have custom completion
	disableFileCompletionRecursive(rootCmd)

	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "Error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(ConfigCmd, VersionCmd)
	// Get OS-specific config directory
	cfgPath, err := os.UserConfigDir()
	cobra.CheckErr(err)
	cfgPath = filepath.Join(cfgPath, "kefw2")

	viper.SetConfigType("yaml")
	viper.SetConfigName("kefw2")
	viper.SetConfigFile(filepath.Join(cfgPath, "kefw2.yaml"))

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", viper.ConfigFileUsed(), "config file")
	rootCmd.PersistentFlags().StringVarP(&currentSpeakerParam, "speaker", "s", "", "speaker to operate on (name or IP). Default speaker will be used if not specified")

	// Register completion function for the speaker flag
	_ = rootCmd.RegisterFlagCompletionFunc("speaker", withCompletionContext(func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		var completions []string
		for _, s := range speakers {
			// Add name as completion option with description
			if s.Name != "" {
				completions = append(completions, fmt.Sprintf("%s\t%s (%s)", s.Name, s.Model, s.IPAddress))
			}
			// Also add IP address as completion option
			completions = append(completions, fmt.Sprintf("%s\t%s", s.IPAddress, s.Name))
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}, nil))
}

func prepareCommand(cmd *cobra.Command, args []string) error {
	if isUtilityCommand(cmd) || isFileOnlyCommand(cmd, args) {
		return nil
	}
	if err := initConfig(); err != nil {
		return err
	}
	if cmd == playerCmd {
		currentSpeaker = nil
		return nil
	}
	if commandRequiresSpeaker(cmd, args) {
		target := currentSpeakerParam
		if target == "" {
			target = viper.GetString("speaker")
		}
		if err := resolveSpeaker(target); err != nil {
			return err
		}
	}
	if !jsonOutput(cmd) && cmd != speakerListCmd && cmd != speakerDiscoverCmd {
		InitCache()
	}
	return nil
}

// initConfig reads configuration without resolving speakers or changing files.
func initConfig() error {
	speakers = nil
	defaultSpeaker = nil
	currentSpeaker = nil
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	}

	viper.SetEnvPrefix("kefw2")
	viper.AutomaticEnv() // read in environment variables that match KEFW2_*

	// Set default values for cache settings
	viper.SetDefault("cache.enabled", true)
	viper.SetDefault("cache.ttl_default", 300)
	viper.SetDefault("cache.ttl_radio", 300)
	viper.SetDefault("cache.ttl_podcast", 300)
	viper.SetDefault("cache.ttl_upnp", 60)

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.Is(err, os.ErrNotExist) && !errors.As(err, &notFound) {
			return fmt.Errorf("reading configuration: %w", err)
		}
	}
	// Unmarshal speakers
	if err := viper.UnmarshalKey("speakers", &speakers); err != nil {
		return fmt.Errorf("reading configured speakers: %w", err)
	}
	// Unmarshal default speaker and set it up
	defaultSpeakerIP := viper.GetString("defaultSpeaker")
	for i := range speakers {
		if speakers[i].IPAddress == defaultSpeakerIP {
			defaultSpeaker = &speakers[i]
			break
		}
	}

	// If no default speaker is set but we have a speaker, use the first one
	if defaultSpeaker == nil && len(speakers) == 1 {
		defaultSpeaker = &speakers[0]
	}
	currentSpeaker = defaultSpeaker
	return nil
}

func resolveSpeaker(target string) error {
	currentSpeaker = nil
	if target != "" {
		for i := range speakers {
			if speakers[i].IPAddress == target {
				currentSpeaker = &speakers[i]
				return nil
			}
		}
		for i := range speakers {
			if speakers[i].Name == target {
				if currentSpeaker != nil {
					return fmt.Errorf("speaker name %q is ambiguous; use an IP address", target)
				}
				currentSpeaker = &speakers[i]
			}
		}
		if currentSpeaker == nil {
			speaker, err := kefw2.NewSpeaker(target)
			if err != nil {
				return fmt.Errorf("resolving speaker %q: %w", target, err)
			}
			currentSpeaker = speaker
		}
	} else {
		currentSpeaker = defaultSpeaker
	}
	if currentSpeaker == nil {
		return fmt.Errorf("no unambiguous speaker selected; use --speaker NAME_OR_IP, " +
			"set a default with 'kefw2 config speaker default', or discover with 'kefw2 config speaker discover --save'")
	}
	return nil
}

func isUtilityCommand(cmd *cobra.Command) bool {
	cmdPath := strings.Split(cmd.CommandPath(), " ")
	return slices.Contains(cmdPath, "skill") || cmd.Name() == "version" || cmd.Name() == "help"
}

func isFileOnlyCommand(cmd *cobra.Command, args []string) bool {
	switch cmd {
	case queueSavedCmd, queueDeleteSavedCmd, eqProfileListCmd, eqProfileDeleteCmd,
		eqProfileImportCmd, eqProfileExportCmd:
		return true
	case eqProfileShowCmd:
		return len(args) > 0
	case upnpSearchCmd, cacheClearCmd:
		return jsonOutput(cmd)
	case upnpIndexCmd:
		rebuild, _ := cmd.Flags().GetBool("rebuild")
		return jsonOutput(cmd) && !rebuild
	default:
		return false
	}
}

func commandRequiresSpeaker(cmd *cobra.Command, args []string) bool {
	if cmd == playerCmd {
		return false
	}
	cmdPath := strings.Split(cmd.CommandPath(), " ")
	if isUtilityCommand(cmd) || slices.Contains(cmdPath, "completion") ||
		cmd.Name() == "__complete" || cmd.Name() == "__completeNoDesc" {
		return false
	}
	if isFileOnlyCommand(cmd, args) || cmd == cacheStatusCmd || cmd == cacheClearCmd {
		return false
	}
	if !slices.Contains(cmdPath, "config") {
		return true
	}
	return configCommandRequiresSpeaker(cmd, args)
}

func configCommandRequiresSpeaker(cmd *cobra.Command, args []string) bool {
	switch cmd {
	case eqProfileCmd, maxVolumeCmd, upnpServerListCmd:
		return true
	case eqProfileSaveCmd, eqProfileLoadCmd:
		return true
	case eqProfileShowCmd:
		return len(args) == 0
	case upnpServerDefaultCmd:
		return len(args) > 0 || (jsonOutput(cmd) && cmd.Flags().Changed("path"))
	case upnpContainerBrowseCmd, upnpContainerIndexCmd:
		return (len(args) > 0 && args[0] != "") || (jsonOutput(cmd) && cmd.Flags().Changed("path"))
	default:
		return false
	}
}
