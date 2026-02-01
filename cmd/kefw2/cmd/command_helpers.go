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
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// ============================================
// Shared Utility Functions
// ============================================

// findItemByName finds a content item by name with fallback matching strategies:
// 1. Exact case-insensitive match
// 2. Case-insensitive substring match (item title contains query)
// 3. Case-insensitive substring match (query contains item title)
// Returns the matched item and true if found, nil and false otherwise.
// Used by both radio and podcast commands for name-based lookups.
func findItemByName(items []kefw2.ContentItem, name string) (*kefw2.ContentItem, bool) {
	lowerName := strings.ToLower(name)

	// First pass: exact case-insensitive match
	for i := range items {
		if strings.EqualFold(items[i].Title, name) {
			return &items[i], true
		}
	}

	// Second pass: item title contains the query
	for i := range items {
		if strings.Contains(strings.ToLower(items[i].Title), lowerName) {
			return &items[i], true
		}
	}

	// Third pass: query contains the item title (for partial input)
	for i := range items {
		if strings.Contains(lowerName, strings.ToLower(items[i].Title)) {
			return &items[i], true
		}
	}

	return nil, false
}

// ============================================
// Pattern 3: Shared Result Handler
// ============================================

// HandlePickerResult processes the result from RunContentPicker and prints
// appropriate success/error messages. Returns true if an error occurred.
func HandlePickerResult(result ContentPickerResult, action ContentAction) bool {
	if result.Cancelled {
		return false
	}

	if result.Error != nil {
		switch action {
		case ActionRemoveFavorite:
			errorPrinter.Printf("Failed to remove favorite: %v\n", result.Error)
		case ActionSaveFavorite:
			errorPrinter.Printf("Failed to save favorite: %v\n", result.Error)
		case ActionAddToQueue:
			errorPrinter.Printf("Failed to add to queue: %v\n", result.Error)
		default:
			errorPrinter.Printf("Failed to play: %v\n", result.Error)
		}
		return true
	}

	if result.Selected == nil {
		return false
	}

	switch {
	case result.Queued:
		taskConpletedPrinter.Printf("Added to queue: %s\n", result.Selected.Title)
	case result.Removed:
		taskConpletedPrinter.Printf("Removed from favorites: %s\n", result.Selected.Title)
	case result.Saved:
		taskConpletedPrinter.Printf("Saved to favorites: %s\n", result.Selected.Title)
	case result.Played:
		taskConpletedPrinter.Printf("Now playing: %s\n", result.Selected.Title)
	}

	return false
}

// ============================================
// Pattern 1: Completion Function Factories
// ============================================

// ContentFetcher is a function that fetches content items from an AirableClient
type ContentFetcher func(client *kefw2.AirableClient) (*kefw2.RowsResponse, error)

// MakeStationCompletion creates a completion function for radio stations
// using the provided fetcher function. This eliminates duplication across
// RadioLocalCompletion, RadioFavoritesCompletion, RadioPopularCompletion, etc.
func MakeStationCompletion(fetcher ContentFetcher) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
		resp, err := fetcher(client)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// MakePodcastCompletion creates a completion function for podcasts with hierarchical
// episode support. It delegates to buildPodcastCompletionsWithEpisodes in completion_helpers.go.
func MakePodcastCompletion(fetcher ContentFetcher) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
		return buildPodcastCompletionsWithEpisodes(client, func() (*kefw2.RowsResponse, error) {
			return fetcher(client)
		}, toComplete)
	}
}

// ============================================
// Pattern 2: Category Command Factories
// ============================================

// CategoryConfig defines the configuration for a category browse command
type CategoryConfig struct {
	// Command configuration
	Use               string
	Short             string
	Aliases           []string
	ValidArgsFunction func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

	// Content fetching
	Fetcher ContentFetcher

	// Display
	EmptyMessage string // e.g., "No popular stations found."
	Title        string // e.g., "Popular Radio Stations"

	// Service-specific behavior
	ServiceType ServiceType
	Callbacks   func(*kefw2.AirableClient) ContentPickerCallbacks

	// Item processing
	FilterItems func([]kefw2.ContentItem) []kefw2.ContentItem
	FindByName  func([]kefw2.ContentItem, string) (*kefw2.ContentItem, bool)

	// Actions
	PlayItem        func(*kefw2.AirableClient, *kefw2.ContentItem) error
	AddFavorite     func(*kefw2.AirableClient, *kefw2.ContentItem) error
	RemoveFavorite  func(*kefw2.AirableClient, *kefw2.ContentItem) error
	AddToQueue      func(*kefw2.AirableClient, *kefw2.ContentItem) error
	SupportsQueue   bool
	SupportsSaveFav bool // For non-favorites categories
	SupportsRemove  bool // For favorites category
}

