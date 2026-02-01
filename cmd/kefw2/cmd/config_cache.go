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
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// cacheConfigSettings defines the available cache settings for completion
var cacheConfigSettings = []string{"enable", "disable", "ttl-default", "ttl-radio", "ttl-podcast", "ttl-upnp"}

// configCacheCmd configures cache settings
var configCacheCmd = &cobra.Command{
	Use:   "cache [setting] [value]",
	Short: "Show or configure cache settings",
	Long: `Show or configure cache settings for tab completion and browsing.

Without arguments, displays all current cache settings.
With a setting name, displays or sets that setting's value.

Available settings:
  enable      Enable caching (improves tab completion speed)
  disable     Disable caching
  ttl-default TTL for new/unknown services in seconds (default: 300)
  ttl-radio   TTL for radio cache in seconds (default: 300)
  ttl-podcast TTL for podcast cache in seconds (default: 300)
  ttl-upnp    TTL for UPnP cache in seconds (default: 60)

Examples:
  kefw2 config cache                    # Show all cache settings
  kefw2 config cache ttl-radio          # Show radio TTL value
  kefw2 config cache enable             # Enable caching
  kefw2 config cache ttl-radio 600      # Set radio cache TTL to 10 minutes`,
	Args: cobra.MaximumNArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			// Complete setting names
			completions := []string{
				"enable\tEnable caching",
				"disable\tDisable caching",
				"ttl-default\tTTL for new services (currently " + fmt.Sprintf("%d", viper.GetInt("cache.ttl_default")) + "s)",
				"ttl-radio\tTTL for radio cache (currently " + fmt.Sprintf("%d", viper.GetInt("cache.ttl_radio")) + "s)",
				"ttl-podcast\tTTL for podcast cache (currently " + fmt.Sprintf("%d", viper.GetInt("cache.ttl_podcast")) + "s)",
				"ttl-upnp\tTTL for UPnP cache (currently " + fmt.Sprintf("%d", viper.GetInt("cache.ttl_upnp")) + "s)",
			}
			return completions, cobra.ShellCompDirectiveNoFileComp
		}
		// No completion for values (user enters a number)
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		// No args: show all cache settings
		if len(args) == 0 {
			showAllCacheSettings()
			return
		}

		setting := args[0]

		switch setting {
		case "enable":
			viper.Set("cache.enabled", true)
			if err := viper.WriteConfig(); err != nil {
				errorPrinter.Printf("Failed to save config: %v\n", err)
				return
			}
			taskConpletedPrinter.Println("Cache enabled")

		case "disable":
			viper.Set("cache.enabled", false)
			if err := viper.WriteConfig(); err != nil {
				errorPrinter.Printf("Failed to save config: %v\n", err)
				return
			}
			taskConpletedPrinter.Println("Cache disabled")

		case "ttl-default", "ttl-radio", "ttl-podcast", "ttl-upnp":
			key := "cache.ttl_" + setting[4:] // Remove "ttl-" prefix

			// No value provided: show current value
			if len(args) < 2 {
				headerPrinter.Printf("%s: ", setting)
				contentPrinter.Printf("%d seconds\n", viper.GetInt(key))
				return
			}

			// Value provided: set it
			var seconds int
			if _, err := fmt.Sscanf(args[1], "%d", &seconds); err != nil {
				errorPrinter.Printf("Invalid TTL value: %s (must be a number in seconds)\n", args[1])
				return
			}
			viper.Set(key, seconds)
			if err := viper.WriteConfig(); err != nil {
				errorPrinter.Printf("Failed to save config: %v\n", err)
				return
			}
			taskConpletedPrinter.Printf("TTL for %s set to %d seconds\n", setting[4:], seconds)

		default:
			errorPrinter.Printf("Unknown setting: %s\n\n", setting)
			errorPrinter.Println("Available settings: enable, disable, ttl-default, ttl-radio, ttl-podcast, ttl-upnp")
		}
	},
}

// showAllCacheSettings displays all current cache configuration values
func showAllCacheSettings() {
	headerPrinter.Println("Cache Configuration:")

	// Enabled status
	enabled := viper.GetBool("cache.enabled")
	headerPrinter.Print("  enabled:     ")
	if enabled {
		contentPrinter.Println("true")
	} else {
		contentPrinter.Println("false")
	}

	// TTL values
	headerPrinter.Print("  ttl-default: ")
	contentPrinter.Printf("%d seconds\n", viper.GetInt("cache.ttl_default"))

	headerPrinter.Print("  ttl-radio:   ")
	contentPrinter.Printf("%d seconds\n", viper.GetInt("cache.ttl_radio"))

	headerPrinter.Print("  ttl-podcast: ")
	contentPrinter.Printf("%d seconds\n", viper.GetInt("cache.ttl_podcast"))

	headerPrinter.Print("  ttl-upnp:    ")
	contentPrinter.Printf("%d seconds\n", viper.GetInt("cache.ttl_upnp"))
}

func init() {
	ConfigCmd.AddCommand(configCacheCmd)
}
