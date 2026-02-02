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
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// queueCmd represents the queue command
var queueCmd = &cobra.Command{
	Use:     "queue",
	Aliases: []string{"q"},
	Short:   "Manage the play queue",
	Long: `View and manage the current play queue.

Without subcommands, shows an interactive picker of queue items.

Keyboard shortcuts in picker:
  Enter     - Play selected track
  Ctrl+d    - Delete selected track
  Ctrl+x    - Clear entire queue
  Esc       - Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetPlayQueue()
		exitOnError(err, "Failed to get play queue")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("Play queue is empty.")
			return
		}

		// Show interactive picker using unified content picker
		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceUPnP,
			Items:       resp.Rows,
			Title:       fmt.Sprintf("Play Queue (%d tracks)", len(resp.Rows)),
			CurrentPath: "",
			Action:      ActionPlay,
			Callbacks:   DefaultQueueCallbacks(client),
		})
		exitOnError(err, "Error")

		if result.Played && result.Selected != nil {
			taskConpletedPrinter.Printf("Now playing: %s\n", result.Selected.Title)
		} else if result.Error != nil {
			exitWithError("Failed to play: %v", result.Error)
		}
	},
}

// DefaultQueueCallbacks returns callbacks for queue playback
func DefaultQueueCallbacks(client *kefw2.AirableClient) ContentPickerCallbacks {
	return ContentPickerCallbacks{
		Play: func(item *kefw2.ContentItem) error {
			// Find the index of this item in the queue
			resp, err := client.GetPlayQueue()
			if err != nil {
				return err
			}
			for i, row := range resp.Rows {
				if row.Path == item.Path {
					return client.PlayQueueIndex(i, item)
				}
			}
			// Fallback: play as first item
			return client.PlayQueueIndex(0, item)
		},
		Navigate: nil, // Queue items are not navigable
		IsPlayable: func(item *kefw2.ContentItem) bool {
			return item.Type == "audio"
		},
		DeleteFromQueue: func(item *kefw2.ContentItem) error {
			// Find the index of this item in the queue and remove it
			resp, err := client.GetPlayQueue()
			if err != nil {
				return err
			}
			for i, row := range resp.Rows {
				if row.Path == item.Path {
					return client.RemoveFromQueue([]int{i})
				}
			}
			return fmt.Errorf("item not found in queue")
		},
		ClearQueue: func() error {
			return client.ClearPlaylist()
		},
	}
}

// queueListCmd lists queue items non-interactively
var queueListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "show"},
	Short:   "List queue items",
	Long:    `List all tracks in the current play queue without an interactive picker.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetPlayQueue()
		exitOnError(err, "Failed to get play queue")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("Play queue is empty.")
			return
		}

		headerPrinter.Printf("Play Queue (%d tracks):\n", len(resp.Rows))
		for i, track := range resp.Rows {
			artist := ""
			if track.MediaData != nil && track.MediaData.MetaData.Artist != "" {
				artist = " - " + track.MediaData.MetaData.Artist
			}
			contentPrinter.Printf("  %2d. %s%s\n", i+1, track.Title, artist)
		}
	},
}

// queueClearCmd clears the queue
var queueClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the play queue",
	Long:  `Remove all tracks from the play queue.`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		err := client.ClearPlaylist()
		exitOnError(err, "Failed to clear queue")

		taskConpletedPrinter.Println("Queue cleared.")
	},
}

// queueRemoveCmd removes item(s) from the queue
var queueRemoveCmd = &cobra.Command{
	Use:   "remove <track>",
	Short: "Remove a track from the queue",
	Long: `Remove a track from the play queue by title or index.

Examples:
  kefw2 queue remove 3                    # Remove track at position 3
  kefw2 queue remove "Yesterday"          # Remove track by title
  kefw2 queue remove "Yesterday - The Beatles"  # Remove by title - artist`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: QueueItemCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetPlayQueue()
		exitOnError(err, "Failed to get play queue")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("Play queue is empty.")
			return
		}

		arg := args[0]

		// Try to parse as index first
		if idx, err := strconv.Atoi(arg); err == nil {
			// User provided a 1-based index
			if idx < 1 || idx > len(resp.Rows) {
				exitWithError("Invalid index: %d (queue has %d tracks)", idx, len(resp.Rows))
			}
			index := idx - 1 // Convert to 0-based
			trackTitle := resp.Rows[index].Title
			err := client.RemoveFromQueue([]int{index})
			exitOnError(err, "Failed to remove track")
			taskConpletedPrinter.Printf("Removed: %s\n", trackTitle)
			return
		}

		// Try to match by title or "title - artist" format
		index := findQueueItemByLabel(resp.Rows, arg)
		if index < 0 {
			exitWithError("Track not found: %s", arg)
		}

		trackTitle := resp.Rows[index].Title
		err = client.RemoveFromQueue([]int{index})
		exitOnError(err, "Failed to remove track")
		taskConpletedPrinter.Printf("Removed: %s\n", trackTitle)
	},
}