// MakeCategoryCommand creates a cobra.Command for browsing a content category.
// This abstracts the common pattern used in radioFavoritesCmd, radioPopularCmd,
// podcastFavoritesCmd, podcastPopularCmd, etc.
func MakeCategoryCommand(cfg CategoryConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:               cfg.Use,
		Aliases:           cfg.Aliases,
		Short:             cfg.Short,
		ValidArgsFunction: cfg.ValidArgsFunction,
		Run: func(cmd *cobra.Command, args []string) {
			// Get flags
			saveFav := false
			removeFav := false
			addToQueue := false

			if cfg.SupportsSaveFav {
				saveFav, _ = cmd.Flags().GetBool("save-favorite")
			}
			if cfg.SupportsRemove {
				removeFav, _ = cmd.Flags().GetBool("remove")
			}
			if cfg.SupportsQueue {
				addToQueue, _ = cmd.Flags().GetBool("queue")
			}

			client := kefw2.NewAirableClient(currentSpeaker)

			// Fetch items
			resp, err := cfg.Fetcher(client)
			if err != nil {
				errorPrinter.Printf("Failed to get %s: %v\n", cfg.Use, err)
				os.Exit(1)
			}

			// Filter items
			items := cfg.FilterItems(resp.Rows)
			if len(items) == 0 {
				contentPrinter.Println(cfg.EmptyMessage)
				return
			}

			// If an item name was provided, handle it directly
			if len(args) > 0 && cfg.FindByName != nil {
				itemName := args[0]
				if len(args) > 1 {
					itemName = args[0]
					for i := 1; i < len(args); i++ {
						itemName += " " + args[i]
					}
				}

				item, found := cfg.FindByName(items, itemName)
				if !found {
					errorPrinter.Printf("'%s' not found.\n", itemName)
					os.Exit(1)
				}

				if removeFav && cfg.RemoveFavorite != nil {
					headerPrinter.Printf("Removing: %s\n", item.Title)
					if err := cfg.RemoveFavorite(client, item); err != nil {
						errorPrinter.Printf("Failed to remove favorite: %v\n", err)
						os.Exit(1)
					}
					taskConpletedPrinter.Printf("Removed from favorites: %s\n", item.Title)
					return
				}

				if saveFav && cfg.AddFavorite != nil {
					headerPrinter.Printf("Saving: %s\n", item.Title)
					if err := cfg.AddFavorite(client, item); err != nil {
						errorPrinter.Printf("Failed to save favorite: %v\n", err)
						os.Exit(1)
					}
					taskConpletedPrinter.Printf("Saved to favorites: %s\n", item.Title)
					return
				}

				if addToQueue && cfg.AddToQueue != nil {
					headerPrinter.Printf("Adding to queue: %s\n", item.Title)
					if err := cfg.AddToQueue(client, item); err != nil {
						errorPrinter.Printf("Failed to add to queue: %v\n", err)
						os.Exit(1)
					}
					taskConpletedPrinter.Printf("Added to queue: %s\n", item.Title)
					return
				}

				// Default: play
				headerPrinter.Printf("Playing: %s\n", item.Title)
				if err := cfg.PlayItem(client, item); err != nil {
					errorPrinter.Printf("Failed to play: %v\n", err)
					os.Exit(1)
				}
				taskConpletedPrinter.Printf("Now playing: %s\n", item.Title)
				return
			}

			// Show interactive picker
			action := ActionPlay
			title := cfg.Title
			if removeFav {
				action = ActionRemoveFavorite
				title = title + " (remove mode)"
			} else if saveFav {
				action = ActionSaveFavorite
				title = title + " (save mode)"
			} else if addToQueue {
				action = ActionAddToQueue
				title = title + " (queue mode)"
			}

			result, err := RunContentPicker(ContentPickerConfig{
				ServiceType: cfg.ServiceType,
				Items:       items,
				Title:       title,
				CurrentPath: "",
				Action:      action,
				Callbacks:   cfg.Callbacks(client),
			})
			if err != nil {
				errorPrinter.Printf("Error: %v\n", err)
				os.Exit(1)
			}

			if HandlePickerResult(result, action) {
				os.Exit(1)
			}
		},
	}

	// Add flags based on configuration
	if cfg.SupportsSaveFav {
		cmd.Flags().BoolP("save-favorite", "f", false, "Save to favorites instead of playing")
	}
	if cfg.SupportsRemove {
		cmd.Flags().BoolP("remove", "r", false, "Remove from favorites")
	}
	if cfg.SupportsQueue {
		cmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing")
	}

	return cmd
}

// PodcastCategoryConfig holds configuration for creating podcast category commands
// This is separate from CategoryConfig because podcast commands have unique behavior:
// - Episode path parsing (ShowName/EpisodeName)
// - Queue support for episodes
// - Play latest episode when no episode specified
type PodcastCategoryConfig struct {
	Use               string // Command usage string (e.g., "popular [show[/episode]]")
	Short             string // Short description
	ValidArgsFunction func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
	Fetcher           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) // Fetch podcasts
	EmptyMessage      string                                                    // Message when no items found
	NotFoundMessage   string                                                    // Message when podcast not found (e.g., "popular podcasts")
	Title             string                                                    // Title for picker
	Callbacks         func(client *kefw2.AirableClient) ContentPickerCallbacks  // Callbacks factory
}

