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

// upnpConfigCmd is the parent for UPnP config subcommands.
var upnpConfigCmd = &cobra.Command{
	Use:   "upnp",
	Short: "Configure UPnP/DLNA settings",
	Long:  `Configure UPnP/DLNA media server settings.`,
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
}

// upnpServerConfigCmd is the parent for server config subcommands.
var upnpServerConfigCmd = &cobra.Command{
	Use:   "server",
	Short: "Configure default UPnP media server",
	Long:  `Configure the default UPnP media server for browsing and playback.`,
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
}

// upnpServerDefaultCmd shows or sets the default server.
var upnpServerDefaultCmd = &cobra.Command{
	Use:   "default [server-name]",
	Short: "Show or set the default UPnP media server",
	Long: `Show the current default UPnP media server, or set a new one.

Without arguments, displays the current default.
With a server name, sets that server as the default.

Examples:
  kefw2 config upnp server default                           # Show current
  kefw2 config upnp server default "Plex Media Server: srv"  # Set default`,
	Run: func(_ *cobra.Command, args []string) {
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

// upnpServerListCmd lists available servers.
var upnpServerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available UPnP media servers",
	Long:  `List all UPnP/DLNA media servers available on the network.`,
	Run: func(_ *cobra.Command, _ []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetMediaServers()
		exitOnError(err, "Failed to get media servers")

		defaultServer := viper.GetString("upnp.default_server")

		headerPrinter.Println("Available UPnP servers:")
		for _, item := range resp.Rows {
			if item.Type == TypeQuery {
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

// UPnPServerCompletion provides tab completion for UPnP server names.
func UPnPServerCompletion(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
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

// upnpIndexConfigCmd is the parent for index config subcommands.
var upnpContainerConfigCmd = &cobra.Command{
	Use:   "container",
	Short: "Configure UPnP container paths",
	Long:  `Configure container paths for browsing and indexing.`,
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
	},
}

// upnpContainerBrowseCmd shows or sets the browse container path.
var upnpContainerBrowseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Show or set the container path for browsing",
	Long: `Show the current container path for browsing, or set a new one.

The browse container determines the starting point when browsing your UPnP library.
When set, you won't see parent containers or other servers - the browse container
becomes your "root" for navigation.

Use "/" as separator for nested paths.

Without arguments, displays the current setting.
With a path, sets that as the starting container for browsing.

Examples:
  kefw2 config upnp container browse                           # Show current
  kefw2 config upnp container browse "Music"                   # Start from Music folder
  kefw2 config upnp container browse "Music/Hilli's Music"     # Start from specific folder
  kefw2 config upnp container browse ""                        # Clear (show all servers)`,
	Run: func(_ *cobra.Command, args []string) {
		if len(args) == 0 {
			// Show current setting
			containerPath := viper.GetString("upnp.browse_container")
			if containerPath == "" {
				contentPrinter.Println("No browse container configured (will show all servers).")
				contentPrinter.Println("Use 'kefw2 config upnp container browse <path>' to set one.")
				return
			}
			headerPrinter.Print("Browse container: ")
			contentPrinter.Println(containerPath)
			return
		}

		// Set new container path
		containerPath := args[0]

		// If a path is provided, validate it exists
		if containerPath != "" {
			serverPath := viper.GetString("upnp.default_server_path")
			if serverPath == "" {
				exitWithError("No default UPnP server configured. Set one first with: kefw2 config upnp server default <name>")
			}

			client := kefw2.NewAirableClient(currentSpeaker)
			_, resolvedName, err := kefw2.FindContainerByPath(client, serverPath, containerPath)
			if err != nil {
				exitWithError("Invalid container path: %v", err)
			}
			// Use the resolved path (with proper casing)
			containerPath = resolvedName
		}

		// Save to config
		viper.Set("upnp.browse_container", containerPath)
		err := viper.WriteConfig()
		exitOnError(err, "Error saving config")

		if containerPath == "" {
			taskConpletedPrinter.Println("Browse container cleared (will show all servers)")
		} else {
			taskConpletedPrinter.Print("Browse container set: ")
			contentPrinter.Println(containerPath)
		}
	},
	ValidArgsFunction: UPnPContainerCompletion,
}

// upnpContainerIndexCmd shows or sets the index container path.
var upnpContainerIndexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Show or set the container path for indexing",
	Long: `Show the current container path for indexing, or set a new one.

The container path determines which folder to start indexing from.
Use "/" as separator for nested paths.

TIP: For best results, use a "By Folder" path (e.g., "Music/Hilli's Music/By Folder").
This indexes your actual folder structure without any reorganization by the media server.

Without arguments, displays the current setting.
With a path, sets that as the default container to index.

Examples:
  kefw2 config upnp container index                                  # Show current
  kefw2 config upnp container index "Music"                          # Index Music folder
  kefw2 config upnp container index "Music/Hilli's Music/By Folder"  # Index by folder (recommended)
  kefw2 config upnp container index ""                               # Clear (index entire server)`,
	Run: func(_ *cobra.Command, args []string) {
		if len(args) == 0 {
			// Show current setting
			containerPath := viper.GetString("upnp.index_container")
			if containerPath == "" {
				contentPrinter.Println("No index container configured (will index entire server).")
				contentPrinter.Println("Use 'kefw2 config upnp container index <path>' to set one.")
				return
			}
			headerPrinter.Print("Index container: ")
			contentPrinter.Println(containerPath)
			return
		}

		// Set new container path
		containerPath := args[0]

		// If a path is provided, validate it exists
		if containerPath != "" {
			serverPath := viper.GetString("upnp.default_server_path")
			if serverPath == "" {
				exitWithError("No default UPnP server configured. Set one first with: kefw2 config upnp server default <name>")
			}

			client := kefw2.NewAirableClient(currentSpeaker)
			_, resolvedName, err := kefw2.FindContainerByPath(client, serverPath, containerPath)
			if err != nil {
				exitWithError("Invalid container path: %v", err)
			}
			// Use the resolved path (with proper casing)
			containerPath = resolvedName
		}

		// Save to config
		viper.Set("upnp.index_container", containerPath)
		err := viper.WriteConfig()
		exitOnError(err, "Error saving config")

		if containerPath == "" {
			taskConpletedPrinter.Println("Index container cleared (will index entire server)")
		} else {
			taskConpletedPrinter.Print("Index container set: ")
			contentPrinter.Println(containerPath)
		}
	},
	ValidArgsFunction: UPnPContainerCompletion,
}

// UPnPContainerCompletion provides tab completion for container paths.
func UPnPContainerCompletion(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	serverPath := viper.GetString("upnp.default_server_path")
	if serverPath == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)

	// Parse the path to complete
	// e.g., "Music/Hilli" -> parentPath="Music", prefix="Hilli"
	var parentPath, prefix string
	if idx := strings.LastIndex(toComplete, "/"); idx >= 0 {
		parentPath = toComplete[:idx]
		prefix = toComplete[idx+1:]
	} else {
		parentPath = ""
		prefix = toComplete
	}

	// Get containers at the parent path
	containers, err := kefw2.ListContainersAtPath(client, serverPath, parentPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var completions []string
	prefixLower := strings.ToLower(prefix)
	for _, name := range containers {
		if strings.HasPrefix(strings.ToLower(name), prefixLower) {
			// Build the full path for completion
			var fullPath string
			if parentPath != "" {
				fullPath = parentPath + "/" + name
			} else {
				fullPath = name
			}
			completions = append(completions, fullPath)
		}
	}

	// Don't add space after completion (user might want to continue the path)
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

func init() {
	ConfigCmd.AddCommand(upnpConfigCmd)
	upnpConfigCmd.AddCommand(upnpServerConfigCmd)
	upnpServerConfigCmd.AddCommand(upnpServerDefaultCmd)
	upnpServerConfigCmd.AddCommand(upnpServerListCmd)
	upnpConfigCmd.AddCommand(upnpContainerConfigCmd)
	upnpContainerConfigCmd.AddCommand(upnpContainerBrowseCmd)
	upnpContainerConfigCmd.AddCommand(upnpContainerIndexCmd)
}