// findQueueItemByLabel finds a queue item by "Title - Artist" label or just title
// Returns the index (0-based) or -1 if not found
func findQueueItemByLabel(items []kefw2.ContentItem, label string) int {
	// Build labels with duplicate handling
	labelMap := buildQueueLabelMap(items)

	// Check if the label matches any item
	for _, entry := range labelMap {
		if entry.Label == label {
			return entry.Index
		}
	}

	// Also try matching just the title
	lowerLabel := strings.ToLower(label)
	for i, item := range items {
		if strings.ToLower(item.Title) == lowerLabel {
			return i
		}
	}

	return -1
}

// queueLabelEntry represents a queue item with its display label
type queueLabelEntry struct {
	Label string
	Index int
}

// buildQueueLabelMap builds unique labels for queue items
// Format: "Title - Artist" with "(2)", "(3)" etc. for duplicates
func buildQueueLabelMap(items []kefw2.ContentItem) []queueLabelEntry {
	entries := make([]queueLabelEntry, len(items))
	labelCounts := make(map[string]int)

	for i, item := range items {
		label := item.Title
		if item.MediaData != nil && item.MediaData.MetaData.Artist != "" {
			label = item.Title + " - " + item.MediaData.MetaData.Artist
		}

		// Track duplicates
		labelCounts[label]++
		if labelCounts[label] > 1 {
			label = fmt.Sprintf("%s (%d)", label, labelCounts[label])
		}

		entries[i] = queueLabelEntry{
			Label: label,
			Index: i,
		}
	}

	return entries
}

// queueMoveCmd moves an item in the queue
var queueMoveCmd = &cobra.Command{
	Use:   "move <track> <destination> [target-track]",
	Short: "Move a track within the queue",
	Long: `Move a track to a new position in the queue.

The track to move can be specified by position number (1-based) or by title.

Destination options:
  <number>         Move to specific position (1-based)
  <title>          Move to position of another track (swaps positions)
  top              Move to beginning of queue
  bottom           Move to end of queue
  up               Move one position up (silent if already at top)
  down             Move one position down (silent if already at bottom)
  next             Move to play after current track (or top if not playing)
  before <track>   Move before specified track
  after <track>    Move after specified track

Examples:
  kefw2 queue move 5 1                        # Move track 5 to position 1
  kefw2 queue move 5 top                      # Move track 5 to beginning
  kefw2 queue move 5 bottom                   # Move track 5 to end
  kefw2 queue move 5 up                       # Move track 5 one position up
  kefw2 queue move 5 down                     # Move track 5 one position down
  kefw2 queue move 5 next                     # Move track 5 to play after current
  kefw2 queue move "Yesterday" top            # Move track by title to beginning
  kefw2 queue move "Yesterday" before "Help"  # Move before another track
  kefw2 queue move "Yesterday" after "Help"   # Move after another track`,
	Args:              cobra.RangeArgs(2, 3),
	ValidArgsFunction: QueueMoveCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		// Get queue first
		resp, err := client.GetPlayQueue()
		exitOnError(err, "Failed to get play queue")

		queueLen := len(resp.Rows)
		if queueLen == 0 {
			contentPrinter.Println("Play queue is empty.")
			return
		}

		// Parse the track to move (first argument)
		fromIndex, err := parseQueueTrackArg(args[0], resp.Rows)
		if err != nil {
			exitWithError("%v", err)
		}

		// Parse the destination (second argument, and optionally third)
		toIndex := -1
		destination := strings.ToLower(args[1])

		switch destination {
		case "top":
			toIndex = 0
		case "bottom":
			toIndex = queueLen - 1
		case "up":
			if fromIndex == 0 {
				// Already at top, silent return
				return
			}
			toIndex = fromIndex - 1
		case "down":
			if fromIndex == queueLen-1 {
				// Already at bottom, silent return
				return
			}
			toIndex = fromIndex + 1
		case "next":
			currentIdx, _ := client.GetCurrentQueueIndex()
			if currentIdx < 0 {
				// Not playing from queue, move to top
				toIndex = 0
			} else {
				// Move to position after current track
				toIndex = currentIdx + 1
				if toIndex > queueLen-1 {
					toIndex = queueLen - 1
				}
			}
		case "before":
			if len(args) != 3 {
				exitWithError("'before' requires a target track: queue move <track> before <target>")
			}
			targetIdx, err := parseQueueTrackArg(args[2], resp.Rows)
			if err != nil {
				exitWithError("Target track: %v", err)
			}
			toIndex = targetIdx
			// If moving from after target to before, adjust index
			if fromIndex > targetIdx {
				toIndex = targetIdx
			} else {
				toIndex = targetIdx - 1
				if toIndex < 0 {
					toIndex = 0
				}
			}
		case "after":
			if len(args) != 3 {
				exitWithError("'after' requires a target track: queue move <track> after <target>")
			}
			targetIdx, err := parseQueueTrackArg(args[2], resp.Rows)
			if err != nil {
				exitWithError("Target track: %v", err)
			}
			// If moving from before target to after, adjust index
			if fromIndex < targetIdx {
				toIndex = targetIdx
			} else {
				toIndex = targetIdx + 1
				if toIndex > queueLen-1 {
					toIndex = queueLen - 1
				}
			}
		default:
			// Try parsing as position number or track title
			toIndex, err = parseQueueTrackArg(args[1], resp.Rows)
			if err != nil {
				exitWithError("Invalid destination: %v", err)
			}
		}

		// Validate toIndex
		if toIndex < 0 || toIndex >= queueLen {
			exitWithError("Invalid destination position: %d (queue has %d tracks)", toIndex+1, queueLen)
		}

		// No-op if same position
		if fromIndex == toIndex {
			return
		}

		trackTitle := resp.Rows[fromIndex].Title

		err = client.MoveQueueItem(fromIndex, toIndex)
		exitOnError(err, "Failed to move track")

		taskConpletedPrinter.Printf("Moved '%s' to position %d\n", trackTitle, toIndex+1)
	},
}