// MakePodcastCategoryCommand creates a cobra.Command for podcast category commands
// (popular, trending, history) with episode path parsing, queue support, and save-favorite.
func MakePodcastCategoryCommand(cfg PodcastCategoryConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:               cfg.Use,
		Short:             cfg.Short,
		ValidArgsFunction: cfg.ValidArgsFunction,
		Run: func(cmd *cobra.Command, args []string) {
			saveFav, _ := cmd.Flags().GetBool("save-favorite")
			addToQueue, _ := cmd.Flags().GetBool("queue")
			client := kefw2.NewAirableClient(currentSpeaker)

			resp, err := cfg.Fetcher(client)
			if err != nil {
				errorPrinter.Printf("Failed to get podcasts: %v\n", err)
				os.Exit(1)
			}

			podcasts := filterPodcastContainers(resp.Rows)

			if len(podcasts) == 0 {
				contentPrinter.Println(cfg.EmptyMessage)
				return
			}

			// If a podcast name was provided, find and play/save it directly
			if len(args) > 0 {
				showName, episodeName, hasEpisode := parsePodcastPath(strings.Join(args, " "))
				if podcast, found := findItemByName(podcasts, showName); found {
					if saveFav {
						headerPrinter.Printf("Saving: %s\n", podcast.Title)
						if err := client.AddPodcastFavorite(podcast); err != nil {
							errorPrinter.Printf("Failed to save favorite: %v\n", err)
							os.Exit(1)
						}
						taskConpletedPrinter.Printf("Saved to favorites: %s\n", podcast.Title)
					} else if hasEpisode {
						// Play specific episode
						episodes, err := client.GetPodcastEpisodesAll(podcast.Path)
						if err != nil {
							errorPrinter.Printf("Failed to get episodes: %v\n", err)
							os.Exit(1)
						}
						if episode, found := findEpisodeByName(episodes.Rows, episodeName); found {
							if addToQueue {
								headerPrinter.Printf("Adding to queue: %s\n", episode.Title)
								if err := client.AddToQueue([]kefw2.ContentItem{*episode}, false); err != nil {
									errorPrinter.Printf("Failed to add to queue: %v\n", err)
									os.Exit(1)
								}
								taskConpletedPrinter.Printf("Added to queue: %s\n", episode.Title)
							} else {
								headerPrinter.Printf("Playing: %s\n", episode.Title)
								if err := client.PlayPodcastEpisode(episode); err != nil {
									errorPrinter.Printf("Failed to play: %v\n", err)
									os.Exit(1)
								}
								taskConpletedPrinter.Printf("Now playing: %s\n", episode.Title)
							}
						} else {
							errorPrinter.Printf("Episode '%s' not found in '%s'.\n", episodeName, podcast.Title)
							os.Exit(1)
						}
					} else {
						// Play latest episode
						if addToQueue {
							headerPrinter.Printf("Adding latest episode of '%s' to queue\n", podcast.Title)
							episode, err := client.GetLatestEpisode(podcast)
							if err != nil {
								errorPrinter.Printf("Failed to get latest episode: %v\n", err)
								os.Exit(1)
							}
							if err := client.AddToQueue([]kefw2.ContentItem{*episode}, false); err != nil {
								errorPrinter.Printf("Failed to add to queue: %v\n", err)
								os.Exit(1)
							}
							taskConpletedPrinter.Printf("Added to queue: %s\n", episode.Title)
						} else {
							headerPrinter.Printf("Playing latest episode of: %s\n", podcast.Title)
							if err := playPodcastLatestEpisode(client, podcast); err != nil {
								errorPrinter.Printf("Failed to play: %v\n", err)
								os.Exit(1)
							}
							taskConpletedPrinter.Printf("Now playing latest episode of: %s\n", podcast.Title)
						}
					}
					return
				}
				errorPrinter.Printf("Podcast '%s' not found in %s.\n", showName, cfg.NotFoundMessage)
				os.Exit(1)
			}

			// Show interactive picker using unified content picker
			action := ActionPlay
			title := cfg.Title
			if saveFav {
				action = ActionSaveFavorite
				title = title + " (save mode)"
			} else if addToQueue {
				action = ActionAddToQueue
				title = title + " (queue mode)"
			}

			result, err := RunContentPicker(ContentPickerConfig{
				ServiceType: ServicePodcast,
				Items:       podcasts,
				Title:       title,
				CurrentPath: "",
				Action:      action,
				Callbacks:   cfg.Callbacks(client),
			})
			if err != nil {
				errorPrinter.Printf("Error: %v\n", err)
				os.Exit(1)
			}

			if HandlePickerResult(result, action) {
				os.Exit(1)
			}
		},
	}

	// Podcast category commands always support save-favorite and queue
	cmd.Flags().BoolP("save-favorite", "f", false, "Save to favorites instead of playing")
	cmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing")

	return cmd
}
