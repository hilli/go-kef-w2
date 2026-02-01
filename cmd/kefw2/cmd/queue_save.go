/*
Copyright © 2023-2026 Jens Hilligsoe

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
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/hilli/go-kef-w2/kefw2"
)

// SavedQueue represents a saved queue file
type SavedQueue struct {
	Name   string       `yaml:"name"`
	Tracks []SavedTrack `yaml:"tracks"`
}

// SavedTrack contains the minimal data needed to restore a track to the queue.
// Only essential fields for playback and display are saved.
type SavedTrack struct {
	Title    string             `yaml:"title"`
	Icon     string             `yaml:"icon,omitempty"`
	Artist   string             `yaml:"artist,omitempty"`
	Album    string             `yaml:"album,omitempty"`
	Duration int                `yaml:"duration,omitempty"` // milliseconds
	Resource SavedTrackResource `yaml:"resource"`
}

// SavedTrackResource contains the audio stream information.
type SavedTrackResource struct {
	URI             string `yaml:"uri"`
	MimeType        string `yaml:"mimetype"`
	BitRate         int    `yaml:"bitrate,omitempty"`
	SampleFrequency int    `yaml:"samplefreq,omitempty"`
}

// toSavedTrack converts a ContentItem to a SavedTrack.
func toSavedTrack(item kefw2.ContentItem) SavedTrack {
	track := SavedTrack{
		Title: item.Title,
		Icon:  item.Icon,
	}

	if item.MediaData != nil {
		track.Artist = item.MediaData.MetaData.Artist
		track.Album = item.MediaData.MetaData.Album

		if len(item.MediaData.Resources) > 0 {
			res := item.MediaData.Resources[0]
			track.Duration = res.Duration
			track.Resource = SavedTrackResource{
				URI:             res.URI,
				MimeType:        res.MimeType,
				BitRate:         res.BitRate,
				SampleFrequency: res.SampleFrequency,
			}
		}
	}

	return track
}

// toContentItem converts a SavedTrack back to a ContentItem for queue loading.
func (t SavedTrack) toContentItem() kefw2.ContentItem {
	return kefw2.ContentItem{
		Title: t.Title,
		Type:  "audio",
		Icon:  t.Icon,
		MediaData: &kefw2.MediaData{
			MetaData: kefw2.MediaMetaData{
				Artist: t.Artist,
				Album:  t.Album,
			},
			Resources: []kefw2.MediaResource{
				{
					URI:             t.Resource.URI,
					MimeType:        t.Resource.MimeType,
					BitRate:         t.Resource.BitRate,
					Duration:        t.Duration,
					SampleFrequency: t.Resource.SampleFrequency,
				},
			},
		},
	}
}

// getQueuesDir returns the directory for saved queues
func getQueuesDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}

	queuesDir := filepath.Join(configDir, "kefw2", "queues")
	if err := os.MkdirAll(queuesDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create queues directory: %w", err)
	}

	return queuesDir, nil
}

// getSavedQueuePath returns the path for a saved queue file
func getSavedQueuePath(name string) (string, error) {
	queuesDir, err := getQueuesDir()
	if err != nil {
		return "", err
	}

	// Sanitize the name for filesystem use
	safeName := strings.ReplaceAll(name, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	safeName = strings.ReplaceAll(safeName, ":", "_")

	return filepath.Join(queuesDir, safeName+".yaml"), nil
}

// listSavedQueues returns a list of saved queue names
func listSavedQueues() ([]string, error) {
	queuesDir, err := getQueuesDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(queuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read queues directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		names = append(names, name)
	}

	return names, nil
}

// queueSaveCmd saves the current queue
var queueSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save the current queue to a file",
	Long: `Save the current play queue to a local file for later use.

Saved queues are stored in the OS config directory:
  macOS:   ~/Library/Application Support/kefw2/queues/
  Linux:   ~/.config/kefw2/queues/
  Windows: %AppData%\kefw2\queues\

Examples:
  kefw2 queue save "workout"
  kefw2 queue save "jazz favorites"`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		name := args[0]

		// Get current queue
		resp, err := client.GetPlayQueue()
		exitOnError(err, "Failed to get play queue")

		if len(resp.Rows) == 0 {
			exitWithError("Play queue is empty. Nothing to save.")
		}

		// Convert to SavedTrack format
		tracks := make([]SavedTrack, len(resp.Rows))
		for i, item := range resp.Rows {
			tracks[i] = toSavedTrack(item)
		}

		// Create saved queue
		savedQueue := SavedQueue{
			Name:   name,
			Tracks: tracks,
		}

		// Marshal to YAML
		data, err := yaml.Marshal(&savedQueue)
		exitOnError(err, "Failed to serialize queue")

		// Get file path
		filePath, err := getSavedQueuePath(name)
		exitOnError(err, "Failed to get queue file path")

		// Write to file
		err = os.WriteFile(filePath, data, 0644)
		exitOnError(err, "Failed to save queue")

		taskConpletedPrinter.Printf("Saved %d tracks to '%s'\n", len(resp.Rows), name)
	},
}

// queueLoadCmd loads a saved queue
var queueLoadCmd = &cobra.Command{
	Use:   "load <name>",
	Short: "Load a saved queue",
	Long: `Load a previously saved queue from a local file.

The loaded queue will replace the current play queue and start playback.

Examples:
  kefw2 queue load "workout"
  kefw2 queue load "jazz favorites"`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: SavedQueueCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		name := args[0]

		// Get file path
		filePath, err := getSavedQueuePath(name)
		exitOnError(err, "Failed to get queue file path")

		// Read file
		data, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				exitWithError("Saved queue '%s' not found.", name)
			}
			exitWithError("Failed to read queue file: %v", err)
		}

		// Unmarshal from YAML
		var savedQueue SavedQueue
		err = yaml.Unmarshal(data, &savedQueue)
		exitOnError(err, "Failed to parse queue file")

		if len(savedQueue.Tracks) == 0 {
			exitWithError("Saved queue is empty.")
		}

		// Convert SavedTracks to ContentItems
		contentItems := make([]kefw2.ContentItem, len(savedQueue.Tracks))
		for i, track := range savedQueue.Tracks {
			contentItems[i] = track.toContentItem()
		}

		// Clear current queue
		err = client.ClearPlaylist()
		exitOnError(err, "Failed to clear current queue")

		// Add tracks to queue and start playback
		err = client.AddToQueue(contentItems, true)
		exitOnError(err, "Failed to load queue")

		taskConpletedPrinter.Printf("Loaded %d tracks from '%s'\n", len(savedQueue.Tracks), name)
	},
}

// queueSavedCmd lists saved queues
var queueSavedCmd = &cobra.Command{
	Use:     "saved",
	Aliases: []string{"list-saved"},
	Short:   "List saved queues",
	Long:    `List all saved queues stored locally.`,
	Run: func(cmd *cobra.Command, args []string) {
		names, err := listSavedQueues()
		exitOnError(err, "Failed to list saved queues")

		if len(names) == 0 {
			contentPrinter.Println("No saved queues found.")
			return
		}

		headerPrinter.Println("Saved Queues:")
		for _, name := range names {
			contentPrinter.Printf("  %s\n", name)
		}
	},
}

// queueDeleteSavedCmd deletes a saved queue
var queueDeleteSavedCmd = &cobra.Command{
	Use:               "delete-saved <name>",
	Aliases:           []string{"rm-saved"},
	Short:             "Delete a saved queue",
	Long:              `Delete a previously saved queue file.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: SavedQueueCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		filePath, err := getSavedQueuePath(name)
		exitOnError(err, "Failed to get queue file path")

		err = os.Remove(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				exitWithError("Saved queue '%s' not found.", name)
			}
			exitWithError("Failed to delete queue: %v", err)
		}

		taskConpletedPrinter.Printf("Deleted saved queue '%s'\n", name)
	},
}

// SavedQueueCompletion provides tab completion for saved queue names
func SavedQueueCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names, err := listSavedQueues()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if toComplete == "" {
		return names, cobra.ShellCompDirectiveNoFileComp
	}

	// Filter by prefix
	lowerComplete := strings.ToLower(toComplete)
	var filtered []string
	for _, name := range names {
		if strings.HasPrefix(strings.ToLower(name), lowerComplete) {
			filtered = append(filtered, name)
		}
	}

	return filtered, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	queueCmd.AddCommand(queueSaveCmd)
	queueCmd.AddCommand(queueLoadCmd)
	queueCmd.AddCommand(queueSavedCmd)
	queueCmd.AddCommand(queueDeleteSavedCmd)
}
