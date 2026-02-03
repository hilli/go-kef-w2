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
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

// Styles for the UPnP TUI
var (
	upnpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")).
			MarginBottom(1)

	upnpStatusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	upnpSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))

	upnpPlayingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("82"))
)

// upnpItem represents a media server, container, or track in the list
type upnpItem struct {
	item kefw2.ContentItem
}

func (i upnpItem) Title() string {
	prefix := ""
	switch i.item.Type {
	case "container":
		prefix = "[Folder] "
	case "audio":
		prefix = "[Track] "
	}
	return prefix + i.item.Title
}

func (i upnpItem) Description() string {
	if i.item.MediaData != nil && i.item.MediaData.MetaData.Artist != "" {
		return i.item.MediaData.MetaData.Artist
	}
	return i.item.LongDescription
}

func (i upnpItem) FilterValue() string { return i.item.Title }

// upnpModel is the Bubbletea model for the UPnP browser
type upnpModel struct {
	client      *kefw2.AirableClient
	list        list.Model
	items       []kefw2.ContentItem
	loading     bool
	playing     string
	err         error
	width       int
	height      int
	breadcrumbs []breadcrumb // Navigation history
	quitting    bool
}

type breadcrumb struct {
	title string
	path  string
}

// Messages for async operations
type upnpBrowseResultMsg struct {
	items []kefw2.ContentItem
	err   error
}

type upnpPlayResultMsg struct {
	trackName string
	err       error
}

type upnpQueueResultMsg struct {
	trackName string
	count     int
	err       error
}

func initialUpnpModel(client *kefw2.AirableClient) upnpModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = upnpSelectedStyle
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Media Servers"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	return upnpModel{
		client:      client,
		list:        l,
		breadcrumbs: []breadcrumb{{title: "Media Servers", path: "ui:/upnp"}},
	}
}

func (m upnpModel) Init() tea.Cmd {
	return m.loadServers()
}

func (m upnpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't handle navigation keys when filtering
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if item, ok := m.list.SelectedItem().(upnpItem); ok {
				switch item.item.Type {
				case "container":
					// Navigate into container
					m.breadcrumbs = append(m.breadcrumbs, breadcrumb{
						title: item.item.Title,
						path:  item.item.Path,
					})
					m.loading = true
					return m, m.browseContainer(item.item.Path)
				case "audio":
					// Play the track
					return m, m.playTrack(&item.item)
				default:
					// Try to browse it (might be a server)
					m.breadcrumbs = append(m.breadcrumbs, breadcrumb{
						title: item.item.Title,
						path:  item.item.Path,
					})
					m.loading = true
					return m, m.browseContainer(item.item.Path)
				}
			}
		case "backspace", "esc", "h", "left":
			// Go back
			if len(m.breadcrumbs) > 1 {
				m.breadcrumbs = m.breadcrumbs[:len(m.breadcrumbs)-1]
				current := m.breadcrumbs[len(m.breadcrumbs)-1]
				m.loading = true
				return m, m.browseContainer(current.path)
			}
		case "p":
			// Play all tracks in current container
			if len(m.items) > 0 {
				return m, m.playAll()
			}
		case "r":
			// Refresh current view
			if len(m.breadcrumbs) > 0 {
				current := m.breadcrumbs[len(m.breadcrumbs)-1]
				m.loading = true
				return m, m.browseContainer(current.path)
			}
		case "a":
			// Add selected track to queue
			if i, ok := m.list.SelectedItem().(upnpItem); ok {
				if i.item.Type == "audio" {
					return m, m.addToQueue(&i.item)
				}
			}
		case "A":
			// Add all audio tracks to queue
			return m, m.addAllToQueue()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4
		footerHeight := 3
		listHeight := m.height - headerHeight - footerHeight
		if listHeight < 5 {
			listHeight = 5
		}
		m.list.SetSize(m.width-4, listHeight)

	case upnpBrowseResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.items = msg.items
			m.err = nil
			items := make([]list.Item, len(msg.items))
			for i, s := range msg.items {
				items[i] = upnpItem{item: s}
			}
			m.list.SetItems(items)
		}

	case upnpPlayResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.playing = msg.trackName
			m.err = nil
		}

	case upnpQueueResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else if msg.count == 1 {
			m.playing = fmt.Sprintf("Added to queue: %s", msg.trackName)
		} else {
			m.playing = fmt.Sprintf("Added %d tracks to queue", msg.count)
		}
	}

	// Update list
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m upnpModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Build breadcrumb path
	var pathParts []string
	for _, bc := range m.breadcrumbs {
		pathParts = append(pathParts, bc.title)
	}
	title := strings.Join(pathParts, " > ")
	b.WriteString(upnpTitleStyle.Render(title))
	b.WriteString("\n\n")

	// Loading or list
	if m.loading {
		b.WriteString("Loading...")
		b.WriteString("\n")
	} else {
		b.WriteString(m.list.View())
	}

	// Status bar
	var status string
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("Error: %v", m.err))
	} else if m.playing != "" {
		status = upnpPlayingStyle.Render(fmt.Sprintf("Now playing: %s", m.playing))
	} else {
		status = "Enter: play | a: add to queue | A: add all | p: play all | Backspace: back | q: quit"
	}
	b.WriteString("\n")
	b.WriteString(upnpStatusStyle.Render(status))

	return b.String()
}

