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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

// upnpConfigCmd is the parent for UPnP config subcommands
var upnpConfigCmd = &cobra.Command{
	Use:   "upnp",
	Short: "Configure UPnP/DLNA settings",
	Long:  `Configure UPnP/DLNA media server settings.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// upnpServerConfigCmd is the parent for server config subcommands
var upnpServerConfigCmd = &cobra.Command{
	Use:   "server",
	Short: "Configure default UPnP media server",
	Long:  `Configure the default UPnP media server for browsing and playback.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// upnpServerDefaultCmd shows or sets the default server
var upnpServerDefaultCmd = &cobra.Command{
	Use:   "default [server-name]",
	Short: "Show or set the default UPnP media server",
	Long: `Show the current default UPnP media server, or set a new one.

Without arguments, displays the current default.
With a server name, sets that server as the default.

Examples:
  kefw2 config upnp server default                           # Show current
  kefw2 config upnp server default "Plex Media Server: srv"  # Set default`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			// Show current default
			serverName := viper.GetString("upnp.default_server")
			if serverName == "" {
				contentPrinter.Println("No default UPnP server configured.")
				contentPrinter.Println("Use 'kefw2 config upnp server list' to see available servers.")
				return
			}
			headerPrinter.Print("Default UPnP server: ")
			contentPrinter.Println(serverName)
			return
		}

		// Set new default
		serverName := args[0]
		client := kefw2.NewAirableClient(currentSpeaker)

		server, err := client.GetMediaServerByName(serverName)
		exitOnError(err, "Error")

		// Save to config
		viper.Set("upnp.default_server", server.Title)
		viper.Set("upnp.default_server_path", server.Path)
		err = viper.WriteConfig()
		exitOnError(err, "Error saving config")

		taskConpletedPrinter.Print("Default UPnP server set: ")
		contentPrinter.Println(server.Title)
	},
	ValidArgsFunction: UPnPServerCompletion,
}

// upnpServerListCmd lists available servers
var upnpServerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available UPnP media servers",
	Long:  `List all UPnP/DLNA media servers available on the network.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetMediaServers()
		exitOnError(err, "Failed to get media servers")

		defaultServer := viper.GetString("upnp.default_server")

		headerPrinter.Println("Available UPnP servers:")
		for _, item := range resp.Rows {
			if item.Type == "query" {
				continue // Skip "Search" entry
			}
			if item.Title == defaultServer {
				contentPrinter.Printf("  %s [default]\n", item.Title)
			} else {
				contentPrinter.Printf("  %s\n", item.Title)
			}
		}
	},
	ValidArgsFunction: cobra.NoFileCompletions,
}

// UPnPServerCompletion provides tab completion for UPnP server names
func UPnPServerCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)
	resp, err := client.GetMediaServers()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var completions []string
	for _, item := range resp.Rows {
		if item.Type == "query" {
			continue // Skip "Search"
		}
		if strings.HasPrefix(strings.ToLower(item.Title), strings.ToLower(toComplete)) {
			completions = append(completions, item.Title)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	ConfigCmd.AddCommand(upnpConfigCmd)
	upnpConfigCmd.AddCommand(upnpServerConfigCmd)
	upnpServerConfigCmd.AddCommand(upnpServerDefaultCmd)
	upnpServerConfigCmd.AddCommand(upnpServerListCmd)
}