// parseQueueTrackArg parses a track argument as either a 1-based position or a title.
// Returns the 0-based index of the track.
func parseQueueTrackArg(arg string, items []kefw2.ContentItem) (int, error) {
	// Try to parse as position number first (1-based)
	if idx, err := strconv.Atoi(arg); err == nil {
		if idx < 1 || idx > len(items) {
			return -1, fmt.Errorf("invalid position: %d (queue has %d tracks)", idx, len(items))
		}
		return idx - 1, nil // Return 0-based
	}

	// Try to match by title or "title - artist" format
	index := findQueueItemByLabel(items, arg)
	if index < 0 {
		return -1, fmt.Errorf("track not found: %s", arg)
	}
	return index, nil
}

// queuePlayCmd plays a specific track from the queue
var queuePlayCmd = &cobra.Command{
	Use:   "play <track>",
	Short: "Play a track from the queue",
	Long: `Play a specific track from the queue by title or index.

Examples:
  kefw2 queue play 3                    # Play track at position 3
  kefw2 queue play "Yesterday"          # Play track by title`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: QueueItemCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetPlayQueue()
		exitOnError(err, "Failed to get play queue")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("Play queue is empty.")
			return
		}

		arg := args[0]
		var index int
		var track *kefw2.ContentItem

		// Try to parse as index first
		if idx, err := strconv.Atoi(arg); err == nil {
			if idx < 1 || idx > len(resp.Rows) {
				exitWithError("Invalid index: %d (queue has %d tracks)", idx, len(resp.Rows))
			}
			index = idx - 1
			track = &resp.Rows[index]
		} else {
			// Try to match by title or label
			index = findQueueItemByLabel(resp.Rows, arg)
			if index < 0 {
				exitWithError("Track not found: %s", arg)
			}
			track = &resp.Rows[index]
		}

		err = client.PlayQueueIndex(index, track)
		exitOnError(err, "Failed to play track")

		taskConpletedPrinter.Printf("Now playing: %s\n", track.Title)
	},
}

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueListCmd)
	queueCmd.AddCommand(queueClearCmd)
	queueCmd.AddCommand(queueRemoveCmd)
	queueCmd.AddCommand(queueMoveCmd)
	queueCmd.AddCommand(queuePlayCmd)
}