// Command functions
func (m upnpModel) loadServers() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetMediaServers()
		if err != nil {
			return upnpBrowseResultMsg{err: err}
		}
		return upnpBrowseResultMsg{items: resp.Rows}
	}
}

func (m upnpModel) browseContainer(path string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.BrowseContainerAll(path)
		if err != nil {
			return upnpBrowseResultMsg{err: err}
		}
		return upnpBrowseResultMsg{items: resp.Rows}
	}
}

func (m upnpModel) playTrack(track *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.PlayUPnPTrack(track)
		if err != nil {
			return upnpPlayResultMsg{err: err}
		}
		return upnpPlayResultMsg{trackName: track.Title}
	}
}

func (m upnpModel) playAll() tea.Cmd {
	return func() tea.Msg {
		// Filter for audio tracks
		var tracks []kefw2.ContentItem
		for _, item := range m.items {
			if item.Type == "audio" {
				tracks = append(tracks, item)
			}
		}

		if len(tracks) == 0 {
			return upnpPlayResultMsg{err: fmt.Errorf("no audio tracks in current folder")}
		}

		err := m.client.PlayUPnPTracks(tracks)
		if err != nil {
			return upnpPlayResultMsg{err: err}
		}
		return upnpPlayResultMsg{trackName: fmt.Sprintf("%d tracks", len(tracks))}
	}
}

func (m upnpModel) addToQueue(track *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.AddToQueue([]kefw2.ContentItem{*track}, false)
		if err != nil {
			return upnpQueueResultMsg{err: err}
		}
		return upnpQueueResultMsg{trackName: track.Title, count: 1}
	}
}

func (m upnpModel) addAllToQueue() tea.Cmd {
	return func() tea.Msg {
		var tracks []kefw2.ContentItem
		for _, item := range m.items {
			if item.Type == "audio" {
				tracks = append(tracks, item)
			}
		}
		if len(tracks) == 0 {
			return upnpQueueResultMsg{err: fmt.Errorf("no audio tracks in current folder")}
		}
		err := m.client.AddToQueue(tracks, false)
		if err != nil {
			return upnpQueueResultMsg{err: err}
		}
		return upnpQueueResultMsg{count: len(tracks)}
	}
}

// upnpCmd represents the upnp command
var upnpCmd = &cobra.Command{
	Use:     "upnp",
	Aliases: []string{"media", "dlna"},
	Short:   "Browse and play from UPnP/DLNA media servers",
	Long: `Browse and play music from UPnP/DLNA media servers (Plex, Sonos, etc.)
on your local network.

Interactive mode: Opens a TUI for browsing media servers.

Keyboard shortcuts:
  Enter     - Open folder or play track
  a         - Add selected track to queue
  A         - Add all tracks in folder to queue
  p         - Play all tracks in current folder
  Backspace - Go back to parent folder
  r         - Refresh current view
  /         - Filter items
  q         - Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		model := initialUpnpModel(client)

		p := tea.NewProgram(model, tea.WithAltScreen())
		_, err := p.Run()
		exitOnError(err, "Error running media browser")
	},
}

// upnpBrowseCmd browses a specific path
var upnpBrowseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Browse a media server path with interactive picker",
	Long: `Browse a specific path on a media server with an interactive picker.
If no path is provided and a default server is configured, browses the default server.
Otherwise, lists available media servers.

Paths can be display paths (e.g., "Music/Albums") or full API paths (e.g., "upnp:/...").
Use tab completion to navigate the folder hierarchy.

Flags:
  -q, --queue    Add to queue instead of playing when selecting a track

You can navigate into folders and play tracks directly from the picker.`,
	ValidArgsFunction: HierarchicalUPnPCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		addToQueue, _ := cmd.Flags().GetBool("queue")

		var resp *kefw2.RowsResponse
		var err error
		var title string

		if len(args) > 0 {
			path := args[0]
			// Check if it's an API path or display path
			if strings.HasPrefix(path, "upnp:/") || strings.HasPrefix(path, "ui:/") {
				// API path - use directly, fetch all items for interactive browsing
				resp, err = client.BrowseContainerAll(path)
				title = fmt.Sprintf("Browse: %s", path)
			} else {
				// Display path - resolve via default server, fetch all items
				serverPath := viper.GetString("upnp.default_server_path")
				resp, err = client.BrowseUPnPByDisplayPathAll(path, serverPath)
				title = fmt.Sprintf("Browse: %s", path)
			}
		} else {
			// No args - check for default server or show server list
			serverPath := viper.GetString("upnp.default_server_path")
			if serverPath != "" {
				resp, err = client.BrowseContainerAll(serverPath)
				title = viper.GetString("upnp.default_server")
			} else {
				resp, err = client.GetMediaServers()
				title = "Media Servers"
			}
		}

		exitOnError(err, "Failed to browse")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No items found.")
			return
		}

		// Show interactive picker using unified content picker
		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title = title + " (queue mode)"
		}
		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceUPnP,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultUPnPCallbacks(client),
		})
		exitOnError(err, "Error")

		if result.Queued && result.Selected != nil {
			taskConpletedPrinter.Printf("Added to queue: %s\n", result.Selected.Title)
		} else if result.Played && result.Selected != nil {
			taskConpletedPrinter.Printf("Now playing: %s\n", result.Selected.Title)
		} else if result.Error != nil {
			exitWithError("Failed to play: %v", result.Error)
		}
	},
}

// upnpPlayCmd plays content from a path
var upnpPlayCmd = &cobra.Command{
	Use:   "play <path>",
	Short: "Play content from a UPnP path",
	Long: `Play all audio tracks from a UPnP path (artist, album, folder, etc.)

Paths can be display paths (e.g., "Music/Albums/Abbey Road") or full API paths.
Use tab completion to navigate the folder hierarchy.

If the path contains sub-folders (e.g., an artist with multiple albums),
all tracks from all sub-folders are collected and played.

Flags:
  -q, --queue    Add to queue instead of playing immediately
                 (starts playback only if queue was empty)

Examples:
  kefw2 upnp play "Music/Albums/Abbey Road"
  kefw2 upnp play "Music/All Artists/Alice in Chains"    # Plays all albums
  kefw2 upnp play -q "Music/Albums/Abbey Road"           # Add to queue`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: HierarchicalUPnPCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithHTTPTimeout(60*time.Second))
		addToQueue, _ := cmd.Flags().GetBool("queue")

		var containerPath string

		// Check if it's an API path or display path
		if strings.HasPrefix(path, "upnp:/") || strings.HasPrefix(path, "ui:/") {
			containerPath = path
		} else {
			// Display path - resolve via default server
			serverPath := viper.GetString("upnp.default_server_path")
			resp, err := client.BrowseUPnPByDisplayPath(path, serverPath)
			exitOnError(err, "Failed to resolve path")

			// Get the container path from the response
			if resp.Roles != nil && resp.Roles.Path != "" {
				containerPath = resp.Roles.Path
			} else {
				// Fallback: navigate to get the actual container path
				containerPath, _, err = findContainerByPath(client, serverPath, path)
				exitOnError(err, "Failed to find container")
			}
		}

		// Get all tracks recursively
		headerPrinter.Printf("Scanning: %s\n", path)
		tracks, err := client.GetContainerTracksRecursive(containerPath)
		exitOnError(err, "Failed to get tracks")

		if len(tracks) == 0 {
			exitWithError("No audio tracks found at this path.")
		}

		if addToQueue {
			headerPrinter.Printf("Adding to queue: %s\n", path)
			err := client.AddToQueue(tracks, true)
			exitOnError(err, "Failed to add to queue")
			taskConpletedPrinter.Printf("Added %d tracks to queue.\n", len(tracks))
		} else {
			headerPrinter.Printf("Playing %d tracks from: %s\n", len(tracks), path)
			err := client.PlayUPnPTracks(tracks)
			exitOnError(err, "Failed to play")
			taskConpletedPrinter.Println("Playback started!")
		}
	},
}

// upnpSearchCmd searches the local track index
var upnpSearchCmd = &cobra.Command{
	Use:   "search [query...]",
	Short: "Search or browse tracks in your UPnP music library",
	Long: `Search for tracks by title, artist, or album in your UPnP music library.

Without a query, opens the full library browser where you can filter interactively.
With a query, shows pre-filtered results matching your search terms.

Requires a search index. Run 'kefw2 upnp index --rebuild' to create or update it.

Examples:
  kefw2 upnp search                    # Browse full library with filter
  kefw2 upnp search beatles            # Search for "beatles"
  kefw2 upnp search abbey road         # Search for "abbey road"
  kefw2 upnp search public enemy uzi   # Search across title/artist/album
  kefw2 upnp search -q bohemian        # Add to queue instead of playing`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		query := ""
		if len(args) > 0 {
			query = strings.Join(args, " ")
		}
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithHTTPTimeout(60*time.Second))

		// Load index
		index, err := LoadTrackIndex()
		if err != nil {
			exitWithError("Could not load search index: %v\nRun 'kefw2 upnp index --rebuild' to create one.", err)
		}
		if index == nil {
			exitWithError("No search index found. Run 'kefw2 upnp index --rebuild' to create one.")
		}

		// Check if index is stale
		if !IsTrackIndexFresh(index, defaultIndexMaxAge) {
			headerPrinter.Printf("Note: Search index is %s old. Run 'kefw2 upnp index --rebuild' to refresh.\n\n",
				time.Since(index.IndexedAt).Round(time.Hour))
		}

		// Search or browse all tracks
		var results []IndexedTrack
		var title string
		if query != "" {
			results = SearchTracks(index, query, 100)
			if len(results) == 0 {
				contentPrinter.Printf("No tracks found for '%s'\n", query)
				return
			}
			title = fmt.Sprintf("Search results for '%s' (%d tracks)", query, len(results))
		} else {
			// No query - show all tracks for browsing
			results = index.Tracks
			title = fmt.Sprintf("UPnP Library (%d tracks)", len(results))
		}

		// Convert to ContentItems for the picker
		items := make([]kefw2.ContentItem, len(results))
		for i, track := range results {
			items[i] = IndexedTrackToContentItem(&track)
			// Add duration and details to description for display
			desc := track.Artist
			if track.Album != "" {
				if desc != "" {
					desc += " - "
				}
				desc += track.Album
			}
			if track.Duration > 0 {
				if desc != "" {
					desc += " "
				}
				desc += fmt.Sprintf("(%s)", FormatDuration(track.Duration))
			}
			items[i].LongDescription = desc
		}

		// Show interactive picker
		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title = title + " (queue mode)"
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceUPnP,
			Items:       items,
			Title:       title,
			Action:      action,
			Callbacks:   DefaultUPnPCallbacks(client),
		})
		exitOnError(err, "Error")

		if result.Queued && result.Selected != nil {
			taskConpletedPrinter.Printf("Added to queue: %s\n", result.Selected.Title)
		} else if result.Played && result.Selected != nil {
			taskConpletedPrinter.Printf("Now playing: %s\n", result.Selected.Title)
		} else if result.Error != nil {
			exitWithError("Failed: %v", result.Error)
		}
	},
}

// upnpIndexCmd builds or shows the track index
var upnpIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Build or show the search index for your UPnP library",
	Long: `Build or show the search index for your UPnP music library.

Without flags, shows the current index status.
Use --rebuild to force a fresh index.
Use --container to start indexing from a specific folder path (e.g., "Music/Hilli's Music/By Folder").

The container path can be saved in config with:
  kefw2 config upnp index container "Music/Hilli's Music/By Folder"

The index is stored locally and used by 'kefw2 upnp search' for instant results.

Examples:
  kefw2 upnp index                                                       # Show index status
  kefw2 upnp index --rebuild                                             # Rebuild using configured container
  kefw2 upnp index --rebuild --container "Music/Hilli's Music/By Folder" # Index specific folder`,
	Run: func(cmd *cobra.Command, args []string) {
		rebuild, _ := cmd.Flags().GetBool("rebuild")
		containerPath, _ := cmd.Flags().GetString("container")
		// Use longer timeout for indexing (60s per request, large libraries need more time)
		client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithHTTPTimeout(60*time.Second))

		// Check for default server
		serverPath := viper.GetString("upnp.default_server_path")
		serverName := viper.GetString("upnp.default_server")
		if serverPath == "" {
			exitWithError("No default UPnP server configured. Set one with: kefw2 config upnp server default <name>")
		}

		// Use configured container if --container not specified
		configuredContainer := viper.GetString("upnp.index_container")
		if containerPath == "" && configuredContainer != "" {
			containerPath = configuredContainer
		}

		if !rebuild {
			// Show status
			index, _ := LoadTrackIndex()
			headerPrinter.Println("UPnP Track Index Status:")
			if index != nil {
				contentPrinter.Printf("  Server:      %s\n", index.ServerName)
				if index.ContainerName != "" {
					contentPrinter.Printf("  Container:   %s\n", index.ContainerName)
				}
				contentPrinter.Printf("  Tracks:      %d\n", index.TrackCount)
				age := time.Since(index.IndexedAt)
				contentPrinter.Printf("  Age:         %v\n", age.Round(time.Second))
				contentPrinter.Printf("  Location:    %s\n", getTrackIndexPath())

				if index.ServerName != serverName {
					headerPrinter.Printf("\n  Note: Index is for different server (%s vs %s)\n", index.ServerName, serverName)
					headerPrinter.Println("  Run 'kefw2 upnp index --rebuild' to reindex")
				} else if age > defaultIndexMaxAge {
					headerPrinter.Println("\n  Note: Index is older than 24 hours")
					headerPrinter.Println("  Run 'kefw2 upnp index --rebuild' to refresh")
				}
			} else {
				contentPrinter.Println("  No index found")
			}

			// Show configured container for next rebuild
			if configuredContainer != "" {
				headerPrinter.Printf("\n  Configured container: %s\n", configuredContainer)
			}
			if index == nil || configuredContainer != "" {
				contentPrinter.Println("  Run 'kefw2 upnp index --rebuild' to create/update index")
			}
			return
		}

		// Rebuild index
		if containerPath != "" {
			headerPrinter.Printf("Building search index for %s (starting from %s)...\n", serverName, containerPath)
		} else {
			headerPrinter.Printf("Building search index for %s...\n", serverName)
		}

		startTime := time.Now()
		index, err := BuildTrackIndex(client, serverPath, serverName, containerPath, func(containers, tracks int, current string) {
			fmt.Printf("\r  Scanning... %d containers, %d tracks found", containers, tracks)
		})
		fmt.Println() // Newline after progress
		exitOnError(err, "Failed to build index")

		if err := SaveTrackIndex(index); err != nil {
			exitWithError("Failed to save index: %v", err)
		}

		duration := time.Since(startTime).Round(time.Millisecond)
		taskConpletedPrinter.Printf("Indexed %d tracks in %v\n", index.TrackCount, duration)
		contentPrinter.Printf("Index saved to: %s\n", getTrackIndexPath())
	},
}

func init() {
	rootCmd.AddCommand(upnpCmd)
	upnpCmd.AddCommand(upnpBrowseCmd)
	upnpCmd.AddCommand(upnpPlayCmd)
	upnpCmd.AddCommand(upnpSearchCmd)
	upnpCmd.AddCommand(upnpIndexCmd)

	// Add -q flag for queue mode
	upnpBrowseCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing when selecting a track")
	upnpPlayCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	upnpSearchCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing")
	upnpIndexCmd.Flags().Bool("rebuild", false, "Force rebuild the index")
	upnpIndexCmd.Flags().String("container", "", "Container path to index (e.g., 'Music/Hilli's Music/By Folder')")

	// Register tab completion for --container flag
	_ = upnpIndexCmd.RegisterFlagCompletionFunc("container", UPnPContainerCompletion)
}
